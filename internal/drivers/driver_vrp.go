package drivers

// Huawei VRP driver - see README-DRIVERS.md for how this driver works
// (SSH CLI only, no NETCONF/eAPI) and how it compares to the others.

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/netboxtool"
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
// prompt reappears - see sshCmd.EndMarker. A capture without this line is
// incomplete and must not be parsed.
var vrpConfigEndMarker = regexp.MustCompile(`^return$`)

// vrpRetrievedConfigDir is where each display current-config capture is
// written, one file per device, including dumps that fail the return check.
// Tests point this at a temp directory.
var vrpRetrievedConfigDir = "/var/lib/factum2/retrieved-config"

// ----------------------------------------------------------------------
// External methods
// ----------------------------------------------------------------------

func (driver *VrpDriver) Exec(cmd string) (*ExecModel, error) {
	output, err := sshRunCLI(context.Background(), driver.p, []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: cmd}})
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
	output, err := sshRunCLI(context.Background(), driver.p, []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display version"}})
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
	output, err := driver.fetchCurrentConfig()
	if err != nil {
		return nil, err
	}
	return &RunningConfigModel{ConfigStr: output}, nil
}

// RunningConfigSave answers VRP's "save" confirmation prompt with "y".
func (driver *VrpDriver) RunningConfigSave() error {
	_, err := sshRunCLI(context.Background(), driver.p, []sshCmd{{Cmd: "save"}, {Cmd: "y"}})
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
	output, err := sshRunCLI(context.Background(), driver.p, []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display interface description"}})
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
	_, err := sshRunCLI(context.Background(), driver.p, cmds)
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
	_, err = sshRunCLI(context.Background(), driver.p, cmds)
	return err
}

// ----------------------------------------------------------------------
// ApplyCLISession - already-rendered CLI lines inside a system-view
// session. sessionName is ignored (VRP has no named configure sessions).
// ----------------------------------------------------------------------

// vrpCLIErrorMarkers matches VRP's "Error: ..." line prefix (e.g.
// "Error: Unrecognized command found at '^' position.", "Error: The VLAN
// does not exist."). sshRunCLIPipeline has no per-command status code -
// the session keeps accepting lines after a rejected one - so an error
// has to be found by scanning the captured output text.
var vrpCLIErrorMarkers = regexp.MustCompile(`(?m)^\s*Error:`)

// vrpFindCLIError returns the first error line found in output (the
// combined capture for a whole sshRunCLIPipeline batch), or "" if none of
// it looks like a VRP CLI error.
func vrpFindCLIError(output string) string {
	loc := vrpCLIErrorMarkers.FindStringIndex(output)
	if loc == nil {
		return ""
	}
	line := output[loc[0]:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// vrpCLISessionCommands wraps cmds in system-view ... return without
// touching the network, so the session shape is unit-testable the way
// vrpInterfaceVLANsCommands is. "return" leaves user view from any nested
// config depth (interface, vlan, ...) rather than requiring a matching
// number of "quit"s.
func vrpCLISessionCommands(cmds []string) []string {
	full := append([]string{"system-view"}, cmds...)
	return append(full, "return")
}

// vrpCLISession pastes cmds inside a system-view session. Each line is
// applied to running immediately - VRP has no candidate/commit step here,
// matching SetInterfaceDescriptions. Does not "save"; persistence is a
// separate RunningConfigSave.
func (driver *VrpDriver) vrpCLISession(cmds []string) error {
	output, err := sshRunCLIPipeline(context.Background(), driver.p, vrpCLISessionCommands(cmds), nil)
	if err != nil {
		return err
	}
	if msg := vrpFindCLIError(output); msg != "" {
		return fmt.Errorf("vrp cli apply failed: %s", msg)
	}
	return nil
}

// ApplyCLISession implements CLISessionApplier for Huawei VRP.
func (driver *VrpDriver) ApplyCLISession(_ string, cmds []string, _ string) error {
	return driver.vrpCLISession(cmds)
}

// ----------------------------------------------------------------------
// GetDeviceConfig parses running config (via config_context.go's
// hierarchical parser, comment char "#") into the shared DeviceConfig
// shape, for internal/device-sync. VRP has no ELINE/ELAN/VRF/L3VPN parsing,
// so only interfaces are parsed here; those maps stay empty. GetNeighbors
// is SNMP, not this CLI parse.
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
	Description string           `cfg:"description {:toend}"`
	VRF         string           `cfg:"vrf {:toend}"`
	IPv4        *iosxrIPv4Prefix `cfg:"ip address {} {}"`
	// A slice: VRP allows several "ipv6 address" lines on one interface.
	// A single value kept only the last line, and device-sync then deleted
	// the others from NetBox.
	IPv6             []netip.Prefix `cfg:"ipv6 address {}"`
	SwitchportMode   string         `cfg:"port link-type {}"`
	AccessVlan       int            `cfg:"port default vlan {}"`
	TrunkPVID        int            `cfg:"port trunk pvid vlan {}"`
	TrunkAllowedVlan string         `cfg:"port trunk allow-pass vlan {:toend}"`
	L2Transport      bool           `cfg:"mode l2"`
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
		for _, prefix := range ic.IPv6 {
			iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{Address: prefix, VRF: iface.VRF})
		}
		dc.AddInterface(iface)
	}
}

func (driver *VrpDriver) GetDeviceConfig() (*DeviceConfig, error) {
	output, err := driver.fetchCurrentConfig()
	if err != nil {
		return nil, err
	}
	nodes := ParseConfigContext(strings.Split(output, "\n"), "#")
	dc := NewDeviceConfig()
	vrpParseGlobalVlans(dc, nodes)
	vrpParseInterfaces(dc, nodes)
	return dc, nil
}

// fetchCurrentConfig runs "display current-config". The capture is written
// under vrpRetrievedConfigDir by the SSH session before this returns. A
// dump that never printed the trailing "return" is an error, so device-sync
// skips the device instead of deleting interfaces that were not in the capture.
func (driver *VrpDriver) fetchCurrentConfig() (string, error) {
	output, err := sshRunCLI(context.Background(), driver.p, []sshCmd{{Cmd: "screen-length 0 temporary"}, {Cmd: "display current-config", EndMarker: vrpConfigEndMarker}})
	if err != nil {
		return "", err
	}
	if err := vrpRequireConfigEnd(output); err != nil {
		return "", fmt.Errorf("display current-config on %s: %w (saved %s)", driver.p.Name, err, vrpRetrievedConfigPath(driver.p.Name))
	}
	return output, nil
}

func vrpRequireConfigEnd(output string) error {
	if hasConfigEndMarker(output, vrpConfigEndMarker) {
		return nil
	}
	return fmt.Errorf("did not end with return")
}

func vrpConfigRunTimeout(cmds []sshCmd) time.Duration {
	for _, c := range cmds {
		if c.EndMarker == vrpConfigEndMarker {
			return vrpConfigTimeout
		}
	}
	return 0
}

func vrpRetrievedConfigPath(host string) string {
	return filepath.Join(vrpRetrievedConfigDir, retrievedConfigName(host))
}

func retrievedConfigName(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "unknown"
	}
	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unknown.cfg"
	}
	return b.String() + ".cfg"
}

// noteRetrievedVRPConfig writes output for host and logs the path. A write
// failure does not fail the fetch; the return check still does.
func noteRetrievedVRPConfig(host, output string) {
	if vrpRetrievedConfigDir == "" {
		return
	}
	path, err := saveRetrievedVRPConfig(host, output)
	complete := vrpRequireConfigEnd(output) == nil
	if err != nil {
		slog.Warn("ssh.config", "host", host, "path", path, "bytes", len(output), "complete", complete, "err", err.Error())
		return
	}
	slog.Info("ssh.config", "host", host, "path", path, "bytes", len(output), "complete", complete)
}

func saveRetrievedVRPConfig(host, output string) (string, error) {
	dir := vrpRetrievedConfigDir
	final := vrpRetrievedConfigPath(host)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return final, err
	}
	f, err := os.CreateTemp(dir, retrievedConfigName(host)+".*.tmp")
	if err != nil {
		return final, err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.WriteString(output); err != nil {
		_ = f.Close()
		return final, err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return final, err
	}
	if err := f.Close(); err != nil {
		return final, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return final, err
	}
	ok = true
	return final, nil
}

// GetNeighbors reads IEEE LLDP-MIB (LLDP-V2-MIB if the original is absent)
// over SNMPv2c. The community is DriverParam.SNMPCommunities.
func (driver *VrpDriver) GetNeighbors() ([]*Neighbor, error) {
	return lldpNeighbors(driver.p.Name, driver.p.SNMPCommunities)
}
