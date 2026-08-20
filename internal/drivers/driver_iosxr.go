package drivers

// Cisco IOS-XR driver - see README-DRIVERS.md for how this driver works
// (NETCONF candidate+commit, SSH CLI fallback) and how it compares to the
// others.

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/netboxtool"
)

// ----------------------------------------------------------------------
// Device
// ----------------------------------------------------------------------

type IOSXRDriver struct {
	DriverClient
	p DriverParam
}

func init() {
	registerDriver("ios-xr", func(p DriverParam) (DriverClient, error) { return NewIosXRDriver(p) })
}

func NewIosXRDriver(p DriverParam) (*IOSXRDriver, error) {
	if err := validateDriverParam(p); err != nil {
		return nil, err
	}
	return &IOSXRDriver{p: p}, nil
}

// iosxrConfigEndMarker matches the lone "end" line IOS-XR prints at the end
// of a "show running-config" dump, right before the prompt reappears - see
// sshCmd.EndMarker.
var iosxrConfigEndMarker = regexp.MustCompile(`^end$`)

// ----------------------------------------------------------------------
// External methods
// ----------------------------------------------------------------------

// run command on device over classic CLI, return output.
func (driver *IOSXRDriver) Exec(cmd string) (*ExecModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "terminal length 0"}, {Cmd: cmd}})
	if err != nil {
		return nil, err
	}
	return &ExecModel{Result: output}, nil
}

// Version reads the chassis component's serial/hardware/software fields via
// NETCONF against openconfig-platform.
func (driver *IOSXRDriver) Version() (*VersionModel, error) {
	data, err := netconfGet(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, ocComponentsFilter{})
	if err != nil {
		return nil, err
	}

	var reply ocComponentsData
	if err := xml.Unmarshal(data, &reply); err != nil {
		return nil, err
	}

	for _, component := range reply.Components {
		if !strings.Contains(component.State.Type, "CHASSIS") {
			continue
		}
		// MemTotal/MemFree/BootupTimestap/Architecture/InternalBuildId/
		// SystemMacAddress have no openconfig-platform chassis
		// equivalent, so they're left zero-valued rather than guessed.
		return &VersionModel{
			ModelName:        component.State.PartNo,
			SerialNumber:     component.State.SerialNo,
			HardwareRevision: component.State.HardwareVersion,
			Version:          component.State.SoftwareVersion,
		}, nil
	}
	return nil, errors.New("no chassis component found in NETCONF response")
}

// RunningConfigGet always returns CLI text (jsonformat is ignored) - NETCONF
// has no RPC that returns config the way an operator would read it (as CLI
// script), only the YANG tree as XML, so this goes over SSH CLI instead.
func (driver *IOSXRDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "terminal length 0"}, {Cmd: "show running-config", EndMarker: iosxrConfigEndMarker}})
	if err != nil {
		return nil, err
	}
	return &RunningConfigModel{ConfigStr: output}, nil
}

// RunningConfigSave has no XR equivalent: SetInterfaceDescriptions already
// commits straight to the running datastore, which persists across reload
// on its own - there's no separate "save" step to run.
func (driver *IOSXRDriver) RunningConfigSave() error {
	return errors.New("RunningConfigSave is not supported for IOS-XR: NETCONF <commit> already applies to and persists the running datastore")
}

// GetInterfacesStatus returns the operational state of every interface via
// NETCONF against openconfig-interfaces.
func (driver *IOSXRDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
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

// ----------------------------------------------------------------------
// Update interface description(s) via NETCONF edit-config against the
// candidate datastore, then commit - see netconfEditConfigCandidate.
// ----------------------------------------------------------------------

func (driver *IOSXRDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func (driver *IOSXRDriver) SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error {
	if err := checkNamesMatchInterfaces(name, intf); err != nil {
		return err
	}
	descriptions := make([]string, len(intf))
	for ix, i := range intf {
		descriptions[ix] = i.Description
	}
	edit := newOcEditInterfaces(name, descriptions)
	return netconfEditConfigCandidate(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, edit)
}

// SetInterfaceVLANs is unsupported on IOS-XR - it has no per-interface
// global-VLAN concept (see Interface.SwitchportMode's doc comment), only
// bridge-domain/EVI-scoped L2, which the web layer's "global VLAN
// platforms" gate is meant to keep this from ever being called for.
func (driver *IOSXRDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	return errors.New("interface VLAN configuration is not supported on ios-xr")
}

// ----------------------------------------------------------------------
// GetDeviceConfig / GetNeighbors - parse running config (via
// config_context.go's hierarchical parser) and LLDP neighbors into the
// shared drivers.DeviceConfig/Neighbor shapes, for internal/device-sync.
// ----------------------------------------------------------------------

// iosxrInterfaceCfg is one "interface X [l2transport]" block, tag-bound by
// tagparse.go's engine - see the type-level comment there for the DSL.
// Replaces what used to be ten package-level regexps
// (iosxrReDescription/iosxrReBundleID/...) plus a hand-written switch.
type iosxrInterfaceCfg struct {
	Description string           `cfg:"description {:toend}"`
	BundleID    string           `cfg:"bundle id {}"`
	VRF         string           `cfg:"vrf {}"`
	VlanID      string           `cfg:"vlan id {}"`
	IPv4        *iosxrIPv4Prefix `cfg:"ipv4 address {} {}"`
	IPv6        *netip.Prefix    `cfg:"ipv6 address {}"`
	L2Transport bool             `cfg:"l2transport"`
}

// iosxrIPv4Prefix is a leaf type for "ipv4 address A.B.C.D W.X.Y.Z" - two
// words on one line (a dotted-decimal mask, not CIDR), joined with a space
// by the tag engine before being handed to UnmarshalText.
type iosxrIPv4Prefix netip.Prefix

func (p *iosxrIPv4Prefix) UnmarshalText(text []byte) error {
	fields := strings.Fields(string(text))
	if len(fields) != 2 {
		return fmt.Errorf(`want "address mask", got %q`, text)
	}
	prefix, ok := iosxrParseIPv4WithMask(fields[0], fields[1])
	if !ok {
		return fmt.Errorf("invalid ipv4 address/mask %q", text)
	}
	*p = iosxrIPv4Prefix(prefix)
	return nil
}

// iosxrNeighborPW is a p2p pseudowire leg's "neighbor ipv4 <ip> pw-id <id>"
// line - two words joined by the tag engine before UnmarshalText.
type iosxrNeighborPW struct {
	Neighbor string
	PWID     int
}

func (n *iosxrNeighborPW) UnmarshalText(text []byte) error {
	fields := strings.Fields(string(text))
	if len(fields) != 2 {
		return fmt.Errorf(`want "ip pw-id", got %q`, text)
	}
	pwid, err := strconv.Atoi(fields[1])
	if err != nil {
		return err
	}
	n.Neighbor, n.PWID = fields[0], pwid
	return nil
}

// iosxrP2PCfg is one l2vpn xconnect group's p2p block: an interface leg and
// a pseudowire leg.
type iosxrP2PCfg struct {
	Interface string           `cfg:"interface {}"`
	Neighbor  *iosxrNeighborPW `cfg:"neighbor ipv4 {} pw-id {}"`
}

type iosxrXconnectGroupCfg struct {
	P2P map[string]iosxrP2PCfg `cfg:"p2p {}"`
}

type iosxrBridgeDomainCfg struct {
	Interfaces []string `cfg:"interface {}"`
	EVI        string   `cfg:"evi {}"`
}

type iosxrBridgeGroupCfg struct {
	BridgeDomains map[string]iosxrBridgeDomainCfg `cfg:"bridge-domain {}"`
}

type iosxrL2VPNCfg struct {
	XconnectGroups map[string]iosxrXconnectGroupCfg `cfg:"xconnect group {}"`
	BridgeGroups   map[string]iosxrBridgeGroupCfg   `cfg:"bridge group {}"`
}

type iosxrEVIBGPCfg struct {
	RD       string `cfg:"rd {}"`
	RTImport string `cfg:"route-target import {}"`
	RTExport string `cfg:"route-target export {}"`
}

type iosxrEVICfg struct {
	BGP *iosxrEVIBGPCfg `cfg:"bgp"`
}

type iosxrEVPNCfg struct {
	EVIs map[string]iosxrEVICfg `cfg:"evi {}"`
}

type iosxrBGPVRFCfg struct {
	RD string `cfg:"rd {}"`
}

// iosxrRouterBGPCfg is only used for the "router bgp <asn>" -> "vrf <name>"
// -> "rd <rd>" path (iosxrParseL3VPN) - the ASN capture itself is unused,
// same as the old iosxrReVRFName-based code never read it either, it just
// needed "router bgp " to identify the block via FindChild's prefix match.
type iosxrRouterBGPCfg struct {
	VRFs map[string]iosxrBGPVRFCfg `cfg:"vrf {}"`
}

// iosxrRunningConfig runs "show running-config" over classic SSH CLI (same
// transport as Exec/RunningConfigGet) and parses it into a ConfigNode tree.
func iosxrRunningConfig(username, password, host string) ([]*ConfigNode, error) {
	output, err := sshRunCLI(username, password, host, "", []sshCmd{{Cmd: "terminal length 0"}, {Cmd: "show running-config", EndMarker: iosxrConfigEndMarker}})
	if err != nil {
		return nil, err
	}
	return ParseConfigContext(strings.Split(output, "\n"), "!"), nil
}

// iosxrInterfaceType classifies an IOS-XR interface name for Netbox's
// "type" field. Real IOS-XR bundle interfaces are named "Bundle-Ether<N>"
// (matching the LagParent string built two lines below, "interface
// Bundle-Ether "+lag_id). Physical interfaces (GigabitEthernet/TenGigE/
// HundredGigE/...) get "other" (Netbox's generic physical type; the exact
// PHY/media isn't derivable from config alone), not "virtual" - Netbox
// rejects terminating a cable on a "virtual" interface, so misclassifying a
// real port here would silently break syncConnections for any interface
// device-sync itself creates.
func iosxrInterfaceType(ifname string) string {
	switch {
	case strings.Contains(ifname, "."):
		return "virtual"
	case strings.HasPrefix(ifname, "BVI"):
		return "virtual"
	case strings.HasPrefix(ifname, "Bundle-Ether"):
		return "lag"
	case strings.HasPrefix(ifname, "Loopback"):
		return "virtual"
	default:
		return "other"
	}
}

// iosxrParseIPv4WithMask converts "172.27.247.23 255.255.255.254" into a
// netip.Prefix - IOS-XR's ipv4 address line carries a dotted-decimal mask
// rather than a CIDR literal.
func iosxrParseIPv4WithMask(addrStr, maskStr string) (netip.Prefix, bool) {
	addr, err := netip.ParseAddr(addrStr)
	if err != nil {
		return netip.Prefix{}, false
	}
	maskIP := net.ParseIP(maskStr).To4()
	if maskIP == nil {
		return netip.Prefix{}, false
	}
	ones, bits := net.IPMask(maskIP).Size()
	if bits == 0 {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, ones), true
}

// iosxrParseInterfaces fills interfaces via tagparse.go's engine instead of
// a hand-written regexp+switch walk. The "[l2transport]" optional token on
// the interface line itself is injected
// as a synthetic "l2transport" child line, which iosxrInterfaceCfg.
// L2Transport picks up as an ordinary presence flag - see tagparse.go's
// injectOptionals.
func iosxrParseInterfaces(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		Interfaces map[string]iosxrInterfaceCfg `cfg:"interface {} [l2transport]"`
	}
	UnmarshalNodes(nodes, &top)

	for _, ifname := range sortedKeys(top.Interfaces) {
		ic := top.Interfaces[ifname]
		iface := &Interface{
			Name:        ifname,
			Type:        iosxrInterfaceType(ifname),
			Description: ic.Description,
			LagID:       ic.BundleID,
			VRF:         ic.VRF,
			VlanID:      ic.VlanID,
			L2Transport: ic.L2Transport,
		}
		if ic.BundleID != "" {
			iface.LagParent = "interface Bundle-Ether " + ic.BundleID
		}
		if ic.IPv4 != nil {
			iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{Address: netip.Prefix(*ic.IPv4), VRF: iface.VRF})
		}
		if ic.IPv6 != nil {
			iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{Address: *ic.IPv6, VRF: iface.VRF})
		}
		dc.AddInterface(iface)
	}
}

// iosxrParseEline parses l2vpn -> xconnect group -> p2p, each with an
// interface conn and a neighbor/pw-id pw conn.
// The interface leg always wins Conn1 (even if a pseudowire leg was parsed
// first) and the pseudowire leg only takes Conn1 if nothing already has it
// - matching the original hand-written parser's behavior exactly, not just
// its outcome on this file's test fixture.
func iosxrParseEline(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		L2VPN *iosxrL2VPNCfg `cfg:"l2vpn"`
	}
	UnmarshalNodes(nodes, &top)
	if top.L2VPN == nil {
		return
	}

	for _, groupName := range sortedKeys(top.L2VPN.XconnectGroups) {
		group := top.L2VPN.XconnectGroups[groupName]
		for _, name := range sortedKeys(group.P2P) {
			p2p := group.P2P[name]
			eline := &ELINE{Name: name}
			if iface, ok := dc.InterfacesByName[p2p.Interface]; ok {
				eline.Conn1 = iface
			}
			if p2p.Neighbor != nil {
				pw := &Pseudowire{Name: name, Neighbor: p2p.Neighbor.Neighbor, PWID: p2p.Neighbor.PWID}
				if eline.Conn1 == nil {
					eline.Conn1 = pw
				} else {
					eline.Conn2 = pw
				}
			}
			dc.ELINEs[eline.Name] = eline
		}
	}
}

// iosxrParseElan parses evpn evi blocks (looked up by ID) attached to
// l2vpn bridge group / bridge-domain ELANs.
func iosxrParseElan(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		L2VPN *iosxrL2VPNCfg `cfg:"l2vpn"`
		EVPN  *iosxrEVPNCfg  `cfg:"evpn"`
	}
	UnmarshalNodes(nodes, &top)
	if top.L2VPN == nil {
		return
	}
	var evis map[string]iosxrEVICfg
	if top.EVPN != nil {
		evis = top.EVPN.EVIs
	}

	for _, groupName := range sortedKeys(top.L2VPN.BridgeGroups) {
		group := top.L2VPN.BridgeGroups[groupName]
		for _, name := range sortedKeys(group.BridgeDomains) {
			bd := group.BridgeDomains[name]
			elan := &ELAN{Name: name}
			for _, ifname := range bd.Interfaces {
				if iface, ok := dc.InterfacesByName[ifname]; ok {
					elan.Interfaces = append(elan.Interfaces, iface.Name)
				}
			}
			if evi, ok := evis[bd.EVI]; bd.EVI != "" && ok && evi.BGP != nil {
				elan.RD, elan.RTImport, elan.RTExport = evi.BGP.RD, evi.BGP.RTImport, evi.BGP.RTExport
			}
			dc.ELANs[elan.Name] = elan
		}
	}
}

// iosxrReVRFName matches a top-level "vrf <name>" context line.
var iosxrReVRFName = regexp.MustCompile(`^vrf (\S+)$`)

// iosxrParseVRF parses top-level "vrf <name>" context lines.
func iosxrParseVRF(dc *DeviceConfig, nodes []*ConfigNode) {
	for _, node := range nodes {
		m := iosxrReVRFName.FindStringSubmatch(node.Line)
		if m == nil {
			continue
		}
		vrf := &VRF{Name: m[1]}
		af := FindChild(node.Children, "address-family ipv4 unicast")
		if af != nil {
			if imp := FindChild(af.Children, "import route-target"); imp != nil && len(imp.Children) > 0 {
				vrf.RTImport = imp.Children[0].Line
			}
			if exp := FindChild(af.Children, "export route-target"); exp != nil && len(exp.Children) > 0 {
				vrf.RTExport = exp.Children[0].Line
			}
		}
		dc.VRFs[vrf.Name] = vrf
	}
}

// iosxrParseL3VPN parses "router bgp <asn>" -> "vrf <name>" blocks, each
// attached to (and mutating the rd of) the VRF of the same name.
func iosxrParseL3VPN(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		RouterBGP *iosxrRouterBGPCfg `cfg:"router bgp {}"`
	}
	UnmarshalNodes(nodes, &top)
	if top.RouterBGP == nil {
		return
	}

	for _, name := range sortedKeys(top.RouterBGP.VRFs) {
		vrf, ok := dc.VRFs[name]
		if !ok {
			continue
		}
		if rd := top.RouterBGP.VRFs[name].RD; rd != "" {
			vrf.RD = rd
		}
		dc.L3VPNs[name] = &L3VPN{Name: name, VRF: vrf}
	}
}

// GetDeviceConfig fetches and parses the device's running config.
func (driver *IOSXRDriver) GetDeviceConfig() (*DeviceConfig, error) {
	nodes, err := iosxrRunningConfig(driver.p.Username, driver.p.Password, driver.p.Name)
	if err != nil {
		return nil, err
	}
	dc := NewDeviceConfig()
	iosxrParseInterfaces(dc, nodes)
	iosxrParseEline(dc, nodes)
	iosxrParseElan(dc, nodes)
	iosxrParseVRF(dc, nodes)
	iosxrParseL3VPN(dc, nodes)
	return dc, nil
}

var (
	iosxrReNeighborSep    = "---"
	iosxrReLocalInterface = regexp.MustCompile(`^Local Interface\s*:(.*)$`)
	iosxrRePortID         = regexp.MustCompile(`^Port id\s*:(.*)$`)
	iosxrReSystemName     = regexp.MustCompile(`^System Name\s*:(.*)$`)
)

// GetNeighbors runs "show lldp neighbors detail" and parses the "---"-
// separated blocks. Local interfaces that are subinterfaces (name contains
// ".") are skipped.
func (driver *IOSXRDriver) GetNeighbors() ([]*Neighbor, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "terminal length 0"}, {Cmd: "show lldp neighbors detail"}})
	if err != nil {
		return nil, err
	}
	return iosxrParseNeighbors(output), nil
}

// iosxrParseNeighbors parses "show lldp neighbors detail"'s "---"-separated
// blocks. Local interfaces that are subinterfaces (name contains ".") are
// skipped.
func iosxrParseNeighbors(output string) []*Neighbor {
	var neighbors []*Neighbor
	n := &Neighbor{}
	flush := func() {
		if n.LocalInterface != "" && !strings.Contains(n.LocalInterface, ".") {
			neighbors = append(neighbors, n)
		}
		n = &Neighbor{}
	}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, iosxrReNeighborSep) {
			flush()
			continue
		}
		switch {
		case iosxrReLocalInterface.MatchString(line):
			n.LocalInterface = strings.TrimSpace(iosxrReLocalInterface.FindStringSubmatch(line)[1])
		case iosxrRePortID.MatchString(line):
			n.RemoteInterface = strings.TrimSpace(iosxrRePortID.FindStringSubmatch(line)[1])
		case iosxrReSystemName.MatchString(line):
			n.RemoteName = strings.TrimSpace(iosxrReSystemName.FindStringSubmatch(line)[1])
		}
	}
	flush()
	return neighbors
}
