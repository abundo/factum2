package optical

import (
	"fmt"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

// Graph is an in-memory adjacency list for one walk or a RebuildStale batch.
type Graph struct {
	Ifaces       map[uint]models.Interface
	Devices      map[uint]models.Device
	Ports        map[uint]models.OpticalPort // by interface
	ConnsByIface map[uint][]models.Connection
	XCsByIface   map[uint][]models.OpticalXConnect
	Conns        map[uint]models.Connection
	XCs          map[uint]models.OpticalXConnect
}

// LoadGraph loads every connection, xconnect, optical port, and the
// devices/interfaces they touch.
func LoadGraph(db *gorm.DB) (*Graph, error) {
	g := &Graph{
		Ifaces:       map[uint]models.Interface{},
		Devices:      map[uint]models.Device{},
		Ports:        map[uint]models.OpticalPort{},
		ConnsByIface: map[uint][]models.Connection{},
		XCsByIface:   map[uint][]models.OpticalXConnect{},
		Conns:        map[uint]models.Connection{},
		XCs:          map[uint]models.OpticalXConnect{},
	}
	var conns []models.Connection
	if err := db.Find(&conns).Error; err != nil {
		return nil, err
	}
	var xcs []models.OpticalXConnect
	if err := db.Find(&xcs).Error; err != nil {
		return nil, err
	}
	var ports []models.OpticalPort
	if err := db.Find(&ports).Error; err != nil {
		return nil, err
	}
	for _, p := range ports {
		g.Ports[p.InterfaceID] = p
	}
	ifaceIDs := map[uint]bool{}
	for _, c := range conns {
		g.Conns[c.ID] = c
		g.ConnsByIface[c.InterfaceAID] = append(g.ConnsByIface[c.InterfaceAID], c)
		g.ConnsByIface[c.InterfaceBID] = append(g.ConnsByIface[c.InterfaceBID], c)
		ifaceIDs[c.InterfaceAID] = true
		ifaceIDs[c.InterfaceBID] = true
	}
	for _, x := range xcs {
		g.XCs[x.ID] = x
		g.XCsByIface[x.InterfaceAID] = append(g.XCsByIface[x.InterfaceAID], x)
		g.XCsByIface[x.InterfaceBID] = append(g.XCsByIface[x.InterfaceBID], x)
		ifaceIDs[x.InterfaceAID] = true
		ifaceIDs[x.InterfaceBID] = true
	}
	ids := make([]uint, 0, len(ifaceIDs))
	for id := range ifaceIDs {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		var ifaces []models.Interface
		if err := db.Where("id IN ?", ids).Find(&ifaces).Error; err != nil {
			return nil, err
		}
		devIDs := map[uint]bool{}
		for _, i := range ifaces {
			g.Ifaces[i.ID] = i
			devIDs[i.DeviceID] = true
		}
		dids := make([]uint, 0, len(devIDs))
		for id := range devIDs {
			dids = append(dids, id)
		}
		var devs []models.Device
		if err := db.Where("id IN ?", dids).Find(&devs).Error; err != nil {
			return nil, err
		}
		for _, d := range devs {
			g.Devices[d.ID] = d
		}
	}
	// Always load every interface we might start on even if isolated
	var allIfaces []models.Interface
	if err := db.Find(&allIfaces).Error; err != nil {
		return nil, err
	}
	devNeed := map[uint]bool{}
	for _, i := range allIfaces {
		if _, ok := g.Ifaces[i.ID]; !ok {
			g.Ifaces[i.ID] = i
		}
		if _, ok := g.Devices[i.DeviceID]; !ok {
			devNeed[i.DeviceID] = true
		}
	}
	if len(devNeed) > 0 {
		ids := make([]uint, 0, len(devNeed))
		for id := range devNeed {
			ids = append(ids, id)
		}
		var devs []models.Device
		if err := db.Where("id IN ?", ids).Find(&devs).Error; err != nil {
			return nil, err
		}
		for _, d := range devs {
			g.Devices[d.ID] = d
		}
	}
	return g, nil
}

// Hop is one walk step (same shape as ServiceHop, without IDs).
type Hop struct {
	Kind         string
	InterfaceID  *uint
	ConnectionID *uint
	XConnectID   *uint
	DeviceID     *uint
	FreqHz       uint64
	Label        string
}

// Result is the outcome of a walk.
type Result struct {
	Status      string
	FreqHz      uint64
	Error       string
	Hops        []Hop
	Tributaries []uint
	StartKind   string
}

type arrival string

const (
	arrStart       arrival = "start"
	arrConnection  arrival = "connection"
	arrXConnect    arrival = "xconnect"
	arrPassthrough arrival = "passthrough"
)

// Walk traces from startID. If zID != 0 the walk is "complete" only if Z is reached.
func Walk(g *Graph, startID, zID uint, mode string) Result {
	if mode == "" {
		mode = models.TraceModeWDM
	}
	if _, ok := g.Ifaces[startID]; !ok {
		return Result{Status: models.PathConflict, Error: "start interface not found"}
	}
	startKind := classifyStart(g, startID, mode)
	if mode == models.TraceModeFiber && portRole(g, startID) == models.PortROADMAddDrop ||
		mode == models.TraceModeFiber && portRole(g, startID) == models.PortROADMDegree ||
		mode == models.TraceModeFiber && portRole(g, startID) == models.PortTXPLine {
		return Result{Status: models.PathConflict, Error: "fiber mode cannot start on a ROADM/txp line port", StartKind: startKind}
	}

	var hops []Hop
	visited := map[uint]bool{}
	var lambda uint64
	cur := startID
	arr := arrStart
	var arrID uint
	seenDevice := map[uint]bool{}

	appendIface := func(id uint) {
		iface := g.Ifaces[id]
		hops = append(hops, Hop{Kind: models.HopInterface, InterfaceID: u(id), FreqHz: lambda, Label: ifaceLabel(g, id)})
		if !seenDevice[iface.DeviceID] {
			seenDevice[iface.DeviceID] = true
			hops = append(hops, Hop{Kind: models.HopDevice, DeviceID: u(iface.DeviceID), FreqHz: lambda, Label: g.Devices[iface.DeviceID].Name})
		}
	}
	appendIface(cur)
	visited[cur] = true

	for {
		if zID != 0 && cur == zID && arr != arrStart {
			return Result{Status: models.PathComplete, FreqHz: lambda, Hops: hops, StartKind: startKind}
		}

		nextID, nextArr, nextArrID, hop, err, tributaries := nextMove(g, cur, arr, arrID, startKind, mode, lambda, visited)
		if err == "conflict" {
			return Result{Status: models.PathConflict, FreqHz: lambda, Error: hop.Label, Hops: hops, StartKind: startKind, Tributaries: tributaries}
		}
		if nextID == 0 {
			// no legal leave
			status := models.PathIncomplete
			if zID == 0 && len(hops) > 1 {
				// open-ended preview: we walked as far as we could
				status = models.PathIncomplete
			}
			if zID == 0 && arr != arrStart && noMoreExpected(g, cur, mode) {
				status = models.PathComplete
			}
			return Result{Status: status, FreqHz: lambda, Hops: hops, StartKind: startKind, Tributaries: tributaries}
		}
		if visited[nextID] {
			return Result{Status: models.PathIncomplete, FreqHz: lambda, Error: "revisit", Hops: hops, StartKind: startKind}
		}

		if hop.Kind != "" {
			hops = append(hops, hop)
		}
		if p := g.Ports[nextID]; p.FreqHz != 0 && (p.Role == models.PortTXPLine || p.Role == models.PortROADMAddDrop) {
			if lambda == 0 {
				lambda = p.FreqHz
			} else if lambda != p.FreqHz && mode == models.TraceModeWDM {
				return Result{Status: models.PathConflict, FreqHz: lambda, Error: "wavelength mismatch", Hops: hops, StartKind: startKind}
			}
		}
		if hop.FreqHz != 0 && lambda == 0 {
			lambda = hop.FreqHz
		}
		appendIface(nextID)
		visited[nextID] = true
		cur = nextID
		arr = nextArr
		arrID = nextArrID
	}
}

func noMoreExpected(g *Graph, id uint, mode string) bool {
	// A far customer port or fiber end has a connection we arrived on and
	// nothing else to take — that's a complete open-ended walk.
	role := portRole(g, id)
	if mode == models.TraceModeWDM {
		return role == "" || role == models.PortTXPClient || role == models.PortFiber
	}
	return role == "" || role == models.PortFiber
}

func nextMove(g *Graph, cur uint, arr arrival, arrID uint, startKind, mode string, lambda uint64, visited map[uint]bool) (uint, arrival, uint, Hop, string, []uint) {
	// Start-kind overrides.
	if arr == arrStart {
		switch startKind {
		case models.StartTXPClient, "optical":
			return leaveViaXConnect(g, cur, 0, mode, lambda, visited)
		default:
			return leaveViaConnection(g, cur, 0, mode, visited)
		}
	}
	if arr == arrConnection {
		return leaveViaXConnectOrPT(g, cur, arrID, mode, lambda, visited)
	}
	// arrived via xconnect or passthrough
	return leaveViaConnection(g, cur, 0, mode, visited)
}

func leaveViaConnection(g *Graph, cur, usedConn uint, mode string, visited map[uint]bool) (uint, arrival, uint, Hop, string, []uint) {
	for _, c := range g.ConnsByIface[cur] {
		if c.ID == usedConn {
			continue
		}
		other := c.InterfaceBID
		if other == cur {
			other = c.InterfaceAID
		}
		if visited[other] {
			continue
		}
		if mode == models.TraceModeFiber {
			if r := portRole(g, other); r == models.PortROADMAddDrop || r == models.PortROADMDegree || r == models.PortTXPLine {
				return 0, "", 0, Hop{Label: "fiber path hit a ROADM/txp line"}, "conflict", nil
			}
		}
		return other, arrConnection, c.ID, Hop{
			Kind: models.HopConnection, ConnectionID: u(c.ID),
			Label: fmt.Sprintf("%s ↔ %s", ifaceLabel(g, c.InterfaceAID), ifaceLabel(g, c.InterfaceBID)),
		}, "", nil
	}
	return 0, "", 0, Hop{}, "", nil
}

func leaveViaXConnectOrPT(g *Graph, cur, usedConn uint, mode string, lambda uint64, visited map[uint]bool) (uint, arrival, uint, Hop, string, []uint) {
	id, arr, arrID, hop, err, trib := leaveViaXConnect(g, cur, 0, mode, lambda, visited)
	if id != 0 || err == "conflict" {
		return id, arr, arrID, hop, err, trib
	}
	return leaveViaImplicitPT(g, cur, usedConn, visited)
}

func leaveViaXConnect(g *Graph, cur, usedXC uint, mode string, lambda uint64, visited map[uint]bool) (uint, arrival, uint, Hop, string, []uint) {
	var tributaries []uint
	for _, x := range g.XCsByIface[cur] {
		if x.ID == usedXC {
			continue
		}
		other := x.InterfaceBID
		if other == cur {
			other = x.InterfaceAID
		}
		if mode == models.TraceModeFiber {
			if x.Kind != models.XCPassthrough {
				return 0, "", 0, Hop{Label: "fiber mode cannot follow a WDM xconnect"}, "conflict", nil
			}
		} else {
			if x.Kind == models.XCAddDrop || x.Kind == models.XCExpress {
				if lambda != 0 && x.FreqHz != 0 && x.FreqHz != lambda {
					continue
				}
			}
			if x.Kind == models.XCTributary && portRole(g, cur) == models.PortTXPLine {
				// diagnostic fan-out: collect all clients
				tributaries = append(tributaries, other)
			}
		}
		if visited[other] {
			continue
		}
		hop := Hop{
			Kind: models.HopXConnect, XConnectID: u(x.ID), FreqHz: x.FreqHz,
			Label: fmt.Sprintf("%s ↔ %s", ifaceLabel(g, x.InterfaceAID), ifaceLabel(g, x.InterfaceBID)),
		}
		return other, arrXConnect, x.ID, hop, "", tributaries
	}
	return 0, "", 0, Hop{}, "", tributaries
}

func leaveViaImplicitPT(g *Graph, cur, usedConn uint, visited map[uint]bool) (uint, arrival, uint, Hop, string, []uint) {
	iface := g.Ifaces[cur]
	dev, ok := g.Devices[iface.DeviceID]
	if !ok || (dev.OpticalKind != models.OpticalKindILA && dev.OpticalKind != models.OpticalKindPassive) {
		return 0, "", 0, Hop{}, "", nil
	}
	// exactly two cabled interfaces on this device
	var cabled []uint
	seen := map[uint]bool{}
	for _, i := range g.Ifaces {
		if i.DeviceID != iface.DeviceID {
			continue
		}
		if len(g.ConnsByIface[i.ID]) == 0 {
			continue
		}
		if !seen[i.ID] {
			seen[i.ID] = true
			cabled = append(cabled, i.ID)
		}
	}
	if len(cabled) != 2 {
		return 0, "", 0, Hop{}, "", nil
	}
	other := cabled[0]
	if other == cur {
		other = cabled[1]
	}
	if visited[other] {
		return 0, "", 0, Hop{}, "", nil
	}
	return other, arrPassthrough, 0, Hop{}, "", nil
}

func classifyStart(g *Graph, id uint, mode string) string {
	role := portRole(g, id)
	if role == models.PortFiber {
		return models.StartFiberPort
	}
	if mode != models.TraceModeFiber &&
		(role == models.PortTXPClient || role == models.PortTXPLine ||
			role == models.PortROADMAddDrop || role == models.PortROADMDegree) {
		if role == models.PortTXPClient {
			return models.StartTXPClient
		}
		return "optical"
	}
	for _, c := range g.ConnsByIface[id] {
		other := c.InterfaceBID
		if other == id {
			other = c.InterfaceAID
		}
		if portRole(g, other) == models.PortTXPClient && mode != models.TraceModeFiber {
			return models.StartCustomerPort
		}
	}
	if mode == models.TraceModeFiber {
		return models.StartFiberPort
	}
	return models.StartCustomerPort
}

func portRole(g *Graph, id uint) string {
	return g.Ports[id].Role
}

func ifaceLabel(g *Graph, id uint) string {
	i := g.Ifaces[id]
	d := g.Devices[i.DeviceID]
	if d.Name == "" {
		return i.Name
	}
	return d.Name + " " + i.Name
}

func u(id uint) *uint { return &id }
