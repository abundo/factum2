package drivers

// Cisco SMB driver - see README-DRIVERS.md for how this driver works
// (SSH CLI only, no NETCONF/eAPI). Supports Cisco SG300, SG350, C1200, C1300.

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/netboxtool"
)

// ----------------------------------------------------------------------
// Device
// ----------------------------------------------------------------------

type CiscoSMBDriver struct {
	DriverClient
	p DriverParam
}

func init() {
	registerDriver("ciscosmb", func(p DriverParam) (DriverClient, error) { return NewCiscoSMBDriver(p) })
}

func NewCiscoSMBDriver(p DriverParam) (*CiscoSMBDriver, error) {
	if err := validateDriverParam(p); err != nil {
		return nil, err
	}
	return &CiscoSMBDriver{p: p}, nil
}

var (
	_ DriverClient      = (*CiscoSMBDriver)(nil)
	_ CLISessionApplier = (*CiscoSMBDriver)(nil)
)

var (
	smbCLIErrorMarkers  = regexp.MustCompile(`(?m)^%\s`)
	smbReSWVersion      = regexp.MustCompile(`(?m)^SW version\s+(\S+)`)
	smbReActiveVersion  = regexp.MustCompile(`(?m)^\s+Version:\s+(\S+)`)
	smbReInventory      = regexp.MustCompile(`(?i)PID:\s*(\S+)\s+VID:\s*(\S+)\s+SN:\s*(\S+)`)
	smbReSystemMAC      = regexp.MustCompile(`(?m)^System MAC Address:\s*(\S+)`)
	smbReTableUnderline = regexp.MustCompile(`^-+(\s+-+)+\s*$`)
	smbRePortTypeHeader = regexp.MustCompile(`(?i)^Port\s+Type\s+`)
	smbReDescHeader     = regexp.MustCompile(`(?i)^(Port|Ch)\s+Description\s*$`)
)

// smbIfPrefixes maps abbreviated show-command names onto the long form
// running-config uses. Longer prefixes are listed first so "gigabitethernet"
// wins over "gi".
var smbIfPrefixes = []struct{ short, long string }{
	{"gigabitethernet", "GigabitEthernet"},
	{"tengigabitethernet", "TenGigabitEthernet"},
	{"fastethernet", "FastEthernet"},
	{"port-channel", "Port-channel"},
	{"vlan", "Vlan"},
	{"gi", "GigabitEthernet"},
	{"te", "TenGigabitEthernet"},
	{"fa", "FastEthernet"},
	{"po", "Port-channel"},
	{"vl", "Vlan"},
}

// smbCanonicalIfName expands abbreviated interface names (gi1, po1, vlan 1)
// to the long form used in running-config (GigabitEthernet1, Port-channel1,
// Vlan1) so GetInterfacesStatus/GetNeighbors and GetDeviceConfig agree.
func smbCanonicalIfName(name string) string {
	name = strings.ReplaceAll(strings.TrimSpace(name), " ", "")
	if name == "" {
		return name
	}
	lower := strings.ToLower(name)
	for _, p := range smbIfPrefixes {
		if strings.HasPrefix(lower, p.short) {
			return p.long + name[len(p.short):]
		}
	}
	return name
}

func smbPreamble() []sshCmd {
	// terminal datadump is the pager-off command that actually works on
	// SG300/SG350; terminal width 0 is best-effort (unrecognized on some
	// trains, which just prints "% Unrecognized command" and continues).
	return []sshCmd{{Cmd: "terminal datadump"}, {Cmd: "terminal width 0"}}
}

func (driver *CiscoSMBDriver) runCLI(cmds ...sshCmd) (string, error) {
	full := append(smbPreamble(), cmds...)
	return sshRunCLI(context.Background(), driver.p, full)
}

func (driver *CiscoSMBDriver) runCLIBatch(cmds ...sshCmd) ([]string, error) {
	full := append(smbPreamble(), cmds...)
	return sshRunCLIBatch(context.Background(), driver.p, full)
}

// ----------------------------------------------------------------------
// External methods
// ----------------------------------------------------------------------

func (driver *CiscoSMBDriver) Exec(cmd string) (*ExecModel, error) {
	output, err := driver.runCLI(sshCmd{Cmd: cmd})
	if err != nil {
		return nil, err
	}
	return &ExecModel{Result: output}, nil
}

// Version scrapes "show version", "show inventory" and "show system".
// SG300 prints "SW version <ver>"; SG350/C1200/C1300 print an Active-image
// block with "Version: <ver>". PID/VID/SN come from inventory; MAC from
// "show system". Fields those commands don't expose are left zero-valued.
func (driver *CiscoSMBDriver) Version() (*VersionModel, error) {
	outputs, err := driver.runCLIBatch(
		sshCmd{Cmd: "show version"},
		sshCmd{Cmd: "show inventory"},
		sshCmd{Cmd: "show system"},
	)
	if err != nil {
		return nil, err
	}
	return smbParseVersion(outputs[len(outputs)-3], outputs[len(outputs)-2], outputs[len(outputs)-1]), nil
}

func smbParseVersion(versionOut, inventoryOut, systemOut string) *VersionModel {
	v := &VersionModel{}
	if m := smbReSWVersion.FindStringSubmatch(versionOut); m != nil {
		v.Version = m[1]
	} else if m := smbReActiveVersion.FindStringSubmatch(versionOut); m != nil {
		v.Version = m[1]
	}
	if m := smbReInventory.FindStringSubmatch(inventoryOut); m != nil {
		v.ModelName = m[1]
		v.HardwareRevision = m[2]
		v.SerialNumber = m[3]
	}
	if m := smbReSystemMAC.FindStringSubmatch(systemOut); m != nil {
		v.SystemMacAddress = m[1]
	}
	return v
}

func (driver *CiscoSMBDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	output, err := driver.runCLI(sshCmd{Cmd: "show running-config"})
	if err != nil {
		return nil, err
	}
	return &RunningConfigModel{ConfigStr: output}, nil
}

// RunningConfigSave answers the "Overwrite file [startup-config].... (Y/N)"
// prompt with "y".
func (driver *CiscoSMBDriver) RunningConfigSave() error {
	_, err := sshRunCLI(context.Background(), driver.p, []sshCmd{
		{Cmd: "copy running-config startup-config"},
		{Cmd: "y"},
	})
	return err
}

func (driver *CiscoSMBDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	outputs, err := driver.runCLIBatch(
		sshCmd{Cmd: "show interfaces description"},
		sshCmd{Cmd: "show interfaces status"},
		sshCmd{Cmd: "show interfaces configuration"},
	)
	if err != nil {
		return nil, err
	}
	n := len(outputs)
	return smbParseInterfaceStatus(outputs[n-3], outputs[n-2], outputs[n-1]), nil
}

func smbAdminOperStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "up":
		return "UP"
	case "down", "disabled", "notpresent", "not present":
		return "DOWN"
	default:
		if s == "" || s == "--" {
			return "DOWN"
		}
		return strings.ToUpper(s)
	}
}

func smbRowAt(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// smbParseDashTables splits Cisco SMB "show" tables whose header is
// underlined with dashes (description/status/configuration/LLDP). Each
// table is a list of column-split rows; continuation lines (empty first
// column) are concatenated onto the previous row, matching wrapped Port ID
// / System Name cells.
func smbParseDashTables(output string) [][][]string {
	lines := strings.Split(output, "\n")
	var tables [][][]string
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimRight(lines[i], "\r")
		if !smbReTableUnderline.MatchString(strings.TrimSpace(trimmed)) {
			i++
			continue
		}
		cols := smbColumnEnds(trimmed)
		var rows [][]string
		i++
		for i < len(lines) {
			line := strings.TrimRight(lines[i], "\r")
			if strings.TrimSpace(line) == "" {
				break
			}
			if smbReTableUnderline.MatchString(strings.TrimSpace(line)) ||
				smbRePortTypeHeader.MatchString(strings.TrimSpace(line)) ||
				smbReDescHeader.MatchString(strings.TrimSpace(line)) ||
				strings.HasPrefix(strings.TrimSpace(line), "Ch ") {
				break
			}
			row := smbSplitColumns(line, cols)
			if smbRowAt(row, 0) == "" && len(rows) > 0 {
				prev := rows[len(rows)-1]
				for c := 1; c < len(row) && c < len(prev); c++ {
					if extra := smbRowAt(row, c); extra != "" {
						if prev[c] == "" {
							prev[c] = extra
						} else {
							prev[c] += extra
						}
					}
				}
			} else if smbRowAt(row, 0) != "" {
				rows = append(rows, row)
			}
			i++
		}
		if len(rows) > 0 {
			tables = append(tables, rows)
		}
	}
	return tables
}

func smbColumnEnds(underline string) []int {
	var ends []int
	inDash := false
	for i, r := range underline {
		if r == '-' {
			inDash = true
			continue
		}
		if inDash {
			ends = append(ends, i)
			inDash = false
		}
	}
	ends = append(ends, 10000)
	return ends
}

func smbSplitColumns(line string, ends []int) []string {
	row := make([]string, len(ends))
	start := 0
	runes := []rune(line)
	for i, end := range ends {
		if start >= len(runes) {
			break
		}
		cut := end
		if cut > len(runes) {
			cut = len(runes)
		}
		if cut < start {
			cut = start
		}
		row[i] = strings.TrimSpace(string(runes[start:cut]))
		start = end
	}
	return row
}

func smbParseInterfaceStatus(descOut, statusOut, configOut string) []*netboxtool.NBInterface {
	byName := map[string]*netboxtool.NBInterface{}
	var order []string
	add := func(name string) *netboxtool.NBInterface {
		name = smbCanonicalIfName(name)
		if name == "" {
			return nil
		}
		if iface, ok := byName[name]; ok {
			return iface
		}
		iface := &netboxtool.NBInterface{Name: name, InterfaceStatus: "UP", LineProtocolStatus: "DOWN"}
		byName[name] = iface
		order = append(order, name)
		return iface
	}
	for _, table := range smbParseDashTables(descOut) {
		for _, row := range table {
			iface := add(smbRowAt(row, 0))
			if iface == nil {
				continue
			}
			if len(row) > 1 {
				iface.Description = strings.TrimSpace(strings.Join(row[1:], " "))
			}
		}
	}
	for _, table := range smbParseDashTables(statusOut) {
		for _, row := range table {
			iface := add(smbRowAt(row, 0))
			if iface == nil {
				continue
			}
			state := smbRowAt(row, 6)
			if state == "" && len(row) > 1 {
				state = smbRowAt(row, len(row)-3)
			}
			iface.LineProtocolStatus = smbAdminOperStatus(state)
		}
	}
	for _, table := range smbParseDashTables(configOut) {
		for _, row := range table {
			iface := add(smbRowAt(row, 0))
			if iface == nil {
				continue
			}
			admin := smbRowAt(row, 6)
			if admin == "" {
				admin = smbRowAt(row, 5)
			}
			if admin != "" && admin != "--" {
				iface.InterfaceStatus = smbAdminOperStatus(admin)
			}
		}
	}
	out := make([]*netboxtool.NBInterface, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

// ----------------------------------------------------------------------
// Update interface description(s) - config-mode CLI command list
// ----------------------------------------------------------------------

func (driver *CiscoSMBDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func smbInterfaceDescriptionsCommands(name []string, intf []*netboxtool.NBInterface) ([]sshCmd, error) {
	if err := checkNamesMatchInterfaces(name, intf); err != nil {
		return nil, err
	}
	cmds := []sshCmd{{Cmd: "configure"}}
	for ix, ifname := range name {
		cmds = append(cmds, sshCmd{Cmd: "interface " + ifname})
		if intf[ix].Description != "" {
			cmds = append(cmds, sshCmd{Cmd: "description " + intf[ix].Description})
		} else {
			cmds = append(cmds, sshCmd{Cmd: "no description"})
		}
		cmds = append(cmds, sshCmd{Cmd: "exit"})
	}
	cmds = append(cmds, sshCmd{Cmd: "end"})
	return cmds, nil
}

func (driver *CiscoSMBDriver) SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error {
	cmds, err := smbInterfaceDescriptionsCommands(name, intf)
	if err != nil {
		return err
	}
	_, err = sshRunCLI(context.Background(), driver.p, cmds)
	return err
}

// ----------------------------------------------------------------------
// Update interface VLANs / switchport config
// ----------------------------------------------------------------------

func smbFormatVlanIDList(ids []int) string {
	return eosFormatVlanIDList(ids)
}

func smbParseVlanIDList(s string) []int {
	return eosParseVlanIDList(s)
}

// smbInterfaceVLANsCommands builds SetInterfaceVLANs' config-mode CLI
// command list - split out as a pure function (no SSH dial) so it's
// testable without a fake SSH server.
func smbInterfaceVLANsCommands(name []string, params []*VLANConfig) ([]sshCmd, error) {
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

	cmds := []sshCmd{{Cmd: "configure"}}
	if len(newVlans) > 0 {
		cmds = append(cmds,
			sshCmd{Cmd: "vlan database"},
			sshCmd{Cmd: "vlan " + smbFormatVlanIDList(newVlans)},
			sshCmd{Cmd: "exit"},
		)
	}
	for ix, ifname := range name {
		p := params[ix]
		cmds = append(cmds, sshCmd{Cmd: "interface " + ifname})
		switch p.SwitchportMode {
		case "access":
			cmds = append(cmds, sshCmd{Cmd: "switchport mode access"})
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, sshCmd{Cmd: "switchport access vlan " + strconv.Itoa(p.UntaggedVLAN)})
			} else {
				cmds = append(cmds, sshCmd{Cmd: "no switchport access vlan"})
			}
		case "trunk":
			cmds = append(cmds, sshCmd{Cmd: "switchport mode trunk"})
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, sshCmd{Cmd: "switchport trunk native vlan " + strconv.Itoa(p.UntaggedVLAN)})
			} else {
				cmds = append(cmds, sshCmd{Cmd: "no switchport trunk native vlan"})
			}
			if len(p.TaggedVLANs) > 0 {
				cmds = append(cmds, sshCmd{Cmd: "switchport trunk allowed vlan " + smbFormatVlanIDList(p.TaggedVLANs)})
			} else {
				cmds = append(cmds, sshCmd{Cmd: "no switchport trunk allowed vlan"})
			}
		case "dot1q-tunnel":
			// Cisco SMB's Q-in-Q edge mode is "customer", not IOS's
			// "dot1q-tunnel". The outer/S-VLAN is switchport customer vlan.
			cmds = append(cmds, sshCmd{Cmd: "switchport mode customer"})
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, sshCmd{Cmd: "switchport customer vlan " + strconv.Itoa(p.UntaggedVLAN)})
			} else {
				cmds = append(cmds, sshCmd{Cmd: "no switchport customer vlan"})
			}
		default:
			cmds = append(cmds,
				sshCmd{Cmd: "switchport mode access"},
				sshCmd{Cmd: "no switchport access vlan"},
				sshCmd{Cmd: "no switchport trunk native vlan"},
				sshCmd{Cmd: "no switchport trunk allowed vlan"},
			)
		}
		cmds = append(cmds, sshCmd{Cmd: "exit"})
	}
	cmds = append(cmds, sshCmd{Cmd: "end"})
	return cmds, nil
}

func (driver *CiscoSMBDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	cmds, err := smbInterfaceVLANsCommands(name, params)
	if err != nil {
		return err
	}
	_, err = sshRunCLI(context.Background(), driver.p, cmds)
	return err
}

// ----------------------------------------------------------------------
// ApplyCLISession - already-rendered CLI lines inside a configure session.
// sessionName is ignored (Cisco SMB has no named configure sessions).
// ----------------------------------------------------------------------

func smbFindCLIError(output string) string {
	loc := smbCLIErrorMarkers.FindStringIndex(output)
	if loc == nil {
		return ""
	}
	line := output[loc[0]:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

func smbCLISessionCommands(cmds []string) []string {
	full := append([]string{"configure"}, cmds...)
	return append(full, "end")
}

func (driver *CiscoSMBDriver) smbCLISession(cmds []string) error {
	output, err := sshRunCLIPipeline(context.Background(), driver.p, smbCLISessionCommands(cmds), nil)
	if err != nil {
		return err
	}
	if msg := smbFindCLIError(output); msg != "" {
		return fmt.Errorf("ciscosmb cli apply failed: %s", msg)
	}
	return nil
}

// ApplyCLISession implements CLISessionApplier for Cisco SMB.
func (driver *CiscoSMBDriver) ApplyCLISession(_ string, cmds []string, _ string) error {
	return driver.smbCLISession(cmds)
}

// ----------------------------------------------------------------------
// GetDeviceConfig / GetNeighbors
// ----------------------------------------------------------------------

type smbInterfaceCfg struct {
	Description      string           `cfg:"description {:toend}"`
	Name             string           `cfg:"name {:toend}"`
	IPv4             *iosxrIPv4Prefix `cfg:"ip address {} {}"`
	IPv6             *netip.Prefix    `cfg:"ipv6 address {}"`
	SwitchportMode   string           `cfg:"switchport mode {}"`
	AccessVlan       int              `cfg:"switchport access vlan {}"`
	TrunkNativeVlan  int              `cfg:"switchport trunk native vlan {}"`
	TrunkAllowedVlan string           `cfg:"switchport trunk allowed vlan {:toend}"`
	CustomerVlan     int              `cfg:"switchport customer vlan {}"`
	ChannelGroup     string           `cfg:"channel-group {:toend}"`
}

type smbVlanDatabaseCfg struct {
	Vlans []string `cfg:"vlan {:toend}"`
}

func smbIsBlockStart(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "interface ") ||
		lower == "vlan database" ||
		strings.HasPrefix(lower, "line ")
}

// smbParseExitConfig turns Cisco SMB CLI (exit-delimited, optionally
// indented) into a ConfigNode tree. Block starters are "interface ",
// "vlan database" and "line "; everything until the next starter or an
// "exit" is a child. Indentation is ignored - SG350 dumps indent interface
// children while vlan-database entries stay at column 0, so the shared
// indent parser in config_context.go cannot be used here.
func smbParseExitConfig(lines []string) []*ConfigNode {
	var nodes []*ConfigNode
	var current *ConfigNode
	skipBanner := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if skipBanner != "" {
			if strings.Contains(line, skipBanner) {
				skipBanner = ""
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "@") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "banner ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				skipBanner = fields[len(fields)-1]
			}
			continue
		}
		if strings.EqualFold(line, "exit") {
			current = nil
			continue
		}
		if smbIsBlockStart(line) {
			current = &ConfigNode{Line: line}
			nodes = append(nodes, current)
			continue
		}
		child := &ConfigNode{Line: line}
		if current != nil {
			current.Children = append(current.Children, child)
		} else {
			nodes = append(nodes, child)
		}
	}
	return nodes
}

func smbConfigNodes(output string) []*ConfigNode {
	return smbParseExitConfig(strings.Split(output, "\n"))
}

func smbInterfaceType(ifname string) string {
	n := strings.ToLower(strings.TrimSpace(ifname))
	switch {
	case strings.HasPrefix(n, "range "):
		return ""
	case strings.Contains(ifname, "."):
		return "virtual"
	case strings.HasPrefix(n, "vlan"):
		return "virtual"
	case strings.HasPrefix(n, "loopback"):
		return "virtual"
	case strings.HasPrefix(n, "port-channel"), strings.HasPrefix(n, "po"):
		return "lag"
	default:
		return "other"
	}
}

func smbParseGlobalVlans(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		VlanDatabase *smbVlanDatabaseCfg        `cfg:"vlan database"`
		Interfaces   map[string]smbInterfaceCfg `cfg:"interface {:toend}"`
	}
	UnmarshalNodes(nodes, &top)

	if top.VlanDatabase != nil {
		for _, batch := range top.VlanDatabase.Vlans {
			// "vlan 10,20-30" or "vlan 4 name VLAN4" - strip a trailing
			// "name ..." so the ID list parser sees only IDs.
			idPart := batch
			if i := strings.Index(strings.ToLower(batch), " name "); i >= 0 {
				idPart = batch[:i]
			}
			ids := smbParseVlanIDList(idPart)
			var name string
			if i := strings.Index(strings.ToLower(batch), " name "); i >= 0 {
				name = strings.TrimSpace(batch[i+6:])
			}
			for _, id := range ids {
				vlan, exists := dc.GlobalVLANs[id]
				if !exists {
					vlan = &VLAN{ID: id}
					dc.GlobalVLANs[id] = vlan
				}
				if name != "" && len(ids) == 1 {
					vlan.Name = name
				}
			}
		}
	}
	for _, ifname := range sortedKeys(top.Interfaces) {
		ic := top.Interfaces[ifname]
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ifname)), "vlan ") &&
			!strings.HasPrefix(strings.ToLower(strings.ReplaceAll(ifname, " ", "")), "vlan") {
			continue
		}
		idStr := strings.TrimSpace(ifname)
		idStr = strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(idStr, " ", "")), "vlan")
		var id int
		fmtSscanInt(idStr, &id)
		if id == 0 {
			continue
		}
		vlan, exists := dc.GlobalVLANs[id]
		if !exists {
			vlan = &VLAN{ID: id}
			dc.GlobalVLANs[id] = vlan
		}
		if ic.Name != "" {
			vlan.Name = ic.Name
		}
	}
}

func smbParseInterfaces(dc *DeviceConfig, nodes []*ConfigNode) {
	var top struct {
		Interfaces map[string]smbInterfaceCfg `cfg:"interface {:toend}"`
	}
	UnmarshalNodes(nodes, &top)

	for _, ifname := range sortedKeys(top.Interfaces) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ifname)), "range ") {
			continue
		}
		canon := smbCanonicalIfName(ifname)
		kind := smbInterfaceType(canon)
		if kind == "" {
			continue
		}
		ic := top.Interfaces[ifname]
		mode := ic.SwitchportMode
		if mode == "customer" {
			mode = "dot1q-tunnel"
		}
		iface := &Interface{
			Name:           canon,
			Type:           kind,
			Description:    ic.Description,
			SwitchportMode: mode,
		}
		if cg := strings.Fields(ic.ChannelGroup); len(cg) > 0 {
			iface.LagID = cg[0]
			iface.LagParent = "Port-channel" + cg[0]
		}
		switch {
		case ic.CustomerVlan != 0:
			iface.UntaggedVLAN = ic.CustomerVlan
		case ic.AccessVlan != 0:
			iface.UntaggedVLAN = ic.AccessVlan
		case ic.TrunkNativeVlan != 0:
			iface.UntaggedVLAN = ic.TrunkNativeVlan
		}
		if ic.TrunkAllowedVlan != "" {
			iface.TaggedVLANs = append(iface.TaggedVLANs, smbParseVlanIDList(ic.TrunkAllowedVlan)...)
		}
		if iface.SwitchportMode == "" && (iface.UntaggedVLAN != 0 || len(iface.TaggedVLANs) > 0) {
			iface.SwitchportMode = "access"
		}
		if ic.IPv4 != nil {
			iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{Address: netip.Prefix(*ic.IPv4)})
		}
		if ic.IPv6 != nil {
			iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{Address: *ic.IPv6})
		}
		dc.AddInterface(iface)
	}
}

func (driver *CiscoSMBDriver) GetDeviceConfig() (*DeviceConfig, error) {
	output, err := driver.runCLI(sshCmd{Cmd: "show running-config"})
	if err != nil {
		return nil, err
	}
	nodes := smbConfigNodes(output)
	dc := NewDeviceConfig()
	smbParseGlobalVlans(dc, nodes)
	smbParseInterfaces(dc, nodes)
	return dc, nil
}

func (driver *CiscoSMBDriver) GetNeighbors() ([]*Neighbor, error) {
	output, err := driver.runCLI(sshCmd{Cmd: "show lldp neighbors"})
	if err != nil {
		return nil, err
	}
	return smbParseNeighbors(output), nil
}

// smbParseNeighbors parses "show lldp neighbors": columns Port, Device ID,
// Port ID, System Name. Device ID is used as SystemID; an empty System Name
// is allowed (C1300 neighbors that only advertise a chassis ID).
func smbParseNeighbors(output string) []*Neighbor {
	var neighbors []*Neighbor
	for _, table := range smbParseDashTables(output) {
		for _, row := range table {
			local := smbCanonicalIfName(smbRowAt(row, 0))
			if local == "" {
				continue
			}
			n := &Neighbor{
				LocalInterface:  local,
				SystemID:        smbRowAt(row, 1),
				RemoteInterface: smbRowAt(row, 2),
				RemoteName:      smbRowAt(row, 3),
			}
			if n.RemoteInterface == "" && n.SystemID == "" {
				continue
			}
			neighbors = append(neighbors, n)
		}
	}
	return neighbors
}
