package drivers

// Nokia SR OS driver - see README-DRIVERS.md for how this driver works
// (NETCONF reads over OpenConfig, native nokia-conf writes,
// candidate+lock+commit) and how it compares to the others.

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/netboxtool"
)

// srosConfigEndMarker matches the outermost object's closing "}" line that
// ends SR OS's pretty-printed "show configuration json" dump, right before
// the prompt reappears - see sshCmd.EndMarker. Anchored to
// zero indentation so it only matches that final brace, not any of the many
// nested objects' closing braces throughout the JSON.
var srosConfigEndMarker = regexp.MustCompile(`^}$`)

// ----------------------------------------------------------------------
// Device
// ----------------------------------------------------------------------

type NokiaDriver struct {
	DriverClient
	p DriverParam
}

func init() {
	factory := func(p DriverParam) (DriverClient, error) { return NewNokiaDriver(p) }
	registerDriver("sros", factory)
	registerDriver("sros-md", factory)
}

func NewNokiaDriver(p DriverParam) (*NokiaDriver, error) {
	if err := validateDriverParam(p); err != nil {
		return nil, err
	}
	return &NokiaDriver{p: p}, nil
}

// ----------------------------------------------------------------------
// External methods
// ----------------------------------------------------------------------

// SR OS's NETCONF interface has no RPC to run an arbitrary CLI command
// (that's Arista eAPI's runCmds), so like RunningConfigGet this goes over
// SSH instead. Only the paging-disable step is forced through the classic
// CLI engine via "//" - cmd itself is sent as-is, since it's the caller's
// choice of command/engine, not ours to override.
func (driver *NokiaDriver) Exec(cmd string) (*ExecModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "//environment no more"}, {Cmd: cmd}})
	if err != nil {
		return nil, err
	}
	return &ExecModel{Result: output}, nil
}

func (driver *NokiaDriver) Version() (*VersionModel, error) {
	data, err := netconfGet(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, ocComponentsFilter{})
	if err != nil {
		return nil, err
	}

	var reply ocComponentsData
	if err := xml.Unmarshal(data, &reply); err != nil {
		return nil, err
	}

	// The chassis component carries model/serial/hardware-revision but no
	// software-version (confirmed against a real 7250 IXR: that leaf is
	// simply absent from CHASSIS's <state>) - software-version instead
	// lives on a separate "system-software" component of type
	// OPERATING_SYSTEM, so the two have to be merged from separate loop
	// iterations rather than read off one component.
	var version VersionModel
	var foundChassis bool
	for _, component := range reply.Components {
		switch {
		case strings.Contains(component.State.Type, "CHASSIS"):
			foundChassis = true
			// MemTotal/MemFree/BootupTimestap/Architecture/
			// InternalBuildId/SystemMacAddress have no
			// openconfig-platform chassis equivalent, so they're
			// left zero-valued rather than guessed.
			version.ModelName = component.State.PartNo
			version.SerialNumber = component.State.SerialNo
			version.HardwareRevision = component.State.HardwareVersion
		case strings.Contains(component.State.Type, "OPERATING_SYSTEM"):
			version.Version = component.State.SoftwareVersion
		}
	}
	if !foundChassis {
		return nil, errors.New("no chassis component found in NETCONF response")
	}
	return &version, nil
}

func (driver *NokiaDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "//environment no more"}, {Cmd: "//admin display-config", EndMarker: srosConfigEndMarker}})
	if err != nil {
		return nil, err
	}
	return &RunningConfigModel{ConfigStr: output}, nil
}

// SR OS's <commit> (see SetInterfaceDescriptions) lands directly in the
// running datastore - there's no separate startup datastore involved at the
// NETCONF layer, so there is no separate "save running as startup" step.
// Persisting running config across a reboot is a distinct, unrelated step
// on SR OS (MD-CLI "admin save"), which this driver doesn't implement.
func (driver *NokiaDriver) RunningConfigSave() error {
	return errors.New("RunningConfigSave is not supported for Nokia SR OS: config is committed directly to the running datastore over NETCONF, with no separate startup-save step")
}

// GetInterfacesStatus returns the operational state of every interface.
func (driver *NokiaDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	data, err := netconfGet(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, ocInterfacesFilter{})
	if err != nil {
		return nil, err
	}

	var reply ocInterfacesData
	if err := xml.Unmarshal(data, &reply); err != nil {
		return nil, err
	}
	return interfacesFromOcData(reply), nil
}

func (driver *NokiaDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func (driver *NokiaDriver) SetInterfaceDescriptions(name []string, description []*netboxtool.NBInterface) error {
	if err := checkNamesMatchInterfaces(name, description); err != nil {
		return err
	}
	descriptions := make([]string, len(description))
	for ix, intf := range description {
		descriptions[ix] = intf.Description
	}
	edit := newNokiaEditPortDescriptions(name, descriptions)
	return netconfEditConfigCandidate(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, edit)
}

// SetInterfaceVLANs is unsupported on SR OS - it has no per-interface
// global-VLAN concept (see Interface.SwitchportMode's doc comment); every
// VLAN-tagged construct on this platform is a SAP scoped inside a service
// (epipe/vpls/vprn), not a standalone interface property. The web layer's
// "global VLAN platforms" gate is meant to keep this from ever being called.
func (driver *NokiaDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	return errors.New("interface VLAN configuration is not supported on sros")
}

// nokiaEditPorts is an edit-config payload against Nokia's native "conf"
// YANG model (urn:nokia.com:sros:ns:yang:sr:conf, module nokia-conf) - the
// same tree the classic/MD-CLI "configure" command walks, e.g. "configure
// port 1/1/1 description ...". Unlike openconfig-interfaces (see
// README-DRIVERS.md for why that doesn't work here), setting "description"
// on an existing port entry needs no other mandatory fields.
type nokiaEditPorts struct {
	XMLName xml.Name        `xml:"urn:nokia.com:sros:ns:yang:sr:conf configure"`
	Port    []nokiaEditPort `xml:"port"`
}

type nokiaEditPort struct {
	PortID      string `xml:"port-id"`
	Description string `xml:"description"`
}

func newNokiaEditPortDescriptions(names []string, descriptions []string) nokiaEditPorts {
	edit := nokiaEditPorts{Port: make([]nokiaEditPort, len(names))}
	for i := range names {
		edit.Port[i] = nokiaEditPort{PortID: names[i], Description: descriptions[i]}
	}
	return edit
}

// ----------------------------------------------------------------------
// GetDeviceConfig / GetNeighbors - parse the MD-CLI JSON running config and
// LLDP neighbors into the shared drivers.DeviceConfig/Neighbor shapes, for
// internal/device-sync.
// ----------------------------------------------------------------------

// srosMap/srosSlice/srosStr/srosInt are best-effort accessors into the
// generic map[string]any tree json.Unmarshal produces for SR OS's MD-CLI
// "show configuration json" output - every field is optional depending on
// what's actually configured, so a missing/wrong-typed key just yields a
// zero value rather than a panic or an error.
func srosMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func srosSlice(m map[string]any, key string) []any {
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

func srosStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func srosInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// srosConfig fetches the MD-CLI JSON running config and returns the
// "nokia-conf:configure" subtree.
func (driver *NokiaDriver) srosConfig() (map[string]any, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{
		{Cmd: "//environment no more"},
		{Cmd: "/admin show configuration json | no-more", EndMarker: srosConfigEndMarker},
	})
	if err != nil {
		return nil, err
	}
	// output is raw SSH terminal capture, not just the JSON: it's prefixed
	// with the device's echo of the command itself and suffixed with the
	// prompt reappearing after the final "}" line, so the actual JSON
	// object has to be sliced out before unmarshaling.
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end < start {
		return nil, errors.New("srosConfig: no JSON object found in output")
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(output[start:end+1]), &root); err != nil {
		return nil, err
	}
	return srosMap(root, "nokia-conf:configure"), nil
}

// srosAddIPv4/srosAddIPv6 append a router/vprn interface's configured
// address, built from separate address/prefix-length fields.
func srosAddIPv4(iface *Interface, ipv4 map[string]any) {
	primary := srosMap(ipv4, "primary")
	if primary == nil {
		return
	}
	addr, err := netip.ParseAddr(srosStr(primary, "address"))
	if err != nil {
		return
	}
	iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{
		Address: netip.PrefixFrom(addr, srosInt(primary, "prefix-length")),
		VRF:     iface.VRF,
	})
}

func srosAddIPv6(iface *Interface, ipv6 map[string]any) {
	for _, a := range srosSlice(ipv6, "address") {
		entry, ok := a.(map[string]any)
		if !ok {
			continue
		}
		addr, err := netip.ParseAddr(srosStr(entry, "ipv6-address"))
		if err != nil {
			continue
		}
		iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{
			Address: netip.PrefixFrom(addr, srosInt(entry, "prefix-length")),
			VRF:     iface.VRF,
		})
	}
}

// srosParsePorts parses physical ports, with no VRF/address (those only
// apply to router interfaces). Type is "other" (Netbox's generic physical
// type; the exact PHY/media isn't derivable from config alone) rather than
// left empty - Netbox rejects terminating a cable on an interface with
// no/"virtual" type, so an unset Type here would silently break
// syncConnections for any port device-sync itself creates.
func srosParsePorts(dc *DeviceConfig, configure map[string]any) {
	for _, p := range srosSlice(configure, "port") {
		port, ok := p.(map[string]any)
		if !ok {
			continue
		}
		dc.AddInterface(&Interface{
			Name:        srosStr(port, "port-id"),
			Description: srosStr(port, "description"),
			Type:        "other",
		})
	}
}

// srosParseRouterInterfaces parses the global RIB's interfaces, each
// optionally carrying an IPv4/IPv6 address.
func srosParseRouterInterfaces(dc *DeviceConfig, configure map[string]any) {
	routers := srosSlice(configure, "router")
	if len(routers) == 0 {
		return
	}
	router, ok := routers[0].(map[string]any)
	if !ok {
		return
	}
	for _, ri := range srosSlice(router, "interface") {
		r, ok := ri.(map[string]any)
		if !ok {
			continue
		}
		iface := &Interface{Name: srosStr(r, "interface-name"), Type: "virtual"}
		if ipv4 := srosMap(r, "ipv4"); ipv4 != nil {
			srosAddIPv4(iface, ipv4)
		}
		if ipv6 := srosMap(r, "ipv6"); ipv6 != nil {
			srosAddIPv6(iface, ipv6)
		}
		dc.AddInterface(iface)
	}
}

// srosAddSapInterface registers a SAP id (e.g. "1/1/1:3704.*") as an
// Interface in its own right, named exactly as SR OS reports it. Without
// this, a SAP has no corresponding entry in
// InterfacesByName, so device-sync's interfacesDelete (which deletes any
// Netbox interface not present in InterfacesByName) removes the Netbox-side
// interface admins create as a substitute for a SAP - SR OS has no real
// subinterfaces, so that Netbox interface is the only place the SAP is
// represented at all. Type is "virtual" (SAPs are L2 service bindings, never
// something a cable terminates on) and it carries no addresses, matching a
// SAP's actual role. Parent is the physical port the SAP id is bound to
// (the part before the first ":", e.g. "1/1/1" out of "1/1/1:3704.*" -
// always the same port-id srosParsePorts already registers), and Label is
// always "SAP" - both make the Netbox-side interface read as what it
// actually represents rather than a real subinterface. A SAP already
// registered (its id repeated verbatim across the eline/elan tables) is
// left as-is rather than re-added.
func srosAddSapInterface(dc *DeviceConfig, sapID string) {
	if sapID == "" {
		return
	}
	if _, exists := dc.InterfacesByName[sapID]; exists {
		return
	}
	parent := sapID
	if idx := strings.Index(sapID, ":"); idx >= 0 {
		parent = sapID[:idx]
	}
	dc.AddInterface(&Interface{Name: sapID, Type: "virtual", Parent: parent, Label: "SAP"})
}

// srosParseEline parses epipe services, each with SAP connections and/or
// an SDP-bound pseudowire connection resolved against the service's own
// sdp table.
func srosParseEline(dc *DeviceConfig, configure map[string]any) {
	services := srosMap(configure, "service")
	if services == nil {
		return
	}
	sdpByID := map[int]map[string]any{}
	for _, s := range srosSlice(services, "sdp") {
		sdp, ok := s.(map[string]any)
		if !ok {
			continue
		}
		sdpByID[srosInt(sdp, "sdp-id")] = sdp
	}

	for _, e := range srosSlice(services, "epipe") {
		epipe, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name := srosStr(epipe, "service-name")
		eline := &ELINE{Name: name, Description: srosStr(epipe, "description")}
		addConn := func(conn any) {
			if eline.Conn1 == nil {
				eline.Conn1 = conn
			} else {
				eline.Conn2 = conn
			}
		}
		for _, s := range srosSlice(epipe, "sap") {
			if sap, ok := s.(map[string]any); ok {
				sapID := srosStr(sap, "sap-id")
				addConn(sapID)
				srosAddSapInterface(dc, sapID)
			}
		}
		for _, s := range srosSlice(epipe, "spoke-sdp") {
			spokeSdp, ok := s.(map[string]any)
			if !ok {
				continue
			}
			parts := strings.SplitN(srosStr(spokeSdp, "sdp-bind-id"), ":", 2)
			if len(parts) != 2 {
				continue
			}
			sdpNo, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			pw := &Pseudowire{Name: name}
			fmtSscanInt(parts[1], &pw.PWID)
			if sdp, ok := sdpByID[sdpNo]; ok {
				pw.Neighbor = srosStr(srosMap(sdp, "far-end"), "ip-address")
			}
			addConn(pw)
		}
		dc.ELINEs[eline.Name] = eline
	}
}

// srosParseElan parses vpls services.
func srosParseElan(dc *DeviceConfig, configure map[string]any) {
	services := srosMap(configure, "service")
	if services == nil {
		return
	}
	for _, v := range srosSlice(services, "vpls") {
		vpls, ok := v.(map[string]any)
		if !ok {
			continue
		}
		elan := &ELAN{Name: srosStr(vpls, "service-name"), Description: srosStr(vpls, "description")}
		if bgpList := srosSlice(vpls, "bgp"); len(bgpList) > 0 {
			if bgp, ok := bgpList[0].(map[string]any); ok {
				elan.RD = srosStr(bgp, "route-distinguisher")
				rt := srosMap(bgp, "route-target")
				elan.RTImport = srosStr(rt, "import")
				elan.RTExport = srosStr(rt, "export")
			}
		}
		for _, s := range srosSlice(vpls, "sap") {
			if sap, ok := s.(map[string]any); ok {
				sapID := srosStr(sap, "sap-id")
				elan.Interfaces = append(elan.Interfaces, sapID)
				srosAddSapInterface(dc, sapID)
			}
		}
		dc.ELANs[elan.Name] = elan
	}
}

// srosParseL3VPN parses vprn services. The VRF built here is only ever
// attached to its L3VPN, never registered in DeviceConfig.VRFs.
func srosParseL3VPN(dc *DeviceConfig, configure map[string]any) {
	services := srosMap(configure, "service")
	if services == nil {
		return
	}
	for _, v := range srosSlice(services, "vprn") {
		service, ok := v.(map[string]any)
		if !ok {
			continue
		}
		name := srosStr(service, "service-name")
		vrfName := name
		if fields := strings.Fields(name); len(fields) > 0 {
			vrfName = fields[0]
		}
		vrf := &VRF{Name: vrfName}
		if bgpIPVPN := srosMap(service, "bgp-ipvpn"); bgpIPVPN != nil {
			if mpls := srosMap(bgpIPVPN, "mpls"); mpls != nil {
				vrf.RD = srosStr(mpls, "route-distinguisher")
				if vt := srosMap(mpls, "vrf-target"); vt != nil {
					community := srosStr(vt, "community")
					parts := strings.SplitN(community, ":", 2)
					if len(parts) == 2 {
						vrf.RTImport, vrf.RTExport = parts[1], parts[1]
					}
				}
			}
		}

		l3vpn := &L3VPN{Name: name, VRF: vrf}
		for _, i := range srosSlice(service, "interface") {
			r, ok := i.(map[string]any)
			if !ok {
				continue
			}
			iface := &Interface{Name: srosStr(r, "interface-name"), Type: "virtual", VRF: vrfName}
			if ipv4 := srosMap(r, "ipv4"); ipv4 != nil {
				srosAddIPv4(iface, ipv4)
			}
			if ipv6 := srosMap(r, "ipv6"); ipv6 != nil {
				srosAddIPv6(iface, ipv6)
			}
			vrf.Interfaces = append(vrf.Interfaces, iface.Name)
			dc.AddInterface(iface)
		}
		dc.L3VPNs[l3vpn.Name] = l3vpn
	}
}

// GetDeviceConfig fetches and parses the device's MD-CLI JSON running
// config.
func (driver *NokiaDriver) GetDeviceConfig() (*DeviceConfig, error) {
	configure, err := driver.srosConfig()
	if err != nil {
		return nil, err
	}
	dc := NewDeviceConfig()
	srosParsePorts(dc, configure)
	srosParseRouterInterfaces(dc, configure)
	srosParseEline(dc, configure)
	srosParseElan(dc, configure)
	srosParseL3VPN(dc, configure)
	return dc, nil
}

// srosNextNonBlankLine advances *idx past lines[*idx] to the next
// non-blank line - returns "", false once lines is exhausted.
func srosNextNonBlankLine(lines []string, idx *int) (string, bool) {
	for *idx < len(lines) {
		line := lines[*idx]
		*idx++
		if strings.TrimSpace(line) == "" {
			continue
		}
		return line, true
	}
	return "", false
}

// GetNeighbors runs "show port * ethernet lldp remote-info" and block-
// parses it - each neighbor starts at a "Port 1..." line, and "Port Id"'s
// real value (unlike every other field) is on the line *after* its header,
// quoted.
func (driver *NokiaDriver) GetNeighbors() ([]*Neighbor, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{
		{Cmd: "//environment no more"},
		{Cmd: "show port * ethernet lldp remote-info"},
	})
	if err != nil {
		return nil, err
	}
	return srosParseNeighbors(output), nil
}

// srosParseNeighbors block-parses "show port * ethernet lldp remote-info" -
// each neighbor starts at a "Port 1..." line, and "Port Id"'s real value
// (unlike every other field) is on the line *after* its header, quoted.
func srosParseNeighbors(output string) []*Neighbor {
	lines := strings.Split(output, "\n")
	idx := 0
	var neighbors []*Neighbor
	n := &Neighbor{}
	for {
		line, ok := srosNextNonBlankLine(lines, &idx)
		if !ok {
			break
		}
		if strings.HasPrefix(line, "Port 1") {
			if n.RemoteName != "" {
				neighbors = append(neighbors, n)
			}
			n = &Neighbor{}
			if fields := strings.Fields(line); len(fields) > 1 {
				n.LocalInterface = fields[1]
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		switch strings.TrimSpace(parts[0]) {
		case "Port Id":
			if valLine, ok := srosNextNonBlankLine(lines, &idx); ok {
				n.RemoteInterface = strings.Trim(strings.TrimSpace(valLine), `"`)
			}
		case "System Name":
			if len(parts) > 1 {
				n.RemoteName = strings.TrimSpace(parts[1])
			}
		}
	}
	if n.RemoteName != "" {
		neighbors = append(neighbors, n)
	}
	return neighbors
}
