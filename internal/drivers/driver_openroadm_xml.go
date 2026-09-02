package drivers

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/optical"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

// orDeviceFilter is a NETCONF subtree filter selecting the whole
// org-openroadm-device tree (empty containment node = all children).
type orDeviceFilter struct {
	XMLName xml.Name `xml:"http://org/openroadm/device org-openroadm-device"`
}

type orDevice struct {
	XMLName        xml.Name          `xml:"http://org/openroadm/device org-openroadm-device"`
	Info           orInfo            `xml:"info"`
	CircuitPacks   []orCircuitPack   `xml:"circuit-packs"`
	Interfaces     []orInterface     `xml:"interface"`
	Degrees        []orDegree        `xml:"degree"`
	SRGs           []orSRG           `xml:"shared-risk-group"`
	LineAmps       []orLineAmp       `xml:"line-amplifier"`
	Xponders       []orXponder       `xml:"xponder"`
	Connections    []orConnection    `xml:"roadm-connections"`
	ODUConnections []orConnection    `xml:"odu-connection"`
	EthConnections []orConnection    `xml:"eth-connections"`
	ConnectionMap  []orConnectionMap `xml:"connection-map"`
	InternalLinks  []orPortLink      `xml:"internal-link"`
	ExternalLinks  []orExternalLink  `xml:"external-link"`
}

type orInfo struct {
	NodeID           string `xml:"node-id"`
	NodeType         string `xml:"node-type"`
	Vendor           string `xml:"vendor"`
	Model            string `xml:"model"`
	SerialID         string `xml:"serial-id"`
	MACAddress       string `xml:"macAddress"`
	SoftwareVersion  string `xml:"softwareVersion"`
	OpenROADMVersion string `xml:"openroadm-version"`
}

type orCircuitPack struct {
	CircuitPackName string   `xml:"circuit-pack-name"`
	Ports           []orPort `xml:"ports"`
}

type orPort struct {
	PortName            string `xml:"port-name"`
	PortQual            string `xml:"port-qual"`
	UserDescription     string `xml:"user-description"`
	AdministrativeState string `xml:"administrative-state"`
	OperationalState    string `xml:"operational-state"`
}

type orInterface struct {
	Name                      string    `xml:"name"`
	Description               string    `xml:"description"`
	Type                      string    `xml:"type"`
	AdministrativeState       string    `xml:"administrative-state"`
	OperationalState          string    `xml:"operational-state"`
	SupportingCircuitPackName string    `xml:"supporting-circuit-pack-name"`
	SupportingPort            string    `xml:"supporting-port"`
	NmcCtp                    orFreqBox `xml:"nmc-ctp"`
	Och                       orFreqBox `xml:"och"`
	Otsi                      orFreqBox `xml:"otsi"`
}

// orFreqBox is the frequency/width pair on NMC-CTP, OCH and OTSi augments.
// Local-name matching (no namespace in the tag) covers the several module
// namespaces those containers have used across MSA releases.
type orFreqBox struct {
	Frequency string `xml:"frequency"`
	Width     string `xml:"width"`
}

type orDegree struct {
	Number          uint16      `xml:"degree-number"`
	ConnectionPorts []orPortRef `xml:"connection-ports"`
	OTSPorts        []orPortRef `xml:"ots-port"`
}

type orSRG struct {
	Number       uint16       `xml:"srg-number"`
	CircuitPacks []orPackRef  `xml:"circuit-packs"`
	Subgroups    []orSubgroup `xml:"subgroup"`
}

type orSubgroup struct {
	ID       uint16      `xml:"subgroup-id"`
	PortList []orPortRef `xml:"port-list"`
}

type orLineAmp struct {
	Number    uint16      `xml:"amp-number"`
	LinePorts []orPortRef `xml:"line-port"`
	OTSPorts  []orPortRef `xml:"ots-port"`
}

type orXponder struct {
	Number uint16      `xml:"xpdr-number"`
	Type   string      `xml:"xpdr-type"`
	Ports  []orPortRef `xml:"xpdr-port"`
}

type orPackRef struct {
	CircuitPackName string `xml:"circuit-pack-name"`
}

type orPortRef struct {
	CircuitPackName string `xml:"circuit-pack-name"`
	PortName        string `xml:"port-name"`
}

type orConnection struct {
	Name             string `xml:"connection-name"`
	CircuitID        string `xml:"circuit-id"`
	WavelengthNumber uint32 `xml:"wavelength-number"`
	Source           struct {
		If string `xml:"src-if"`
	} `xml:"source"`
	Destination struct {
		If string `xml:"dst-if"`
	} `xml:"destination"`
}

type orConnectionMap struct {
	Number      uint32    `xml:"connection-map-number"`
	Source      orPortRef `xml:"source"`
	Destination orPortRef `xml:"destination"`
}

type orPortLink struct {
	Name        string    `xml:"internal-link-name"`
	Source      orPortRef `xml:"source"`
	Destination orPortRef `xml:"destination"`
}

type orExternalLink struct {
	Name        string   `xml:"external-link-name"`
	Source      orExtEnd `xml:"source"`
	Destination orExtEnd `xml:"destination"`
}

type orExtEnd struct {
	NodeID          string `xml:"node-id"`
	CircuitPackName string `xml:"circuit-pack-name"`
	PortName        string `xml:"port-name"`
}

type orPortKey struct {
	pack string
	port string
}

func parseORDevice(data []byte) (*orDevice, error) {
	var dev orDevice
	if err := xml.Unmarshal(data, &dev); err == nil {
		return &dev, nil
	}
	// Some agents wrap the tree in <data> (or a similar envelope) rather
	// than returning org-openroadm-device as the Get payload root.
	var wrap struct {
		Device orDevice `xml:"http://org/openroadm/device org-openroadm-device"`
	}
	if err := xml.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}
	return &wrap.Device, nil
}

func orPortName(pack, port string) string {
	switch {
	case pack == "":
		return port
	case port == "":
		return pack
	default:
		return pack + "/" + port
	}
}

func orLocalName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "}"); i >= 0 {
		return s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func parseTHz(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	thz, err := strconv.ParseFloat(s, 64)
	if err != nil || thz <= 0 {
		return 0
	}
	return optical.HzFromTHz(thz)
}

func orIfaceFreqHz(iface orInterface) uint64 {
	for _, s := range []string{iface.NmcCtp.Frequency, iface.Och.Frequency, iface.Otsi.Frequency} {
		if hz := parseTHz(s); hz != 0 {
			return hz
		}
	}
	return 0
}

// orToOCStatus maps Open ROADM admin/oper enumerations onto the
// openconfig-interfaces UP/DOWN vocabulary GetInterfacesStatus returns on
// every other NETCONF driver. degraded is still UP: the port is forwarding.
func orToOCStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "inservice", "degraded":
		if state == "" {
			return ""
		}
		return "UP"
	default:
		return "DOWN"
	}
}

func orOpticalKind(nodeType string) string {
	switch strings.ToLower(orLocalName(nodeType)) {
	case "rdm":
		return models.OpticalKindROADM
	case "xpdr", "extplug":
		return models.OpticalKindWDMShelf
	case "ila":
		return models.OpticalKindILA
	default:
		return ""
	}
}

func (dev *orDevice) ifaceByName() map[string]orInterface {
	out := make(map[string]orInterface, len(dev.Interfaces))
	for _, iface := range dev.Interfaces {
		if iface.Name != "" {
			out[iface.Name] = iface
		}
	}
	return out
}

func (dev *orDevice) freqsByPort() map[orPortKey]uint64 {
	out := make(map[orPortKey]uint64)
	mixed := make(map[orPortKey]bool)
	for _, iface := range dev.Interfaces {
		hz := orIfaceFreqHz(iface)
		if hz == 0 || iface.SupportingCircuitPackName == "" || iface.SupportingPort == "" {
			continue
		}
		k := orPortKey{iface.SupportingCircuitPackName, iface.SupportingPort}
		if mixed[k] {
			continue
		}
		if existing, ok := out[k]; ok && existing != hz {
			delete(out, k)
			mixed[k] = true
			continue
		}
		out[k] = hz
	}
	return out
}

func (dev *orDevice) opticalRoles() map[orPortKey]string {
	degree := map[orPortKey]bool{}
	for _, d := range dev.Degrees {
		for _, p := range d.ConnectionPorts {
			degree[orPortKey{p.CircuitPackName, p.PortName}] = true
		}
		for _, p := range d.OTSPorts {
			degree[orPortKey{p.CircuitPackName, p.PortName}] = true
		}
	}

	srgExact := map[orPortKey]bool{}
	srgPacks := map[string]bool{}
	for _, srg := range dev.SRGs {
		for _, p := range srg.CircuitPacks {
			srgPacks[p.CircuitPackName] = true
		}
		for _, sg := range srg.Subgroups {
			for _, p := range sg.PortList {
				if p.PortName == "" {
					srgPacks[p.CircuitPackName] = true
					continue
				}
				srgExact[orPortKey{p.CircuitPackName, p.PortName}] = true
			}
		}
	}

	xpdrNet := map[orPortKey]bool{}
	for _, x := range dev.Xponders {
		for _, p := range x.Ports {
			xpdrNet[orPortKey{p.CircuitPackName, p.PortName}] = true
		}
	}

	ila := map[orPortKey]bool{}
	for _, a := range dev.LineAmps {
		for _, p := range a.LinePorts {
			ila[orPortKey{p.CircuitPackName, p.PortName}] = true
		}
		for _, p := range a.OTSPorts {
			ila[orPortKey{p.CircuitPackName, p.PortName}] = true
		}
	}

	roles := map[orPortKey]string{}
	for _, pack := range dev.CircuitPacks {
		for _, port := range pack.Ports {
			k := orPortKey{pack.CircuitPackName, port.PortName}
			qual := strings.ToLower(orLocalName(port.PortQual))
			switch {
			case degree[k]:
				roles[k] = models.PortROADMDegree
			case srgExact[k]:
				roles[k] = models.PortROADMAddDrop
			case srgPacks[pack.CircuitPackName] && qual == "roadm-external" && !degree[k]:
				roles[k] = models.PortROADMAddDrop
			case qual == "xpdr-client" || qual == "switch-client":
				roles[k] = models.PortTXPClient
			case qual == "xpdr-network" || qual == "switch-network" || xpdrNet[k]:
				roles[k] = models.PortTXPLine
			case qual == "ila-external" || ila[k]:
				roles[k] = models.PortFiber
			}
		}
	}
	return roles
}

func orXConnectKind(roleA, roleB string) string {
	addDrop := func(r string) bool { return r == models.PortROADMAddDrop }
	degree := func(r string) bool { return r == models.PortROADMDegree }
	client := func(r string) bool { return r == models.PortTXPClient }
	line := func(r string) bool { return r == models.PortTXPLine }
	switch {
	case (addDrop(roleA) && degree(roleB)) || (degree(roleA) && addDrop(roleB)):
		return models.XCAddDrop
	case degree(roleA) && degree(roleB):
		return models.XCExpress
	case client(roleA) || client(roleB) || (line(roleA) && line(roleB)):
		return models.XCTributary
	default:
		return models.XCPassthrough
	}
}

func versionFromORDevice(dev *orDevice) *VersionModel {
	return &VersionModel{
		ModelName:        dev.Info.Model,
		SerialNumber:     dev.Info.SerialID,
		Version:          dev.Info.SoftwareVersion,
		SystemMacAddress: dev.Info.MACAddress,
		InternalVersion:  dev.Info.OpenROADMVersion,
	}
}

func interfacesFromORDevice(dev *orDevice) []*netboxtool.NBInterface {
	var out []*netboxtool.NBInterface
	for _, pack := range dev.CircuitPacks {
		for _, port := range pack.Ports {
			out = append(out, &netboxtool.NBInterface{
				Name:               orPortName(pack.CircuitPackName, port.PortName),
				Description:        port.UserDescription,
				InterfaceStatus:    orToOCStatus(port.AdministrativeState),
				LineProtocolStatus: orToOCStatus(port.OperationalState),
			})
		}
	}
	return out
}

func deviceConfigFromORDevice(dev *orDevice) *DeviceConfig {
	dc := NewDeviceConfig()
	for _, pack := range dev.CircuitPacks {
		for _, port := range pack.Ports {
			dc.AddInterface(&Interface{
				Name:        orPortName(pack.CircuitPackName, port.PortName),
				Description: port.UserDescription,
			})
		}
	}
	return dc
}

func neighborsFromORDevice(dev *orDevice) []*Neighbor {
	self := dev.Info.NodeID
	var out []*Neighbor
	for _, link := range dev.ExternalLinks {
		local, remote := link.Source, link.Destination
		if self != "" && local.NodeID != "" && local.NodeID != self {
			local, remote = remote, local
		}
		out = append(out, &Neighbor{
			LocalInterface:  orPortName(local.CircuitPackName, local.PortName),
			RemoteName:      remote.NodeID,
			RemoteInterface: orPortName(remote.CircuitPackName, remote.PortName),
		})
	}
	return out
}

func inventoryFromORDevice(dev *orDevice) *OpticalInventory {
	roles := dev.opticalRoles()
	freqs := dev.freqsByPort()
	ifaces := dev.ifaceByName()

	inv := &OpticalInventory{
		NodeID:           dev.Info.NodeID,
		NodeType:         orLocalName(dev.Info.NodeType),
		OpticalKind:      orOpticalKind(dev.Info.NodeType),
		Vendor:           dev.Info.Vendor,
		Model:            dev.Info.Model,
		Serial:           dev.Info.SerialID,
		SoftwareVersion:  dev.Info.SoftwareVersion,
		OpenROADMVersion: dev.Info.OpenROADMVersion,
	}

	for _, pack := range dev.CircuitPacks {
		for _, port := range pack.Ports {
			k := orPortKey{pack.CircuitPackName, port.PortName}
			role, ok := roles[k]
			if !ok {
				continue
			}
			inv.Ports = append(inv.Ports, OpticalPortView{
				Name:        orPortName(pack.CircuitPackName, port.PortName),
				CircuitPack: pack.CircuitPackName,
				PortName:    port.PortName,
				Role:        role,
				Description: port.UserDescription,
				AdminStatus: orToOCStatus(port.AdministrativeState),
				OperStatus:  orToOCStatus(port.OperationalState),
				FreqHz:      freqs[k],
				Qual:        orLocalName(port.PortQual),
			})
		}
	}

	addConn := func(name, circuitID, ifA, ifB string) {
		ia, okA := ifaces[ifA]
		ib, okB := ifaces[ifB]
		if !okA || !okB {
			return
		}
		portA := orPortName(ia.SupportingCircuitPackName, ia.SupportingPort)
		portB := orPortName(ib.SupportingCircuitPackName, ib.SupportingPort)
		if portA == "" || portB == "" {
			return
		}
		roleA := roles[orPortKey{ia.SupportingCircuitPackName, ia.SupportingPort}]
		roleB := roles[orPortKey{ib.SupportingCircuitPackName, ib.SupportingPort}]
		hz := orIfaceFreqHz(ia)
		if hz == 0 {
			hz = orIfaceFreqHz(ib)
		}
		inv.XConnects = append(inv.XConnects, OpticalXConnectView{
			Name:      name,
			Kind:      orXConnectKind(roleA, roleB),
			PortA:     portA,
			PortB:     portB,
			FreqHz:    hz,
			CircuitID: circuitID,
		})
	}

	for _, c := range dev.Connections {
		addConn(c.Name, c.CircuitID, c.Source.If, c.Destination.If)
	}
	for _, c := range dev.ODUConnections {
		addConn(c.Name, c.CircuitID, c.Source.If, c.Destination.If)
	}
	for _, c := range dev.EthConnections {
		addConn(c.Name, c.CircuitID, c.Source.If, c.Destination.If)
	}

	for _, m := range dev.ConnectionMap {
		portA := orPortName(m.Source.CircuitPackName, m.Source.PortName)
		portB := orPortName(m.Destination.CircuitPackName, m.Destination.PortName)
		if portA == "" || portB == "" {
			continue
		}
		roleA := roles[orPortKey{m.Source.CircuitPackName, m.Source.PortName}]
		roleB := roles[orPortKey{m.Destination.CircuitPackName, m.Destination.PortName}]
		name := m.Source.CircuitPackName + "/" + m.Source.PortName
		if m.Number != 0 {
			name = strconv.FormatUint(uint64(m.Number), 10)
		}
		hz := freqs[orPortKey{m.Source.CircuitPackName, m.Source.PortName}]
		if hz == 0 {
			hz = freqs[orPortKey{m.Destination.CircuitPackName, m.Destination.PortName}]
		}
		inv.XConnects = append(inv.XConnects, OpticalXConnectView{
			Name:   name,
			Kind:   orXConnectKind(roleA, roleB),
			PortA:  portA,
			PortB:  portB,
			FreqHz: hz,
		})
	}

	for _, link := range dev.InternalLinks {
		portA := orPortName(link.Source.CircuitPackName, link.Source.PortName)
		portB := orPortName(link.Destination.CircuitPackName, link.Destination.PortName)
		if portA == "" || portB == "" {
			continue
		}
		inv.XConnects = append(inv.XConnects, OpticalXConnectView{
			Name:  link.Name,
			Kind:  models.XCPassthrough,
			PortA: portA,
			PortB: portB,
		})
	}

	return inv
}
