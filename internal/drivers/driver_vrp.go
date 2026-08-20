package drivers

// Huawei VRP driver - see README-DRIVERS.md for how this driver works
// (SSH CLI only, no NETCONF/eAPI) and how it compares to the others.

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/netboxtool"
)

// ----------------------------------------------------------------------
// Device
// ----------------------------------------------------------------------

type VrpDriver struct {
	DriverClient
	p DriverParam
}

func init() {
	registerDriver("vrp", func(p DriverParam) (DriverClient, error) { return NewVrpDriver(p) })
}

func NewVrpDriver(p DriverParam) (*VrpDriver, error) {
	if err := validateDriverParam(p); err != nil {
		return nil, err
	}
	return &VrpDriver{p: p}, nil
}

// vrpIgnoreInterfaces lists interfaces to skip when parsing - NULL0 is a
// synthetic discard interface with nothing meaningful to sync.
var vrpIgnoreInterfaces = map[string]bool{"NULL0": true}

// vrpConfigEndMarker matches the lone "return" line VRP prints at the end
// of a "display current-config" (or scoped variant) dump, right before the
// prompt reappears - see sshCmd.EndMarker.
var vrpConfigEndMarker = regexp.MustCompile(`^return$`)

// ----------------------------------------------------------------------
// External methods
// ----------------------------------------------------------------------

func (driver *VrpDriver) Exec(cmd string) (*ExecModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: cmd}})
	if err != nil {
		return nil, err
	}
	return &ExecModel{Result: output}, nil
}

var (
	vrpReVersion = regexp.MustCompile(`VRP \(R\) software, Version (\S+)`)
	vrpReUptime  = regexp.MustCompile(`^\S+ (\S+) uptime is`)
)

// Version scrapes a handful of fields out of "display version" text - VRP
// has no structured (JSON/NETCONF) equivalent to ask instead. Fields VRP's
// "display version" doesn't clearly expose (MemTotal/MemFree/
// BootupTimestap/Architecture/InternalBuildId/SystemMacAddress/
// SerialNumber/HardwareRevision) are left zero-valued rather than guessed.
func (driver *VrpDriver) Version() (*VersionModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display version"}})
	if err != nil {
		return nil, err
	}
	var version VersionModel
	for _, line := range strings.Split(output, "\n") {
		if m := vrpReVersion.FindStringSubmatch(line); m != nil {
			version.Version = m[1]
		}
		if m := vrpReUptime.FindStringSubmatch(line); m != nil {
			version.ModelName = m[1]
		}
	}
	return &version, nil
}

func (driver *VrpDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display current-config", EndMarker: vrpConfigEndMarker}})
	if err != nil {
		return nil, err
	}
	return &RunningConfigModel{ConfigStr: output}, nil
}

// RunningConfigSave answers VRP's "save" confirmation prompt with "y".
func (driver *VrpDriver) RunningConfigSave() error {
	_, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "save"}, {Cmd: "y"}})
	return err
}

// vrpAdminStatus maps "display interface description"'s PHY column onto
// openconfig-interfaces' admin-status vocabulary (UP/DOWN) - only an
// explicit "*down" (administratively down, per the command's own legend)
// means shut down; every other PHY value is an admin-enabled interface.
func vrpAdminStatus(phy string) string {
	if strings.HasPrefix(phy, "*") {
		return "DOWN"
	}
	return "UP"
}

// vrpParseInterfaceDescriptions parses "display interface description"'s
// fixed-column table (Interface/PHY/Protocol/Description), skipping the
// command's legend lines above the header.
func vrpParseInterfaceDescriptions(output string) []*netboxtool.NBInterface {
	var interfaces []*netboxtool.NBInterface
	headerSeen := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !headerSeen {
			if strings.HasPrefix(trimmed, "Interface") && strings.Contains(trimmed, "PHY") {
				headerSeen = true
			}
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			continue
		}
		description := ""
		if len(fields) > 3 {
			description = strings.Join(fields[3:], " ")
		}
		interfaces = append(interfaces, &netboxtool.NBInterface{
			Name:               fields[0],
			Description:        description,
			InterfaceStatus:    vrpAdminStatus(fields[1]),
			LineProtocolStatus: strings.ToUpper(fields[2]),
		})
	}
	return interfaces
}

func (driver *VrpDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display interface description"}})
	if err != nil {
		return nil, err
	}
	return vrpParseInterfaceDescriptions(output), nil
}

// ----------------------------------------------------------------------
// Update interface description(s) - config-mode CLI command list
// ----------------------------------------------------------------------

func (driver *VrpDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func (driver *VrpDriver) SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error {
	if err := checkNamesMatchInterfaces(name, intf); err != nil {
		return err
	}
	cmds := []sshCmd{{Cmd: "system-view"}}
	for ix, ifname := range name {
		cmds = append(cmds, sshCmd{Cmd: "interface " + ifname})
		if intf[ix].Description != "" {
			cmds = append(cmds, sshCmd{Cmd: "description " + intf[ix].Description})
		} else {
			cmds = append(cmds, sshCmd{Cmd: "undo description"})
		}
		cmds = append(cmds, sshCmd{Cmd: "quit"})
	}
	cmds = append(cmds, sshCmd{Cmd: "quit"})
	_, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", cmds)
	return err
}

// ----------------------------------------------------------------------
// Update interface VLANs / switchport config - config-mode CLI command
// list, same transport and session shape as SetInterfaceDescriptions.
// VRP's read side (vrpParseInterfaces/vrpParseGlobalVlans) has no
// dot1q-tunnel/Q-in-Q support, so neither does this - callers must not
// pass VLANConfig.SwitchportMode == "dot1q-tunnel" for this driver.
// ----------------------------------------------------------------------

// vrpFormatVlanIDList renders a VLAN ID list as VRP's space-separated
// syntax - the write-side counterpart of vrpParseVlanIDList's read.
func vrpFormatVlanIDList(ids []int) string {
	parts := make([]string, len(ids))
	for ix, id := range ids {
		parts[ix] = strconv.Itoa(id)
	}
	return strings.Join(parts, " ")
}

// vrpInterfaceVLANsCommands builds SetInterfaceVLANs' config-mode CLI
// command list - split out as a pure function (no SSH dial) so it's
// testable without a fake SSH server, same shape as srosELINECommands in
// driver_nokia_sros_eline.go (there's no fake-SSH-server tier for VRP any
// more than there is for SR OS - see that function's doc comment).
func vrpInterfaceVLANsCommands(name []string, params []*VLANConfig) ([]sshCmd, error) {
	if err := checkNamesMatchVLANConfigs(name, params); err != nil {
		return nil, err
	}

	seenVlans := map[int]bool{}
	var newVlans []int
	addVlan := func(vid int) {
		if vid == 0 || seenVlans[vid] {
			return
		}
		seenVlans[vid] = true
		newVlans = append(newVlans, vid)
	}
	for _, p := range params {
		addVlan(p.UntaggedVLAN)
		for _, vid := range p.TaggedVLANs {
			addVlan(vid)
		}
	}

	cmds := []sshCmd{{Cmd: "system-view"}}
	if len(newVlans) > 0 {
		cmds = append(cmds, sshCmd{Cmd: "vlan batch " + vrpFormatVlanIDList(newVlans)})
	}
	for ix, ifname := range name {
		p := params[ix]
		cmds = append(cmds, sshCmd{Cmd: "interface " + ifname})
		switch p.SwitchportMode {
		case "access":
			cmds = append(cmds, sshCmd{Cmd: "port link-type access"})
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, sshCmd{Cmd: "port default vlan " + strconv.Itoa(p.UntaggedVLAN)})
			} else {
				cmds = append(cmds, sshCmd{Cmd: "undo port default vlan"})
			}
		case "trunk":
			cmds = append(cmds, sshCmd{Cmd: "port link-type trunk"})
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, sshCmd{Cmd: "port trunk pvid vlan " + strconv.Itoa(p.UntaggedVLAN)})
			}
			if len(p.TaggedVLANs) > 0 {
				cmds = append(cmds, sshCmd{Cmd: "port trunk allow-pass vlan " + vrpFormatVlanIDList(p.TaggedVLANs)})
			}
		default:
			cmds = append(cmds, sshCmd{Cmd: "undo port default vlan"}, sshCmd{Cmd: "undo port trunk allow-pass vlan all"})
		}
		cmds = append(cmds, sshCmd{Cmd: "quit"})
	}
	cmds = append(cmds, sshCmd{Cmd: "quit"})
	return cmds, nil
}

// SetInterfaceVLANs pushes switchport mode and VLAN membership to a set of
// interfaces. Every VID referenced by any target interface is first
// declared via "vlan batch <ids>" (matching vrpReVlanBatch's read shape) -
// VRP rejects "port default vlan"/"port trunk allow-pass vlan" for a VID
// that hasn't been declared.
func (driver *VrpDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	cmds, err := vrpInterfaceVLANsCommands(name, params)
	if err != nil {
		return err
	}
	_, err = sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", cmds)
	return err
}

// ----------------------------------------------------------------------
// GetDeviceConfig / GetNeighbors - parse running config (via
// config_context.go's hierarchical parser, comment char "#") and LLDP
// neighbors into the shared drivers.DeviceConfig/Neighbor shapes, for
// internal/device-sync. VRP has no ELINE/ELAN/VRF/L3VPN parsing, so only
// interfaces are parsed here; DeviceConfig's VRF/ELINE/ELAN/L3VPN maps stay
// empty.
// ----------------------------------------------------------------------

// vrpInterfaceCfg is one "interface X [mode l2]" block, tag-bound by
// tagparse.go's engine - see the type-level comment there for the DSL.
// Replaces what used to be nine package-level regexps
// (vrpReVRF/vrpReLinkType/vrpReDefaultVlan/...) plus a hand-written
// switch. VRP's L2 sub-interface syntax (NE-series routers, L2VPN AC
// binding) is "interface GigabitEthernet0/0/1.100 mode l2" - the
// "[mode l2]" optional pair is injected as a synthetic "mode l2" child
// line, which L2Transport picks up as an ordinary presence flag, same
// mechanism as IOS-XR's "[l2transport]" - see tagparse.go's
// injectOptionals.
//
// IPv4 reuses iosxrIPv4Prefix (driver_iosxr.go) rather than a VRP-specific
// type - "ip address A.B.C.D W.X.Y.Z" is the same dotted-decimal-mask
// shape as IOS-XR's "ipv4 address", which is why the original hand-written
// code already called iosxrParseIPv4WithMask here too.
type vrpInterfaceCfg struct {
	Description      string           `cfg:"description {:toend}"`
	VRF              string           `cfg:"vrf {:toend}"`
	IPv4             *iosxrIPv4Prefix `cfg:"ip address {} {}"`
	IPv6             *netip.Prefix    `cfg:"ipv6 address {}"`
	SwitchportMode   string           `cfg:"port link-type {}"`
	AccessVlan       int              `cfg:"port default vlan {}"`
	TrunkPVID        int              `cfg:"port trunk pvid vlan {}"`
	TrunkAllowedVlan string           `cfg:"port trunk allow-pass vlan {:toend}"`
	L2Transport      bool             `cfg:"mode l2"`
}

// vrpVlanBlockCfg is a top-level "vlan <id>" block - only its optional
// "name" child is modeled, matching vrpParseGlobalVlans' original scope.
type vrpVlanBlockCfg struct {
	Name string `cfg:"name {:toend}"`
}

// vrpConfigTree is the whole "display current-config" tree, tag-bound in
// one pass. "vlan batch {:toend}" (2 literal tokens) beats "vlan {}" (1
// literal token) on specificity, so a "vlan batch ..." line is never
// mistakenly tried against the block-form tag first - though it wouldn't
// match anyway, since matchLine requires the whole line to be consumed and
// "vlan {}" only accounts for two of a batch line's words.
type vrpConfigTree struct {
	Interfaces map[string]vrpInterfaceCfg `cfg:"interface {} [mode l2]"`
	VlanBlocks map[string]vrpVlanBlockCfg `cfg:"vlan {}"`
	VlanBatch  []string                   `cfg:"vlan batch {:toend}"`
}

// vrpParseVlanIDList expands a VRP VLAN ID list - space-separated IDs mixed
// with "A to B" ranges, e.g. {"318", "to", "319", "347", "994", "to",
// "3740"} - into individual IDs. Used for both "vlan batch ..." and "port
// trunk allow-pass vlan ...". Non-numeric tokens are skipped rather than
// aborting the whole line, so one malformed token doesn't lose the rest.
func vrpParseVlanIDList(fields []string) []int {
	var ids []int
	for ix := 0; ix < len(fields); ix++ {
		if ix+2 < len(fields) && fields[ix+1] == "to" {
			start, errStart := strconv.Atoi(fields[ix])
			end, errEnd := strconv.Atoi(fields[ix+2])
			if errStart == nil && errEnd == nil {
				for v := start; v <= end; v++ {
					ids = append(ids, v)
				}
			}
			ix += 2
			continue
		}
		if v, err := strconv.Atoi(fields[ix]); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}

// vrpInterfaceType classifies a VRP interface name for Netbox's "type"
// field - a real physical port gets "other" (Netbox's generic physical
// type; the exact PHY/media isn't derivable from config alone) rather than
// "virtual". Netbox rejects terminating a cable on a "virtual" interface,
// so misclassifying a physical port here would silently break
// syncConnections for any port device-sync itself creates. LAG
// (Eth-Trunk) isn't detected - bundle/trunk membership isn't parsed.
func vrpInterfaceType(ifname string) string {
	if strings.Contains(ifname, ".") {
		return "virtual"
	}
	return "other"
}

// vrpParseGlobalVlans parses global VLANs: "vlan batch 994 2736" and
// "vlan batch 318 to 319 347 994 3740" register
// bare VLANs, while a "vlan <id>" block with a "name" child (only emitted
// when the VLAN was individually configured, e.g. for a description) fills
// in the name of a VLAN either newly seen here or already registered by a
// batch line.
func vrpParseGlobalVlans(dc *DeviceConfig, nodes []*ConfigNode) {
	var top vrpConfigTree
	UnmarshalNodes(nodes, &top)

	for _, batch := range top.VlanBatch {
		for _, id := range vrpParseVlanIDList(strings.Fields(batch)) {
			if _, exists := dc.GlobalVLANs[id]; !exists {
				dc.GlobalVLANs[id] = &VLAN{ID: id}
			}
		}
	}
	for _, idStr := range sortedKeys(top.VlanBlocks) {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		vlan, exists := dc.GlobalVLANs[id]
		if !exists {
			vlan = &VLAN{ID: id}
			dc.GlobalVLANs[id] = vlan
		}
		vlan.Name = top.VlanBlocks[idStr].Name
	}
}

// vrpParseInterfaces fills interfaces via tagparse.go's engine.
// AccessVlan/TrunkPVID are mutually exclusive on a real device (access vs.
// trunk mode), so preferring whichever is set reproduces a single-field
// switch-case assignment.
func vrpParseInterfaces(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		Interfaces map[string]vrpInterfaceCfg `cfg:"interface {} [mode l2]"`
	}
	UnmarshalNodes(nodes, &top)

	for _, ifname := range sortedKeys(top.Interfaces) {
		if vrpIgnoreInterfaces[ifname] {
			continue
		}
		ic := top.Interfaces[ifname]
		iface := &Interface{
			Name:           ifname,
			Type:           vrpInterfaceType(ifname),
			Description:    ic.Description,
			VRF:            ic.VRF,
			SwitchportMode: ic.SwitchportMode,
			L2Transport:    ic.L2Transport,
		}
		switch {
		case ic.AccessVlan != 0:
			iface.UntaggedVLAN = ic.AccessVlan
		case ic.TrunkPVID != 0:
			iface.UntaggedVLAN = ic.TrunkPVID
		}
		if ic.TrunkAllowedVlan != "" {
			iface.TaggedVLANs = append(iface.TaggedVLANs, vrpParseVlanIDList(strings.Fields(ic.TrunkAllowedVlan))...)
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

func (driver *VrpDriver) GetDeviceConfig() (*DeviceConfig, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display current-config", EndMarker: vrpConfigEndMarker}})
	if err != nil {
		return nil, err
	}
	nodes := ParseConfigContext(strings.Split(output, "\n"), "#")
	dc := NewDeviceConfig()
	vrpParseGlobalVlans(dc, nodes)
	vrpParseInterfaces(dc, nodes)
	return dc, nil
}

var (
	vrpReNeighborHeader = regexp.MustCompile(`^(\S+) has \d+ neighbor`)
	vrpReSystemName     = regexp.MustCompile(`^System name\s*:(.*)$`)
	vrpReChassisID      = regexp.MustCompile(`^Chassis ID\s*:(.*)$`)
	vrpRePortID         = regexp.MustCompile(`^Port ID\s*:(.*)$`)
)

// GetNeighbors runs "display lldp neighbor" and parses it: each "<ifname>
// has N neighbor(s):" line starts a new neighbor block.
func (driver *VrpDriver) GetNeighbors() ([]*Neighbor, error) {
	output, err := sshRunCLI(driver.p.Username, driver.p.Password, driver.p.Name, "", []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display lldp neighbor"}})
	if err != nil {
		return nil, err
	}
	return vrpParseNeighbors(output), nil
}

// vrpParseNeighbors parses "display lldp neighbor": each "<ifname> has N
// neighbor(s):" line starts a new neighbor block.
func vrpParseNeighbors(output string) []*Neighbor {
	var neighbors []*Neighbor
	var n *Neighbor
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		switch {
		case vrpReNeighborHeader.MatchString(line):
			if n != nil && n.RemoteInterface != "" {
				neighbors = append(neighbors, n)
			}
			n = &Neighbor{LocalInterface: vrpReNeighborHeader.FindStringSubmatch(line)[1]}
		case n == nil:
			continue
		case vrpReSystemName.MatchString(line):
			n.RemoteName = strings.TrimSpace(vrpReSystemName.FindStringSubmatch(line)[1])
		case vrpReChassisID.MatchString(line):
			n.SystemID = strings.TrimSpace(vrpReChassisID.FindStringSubmatch(line)[1])
		case vrpRePortID.MatchString(line):
			n.RemoteInterface = strings.TrimSpace(vrpRePortID.FindStringSubmatch(line)[1])
		}
	}
	if n != nil && n.RemoteInterface != "" {
		neighbors = append(neighbors, n)
	}
	return neighbors
}
