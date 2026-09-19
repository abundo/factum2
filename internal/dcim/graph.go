package dcim

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

const (
	maxGraphDevices = 200
	maxGraphEdges   = 500
	maxGraphDepth   = 3
)

type GraphQuery struct {
	SiteID   uint
	RackID   uint
	DeviceID uint
	Depth    int
}

func GraphScopeKey(q GraphQuery) string {
	if q.DeviceID != 0 {
		d := q.Depth
		if d <= 0 {
			d = 1
		}
		return fmt.Sprintf("device:%d:d%d", q.DeviceID, d)
	}
	if q.RackID != 0 {
		return fmt.Sprintf("rack:%d", q.RackID)
	}
	if q.SiteID != 0 {
		return fmt.Sprintf("site:%d", q.SiteID)
	}
	return "all"
}

func ConnectionGraph(db *gorm.DB, q GraphQuery) (*GraphDTO, error) {
	depth := q.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > maxGraphDepth {
		depth = maxGraphDepth
	}

	seed, err := graphSeeds(db, q)
	if err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		return &GraphDTO{Scope: GraphScopeKey(q), Nodes: []GraphNode{}, Edges: []GraphEdge{}}, nil
	}

	var conns []models.Connection
	if err := db.Find(&conns).Error; err != nil {
		return nil, err
	}
	adj := map[uint][]models.Connection{}
	for _, c := range conns {
		adj[c.DeviceAID] = append(adj[c.DeviceAID], c)
		adj[c.DeviceBID] = append(adj[c.DeviceBID], c)
	}

	inScope := map[uint]int{}
	queue := make([]uint, 0, len(seed))
	for id := range seed {
		inScope[id] = 0
		queue = append(queue, id)
	}
	// Neighbourhood mode expands from a seed device. Site/rack views keep
	// the selected inventory in-scope and draw one-hop cables as stubs.
	if q.DeviceID != 0 {
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			d := inScope[id]
			if d >= depth {
				continue
			}
			for _, c := range adj[id] {
				other := c.DeviceBID
				if other == id {
					other = c.DeviceAID
				}
				if _, ok := inScope[other]; ok {
					continue
				}
				inScope[other] = d + 1
				queue = append(queue, other)
			}
		}
	}

	truncated := false
	if len(inScope) > maxGraphDevices {
		truncated = true
		keep := make([]uint, 0, len(inScope))
		for id := range inScope {
			keep = append(keep, id)
		}
		sort.Slice(keep, func(i, j int) bool {
			if inScope[keep[i]] != inScope[keep[j]] {
				return inScope[keep[i]] < inScope[keep[j]]
			}
			return keep[i] < keep[j]
		})
		keep = keep[:maxGraphDevices]
		allowed := map[uint]int{}
		for _, id := range keep {
			allowed[id] = inScope[id]
		}
		inScope = allowed
	}

	external := map[uint]bool{}
	edges := make([]GraphEdge, 0)
	for _, c := range conns {
		_, aOK := inScope[c.DeviceAID]
		_, bOK := inScope[c.DeviceBID]
		if !aOK && !bOK {
			continue
		}
		if !aOK {
			external[c.DeviceAID] = true
		}
		if !bOK {
			external[c.DeviceBID] = true
		}
		unavail := c.DeviceAID == 0 || c.DeviceBID == 0 || c.InterfaceAID == 0 || c.InterfaceBID == 0
		edges = append(edges, GraphEdge{
			ID: c.ID, Label: c.Label,
			DeviceAID: c.DeviceAID, InterfaceAID: c.InterfaceAID,
			DeviceBID: c.DeviceBID, InterfaceBID: c.InterfaceBID,
			Unavailable: unavail,
		})
		if len(edges) >= maxGraphEdges {
			truncated = true
			break
		}
	}

	nodeIDs := make([]uint, 0, len(inScope)+len(external))
	for id := range inScope {
		nodeIDs = append(nodeIDs, id)
	}
	for id := range external {
		nodeIDs = append(nodeIDs, id)
	}
	var devices []models.Device
	if len(nodeIDs) > 0 {
		if err := db.Where("id IN ?", nodeIDs).Find(&devices).Error; err != nil {
			return nil, err
		}
	}
	devByID := map[uint]models.Device{}
	for _, d := range devices {
		devByID[d.ID] = d
	}

	usedIface := map[uint]bool{}
	for _, e := range edges {
		usedIface[e.InterfaceAID] = true
		usedIface[e.InterfaceBID] = true
	}
	var ifaces []models.Interface
	if len(nodeIDs) > 0 {
		if err := db.Select("id", "device_id", "name").Where("device_id IN ?", nodeIDs).Order("name").Find(&ifaces).Error; err != nil {
			return nil, err
		}
	}
	ports := map[uint][]GraphPort{}
	for _, i := range ifaces {
		if !usedIface[i.ID] {
			continue
		}
		ports[i.DeviceID] = append(ports[i.DeviceID], GraphPort{ID: i.ID, Name: i.Name})
	}

	placeByDev := map[uint]uint{}
	if len(nodeIDs) > 0 {
		var ps []models.DevicePlacement
		if err := db.Select("device_id", "rack_id").Where("device_id IN ?", nodeIDs).Find(&ps).Error; err != nil {
			return nil, err
		}
		for _, p := range ps {
			placeByDev[p.DeviceID] = p.RackID
		}
	}

	nodes := make([]GraphNode, 0, len(nodeIDs))
	seenNode := map[uint]bool{}
	addNode := func(id uint, ext bool) {
		if id == 0 || seenNode[id] {
			return
		}
		seenNode[id] = true
		d := devByID[id]
		name := d.Name
		if name == "" {
			name = fmt.Sprintf("device-%d", id)
		}
		nodes = append(nodes, GraphNode{
			ID: id, Name: name, External: ext, Site: d.Site, RackID: placeByDev[id],
			Interfaces: ports[id],
		})
	}
	for id := range inScope {
		addNode(id, false)
	}
	for id := range external {
		addNode(id, true)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	return &GraphDTO{
		Scope:     GraphScopeKey(q),
		Truncated: truncated,
		Nodes:     nodes,
		Edges:     edges,
	}, nil
}

func graphSeeds(db *gorm.DB, q GraphQuery) (map[uint]bool, error) {
	seed := map[uint]bool{}
	if q.DeviceID != 0 {
		var d models.Device
		if err := db.First(&d, q.DeviceID).Error; err != nil {
			return nil, errf(http.StatusNotFound, ReasonNotFound, "device not found")
		}
		seed[d.ID] = true
		return seed, nil
	}
	if q.RackID != 0 {
		var ps []models.DevicePlacement
		if err := db.Select("device_id").Where("rack_id = ?", q.RackID).Find(&ps).Error; err != nil {
			return nil, err
		}
		for _, p := range ps {
			seed[p.DeviceID] = true
		}
		return seed, nil
	}
	if q.SiteID != 0 {
		ids, err := DeviceIDsAtLocalSite(db, q.SiteID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			seed[id] = true
		}
		return seed, nil
	}
	return nil, errf(http.StatusBadRequest, ReasonInvalid, "site_id, rack_id or device_id is required")
}
