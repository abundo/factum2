package drivers

// Arista EOS driver - see README-DRIVERS.md for how this driver works
// (NETCONF/eAPI split, eAPI fallback) and how it compares to the others.

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"log/slog"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/netboxtool"
)

// ----------------------------------------------------------------------
// Device
// ----------------------------------------------------------------------

type AristaDriver struct {
	DriverClient
	p DriverParam
}

func init() {
	registerDriver("eos", func(p DriverParam) (DriverClient, error) { return NewAristaDriver(p) })
}

func NewAristaDriver(p DriverParam) (*AristaDriver, error) {
	if err := validateDriverParam(p); err != nil {
		return nil, err
	}
	return &AristaDriver{p: p}, nil
}

// eapiFallback logs why NETCONF didn't answer and runs fallback (the
// equivalent eAPI call) instead. If eAPI fails too, both errors are
// returned - the NETCONF one is usually the more interesting of the pair
// (it's what made this a fallback in the first place), so neither is
// dropped.
func eapiFallback[T any](device string, netconfErr error, fallback func() (T, error)) (T, error) {
	slog.Warn("NETCONF unavailable, falling back to eAPI", "device", device, "error", netconfErr)
	result, err := fallback()
	if err != nil {
		var zero T
		return zero, errors.Join(netconfErr, err)
	}
	return result, nil
}

// ----------------------------------------------------------------------
// External methods - CLI-shaped, eAPI only
// ----------------------------------------------------------------------

// Exec runs one command on the device and returns its output as text. Text
// format rather than JSON because cmd is arbitrary and plenty of EOS
// commands have no JSON representation (the device answers 1003 "not
// supported" for those).
func (driver *AristaDriver) Exec(cmd string) (*ExecModel, error) {
	output, err := eapiRunText(driver.p.Username, driver.p.Password, driver.p.Name, []string{cmd})
	if err != nil {
		return nil, err
	}
	return &ExecModel{Result: output}, nil
}

// versionResponse mirrors "show version"'s JSON result fields.
type versionResponse struct {
	ModelName        string  `json:"modelName"`
	InternalVersion  string  `json:"internalVersion"`
	SystemMacAddress string  `json:"systemMacAddress"`
	SerialNumber     string  `json:"serialNumber"`
	MemTotal         float64 `json:"memTotal"`
	BootupTimestap   float64 `json:"bootupTimestamp"`
	MemFree          float64 `json:"memFree"`
	Version          string  `json:"version"`
	Architecture     string  `json:"architecture"`
	InternalBuildId  string  `json:"internalBuildId"`
	HardwareRevision string  `json:"hardwareRevision"`
}

func (driver *AristaDriver) Version() (*VersionModel, error) {
	results, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, []string{"show version"}, eapiFormatJSON)
	if err != nil {
		return nil, err
	}
	var data versionResponse
	if err := json.Unmarshal(results[0], &data); err != nil {
		return nil, err
	}
	return &VersionModel{
		ModelName:        data.ModelName,
		InternalVersion:  data.InternalVersion,
		SystemMacAddress: data.SystemMacAddress,
		SerialNumber:     data.SerialNumber,
		MemTotal:         data.MemTotal,
		BootupTimestap:   data.BootupTimestap,
		MemFree:          data.MemFree,
		Version:          data.Version,
		Architecture:     data.Architecture,
		InternalBuildId:  data.InternalBuildId,
		HardwareRevision: data.HardwareRevision,
	}, nil
}

// RunningConfigGet returns the running config as CLI text, or - with
// jsonformat - as EOS's own structured JSON rendering of it (a header/
// comments/cmds object), passed through verbatim.
func (driver *AristaDriver) RunningConfigGet(jsonformat bool) (*RunningConfigModel, error) {
	cmds := []string{"show running-config"}
	if !jsonformat {
		output, err := eapiRunText(driver.p.Username, driver.p.Password, driver.p.Name, cmds)
		if err != nil {
			return nil, err
		}
		return &RunningConfigModel{ConfigStr: output}, nil
	}

	results, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, cmds, eapiFormatJSON)
	if err != nil {
		return nil, err
	}
	return &RunningConfigModel{ConfigStr: string(results[0])}, nil
}

func (driver *AristaDriver) RunningConfigSave() error {
	_, err := eapiRunText(driver.p.Username, driver.p.Password, driver.p.Name, []string{"copy running-config startup-config"})
	return err
}

// ----------------------------------------------------------------------
// External methods - NETCONF, falling back to eAPI
// ----------------------------------------------------------------------

// GetInterfacesStatus returns the operational state of every interface via
// NETCONF against openconfig-interfaces, or via eAPI's "show interfaces
// description" if NETCONF isn't available.
func (driver *AristaDriver) GetInterfacesStatus() ([]*netboxtool.NBInterface, error) {
	data, err := netconfGet(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, ocInterfacesFilter{})
	if err != nil {
		return eapiFallback(driver.p.Name, err, driver.getInterfacesStatusEAPI)
	}

	var reply ocInterfacesData
	if err := xml.Unmarshal(data, &reply); err != nil {
		return nil, err
	}
	return interfacesFromOcData(reply), nil
}

// eapiInterfaceDescription is one entry of "show interfaces description"'s
// interfaceDescriptions object.
type eapiInterfaceDescription struct {
	Description        string `json:"description"`
	InterfaceStatus    string `json:"interfaceStatus"`
	LineProtocolStatus string `json:"lineProtocolStatus"`
}

type eapiInterfaceDescriptions struct {
	InterfaceDescriptions json.RawMessage `json:"interfaceDescriptions"`
}

// eapiOperStatus maps eAPI's camelCase line-protocol values onto the
// openconfig-interfaces oper-status vocabulary the NETCONF path returns, so
// a caller can't tell the two transports apart.
func eapiOperStatus(status string) string {
	switch status {
	case "lowerLayerDown":
		return "LOWER_LAYER_DOWN"
	case "notPresent":
		return "NOT_PRESENT"
	default:
		return strings.ToUpper(status)
	}
}

// eapiAdminStatus maps eAPI's interfaceStatus onto openconfig-interfaces'
// admin-status (UP/DOWN). eAPI folds admin state into the same field as
// operational state - only "adminDown"/"disabled" mean shut down, every
// other value ("up", "down", "errdisabled", ...) is an admin-enabled
// interface.
func eapiAdminStatus(status string) string {
	switch status {
	case "adminDown", "disabled":
		return "DOWN"
	default:
		return "UP"
	}
}

func (driver *AristaDriver) getInterfacesStatusEAPI() ([]*netboxtool.NBInterface, error) {
	results, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, []string{"show interfaces description"}, eapiFormatJSON)
	if err != nil {
		return nil, err
	}

	var reply eapiInterfaceDescriptions
	if err := json.Unmarshal(results[0], &reply); err != nil {
		return nil, err
	}
	var byName map[string]eapiInterfaceDescription
	if err := json.Unmarshal(reply.InterfaceDescriptions, &byName); err != nil {
		return nil, err
	}
	// Keyed by interface name, so the encoded key order - the device's own
	// interface order - is the only thing that keeps Ethernet2 ahead of
	// Ethernet2.210 and Ethernet10.
	names, err := jsonObjectKeys(reply.InterfaceDescriptions)
	if err != nil {
		return nil, err
	}

	interfaces := make([]*netboxtool.NBInterface, 0, len(names))
	for _, name := range names {
		ifc := byName[name]
		interfaces = append(interfaces, &netboxtool.NBInterface{
			Name:               name,
			Description:        ifc.Description,
			InterfaceStatus:    eapiAdminStatus(ifc.InterfaceStatus),
			LineProtocolStatus: eapiOperStatus(ifc.LineProtocolStatus),
		})
	}
	return interfaces, nil
}

// ----------------------------------------------------------------------
// Update interface description(s)
// ----------------------------------------------------------------------

func (driver *AristaDriver) SetInterfaceDescription(intf *netboxtool.NBInterface) error {
	return setInterfaceDescription(driver.SetInterfaceDescriptions, intf)
}

func (driver *AristaDriver) SetInterfaceDescriptions(name []string, intf []*netboxtool.NBInterface) error {
	if err := checkNamesMatchInterfaces(name, intf); err != nil {
		return err
	}
	descriptions := make([]string, len(intf))
	for ix, i := range intf {
		descriptions[ix] = i.Description
	}
	edit := newOcEditInterfaces(name, descriptions)
	err := netconfEditConfig(driver.p.Username, driver.p.Password, driver.p.Name, driver.p.Port, edit)
	if err == nil {
		return nil
	}
	_, err = eapiFallback(driver.p.Name, err, func() (struct{}, error) {
		return struct{}{}, driver.setInterfaceDescriptionsEAPI(name, descriptions)
	})
	return err
}

// setInterfaceDescriptionsEAPI applies the same edit as a config-mode CLI
// session. An empty description means "no description" rather than an empty
// one - the same thing the NETCONF edit expresses by omitting the leaf.
func (driver *AristaDriver) setInterfaceDescriptionsEAPI(names []string, descriptions []string) error {
	cmds := []string{"configure"}
	for ix, name := range names {
		cmds = append(cmds, "interface "+name)
		if descriptions[ix] != "" {
			cmds = append(cmds, "description "+descriptions[ix])
		} else {
			cmds = append(cmds, "no description")
		}
	}
	cmds = append(cmds, "end")
	_, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, cmds, eapiFormatText)
	return err
}

// ----------------------------------------------------------------------
// Update interface VLANs / switchport config - eAPI config-mode CLI
// session only (no NETCONF path: openconfig-vlan is unverified/unused
// anywhere in this codebase, unlike openconfig-interfaces).
// ----------------------------------------------------------------------

// eosFormatVlanIDList renders a VLAN ID list as EOS's comma-separated
// syntax - the write-side counterpart of eosParseVlanIDList's read.
func eosFormatVlanIDList(ids []int) string {
	parts := make([]string, len(ids))
	for ix, id := range ids {
		parts[ix] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// SetInterfaceVLANs pushes switchport mode and VLAN membership to a set of
// interfaces, mirroring the config-mode CLI a human operator would type -
// same eAPI transport as setInterfaceDescriptionsEAPI. Every VID referenced
// by any target interface is first ensured to exist in EOS's own VLAN
// database (a top-level "vlan <id>" block, matching eosParseGlobalVlans'
// read shape) - EOS rejects "switchport access vlan"/"switchport trunk
// allowed vlan" for a VID with no such block.
func (driver *AristaDriver) SetInterfaceVLANs(name []string, params []*VLANConfig) error {
	if err := checkNamesMatchVLANConfigs(name, params); err != nil {
		return err
	}

	cmds := []string{"configure"}
	seenVlans := map[int]bool{}
	ensureVlan := func(vid int) {
		if vid == 0 || seenVlans[vid] {
			return
		}
		seenVlans[vid] = true
		cmds = append(cmds, "vlan "+strconv.Itoa(vid))
	}
	for _, p := range params {
		ensureVlan(p.UntaggedVLAN)
		for _, vid := range p.TaggedVLANs {
			ensureVlan(vid)
		}
	}

	for ix, ifname := range name {
		p := params[ix]
		cmds = append(cmds, "interface "+ifname)
		switch p.SwitchportMode {
		case "access":
			cmds = append(cmds, "switchport mode access")
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, "switchport access vlan "+strconv.Itoa(p.UntaggedVLAN))
			} else {
				cmds = append(cmds, "no switchport access vlan")
			}
		case "trunk":
			cmds = append(cmds, "switchport mode trunk")
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, "switchport trunk native vlan "+strconv.Itoa(p.UntaggedVLAN))
			} else {
				cmds = append(cmds, "no switchport trunk native vlan")
			}
			if len(p.TaggedVLANs) > 0 {
				cmds = append(cmds, "switchport trunk allowed vlan "+eosFormatVlanIDList(p.TaggedVLANs))
			} else {
				cmds = append(cmds, "no switchport trunk allowed vlan")
			}
		case "dot1q-tunnel":
			// The Q-in-Q outer/S-VLAN tag is carried in the access vlan,
			// same as UntaggedVLAN's read-side meaning for this mode (see
			// Interface.UntaggedVLAN's doc comment).
			cmds = append(cmds, "switchport mode dot1q-tunnel")
			if p.UntaggedVLAN != 0 {
				cmds = append(cmds, "switchport access vlan "+strconv.Itoa(p.UntaggedVLAN))
			} else {
				cmds = append(cmds, "no switchport access vlan")
			}
		default:
			cmds = append(cmds, "no switchport")
		}
	}
	cmds = append(cmds, "end")
	_, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, cmds, eapiFormatText)
	return err
}

// ----------------------------------------------------------------------
// GetDeviceConfig / GetNeighbors - parse running config and LLDP neighbors
// into the shared drivers.DeviceConfig/Neighbor shapes, for
// internal/device-sync.
// ----------------------------------------------------------------------

// eapiConfigSection is one node of eAPI's "show running-config" JSON tree -
// a config line's own sub-lines, keyed by line text (order matters, so the
// raw bytes are kept around for jsonObjectKeys rather than decoding
// straight into a map).
type eapiConfigSection struct {
	Cmds json.RawMessage `json:"cmds"`
}

// eapiConfigLines decodes one config section into its child lines, in
// device order, plus each line's own raw sub-section for further descent.
// Returns nil, nil, nil for a leaf line (no "cmds" of its own).
func eapiConfigLines(raw json.RawMessage) ([]string, map[string]json.RawMessage, error) {
	var section eapiConfigSection
	if err := json.Unmarshal(raw, &section); err != nil {
		return nil, nil, err
	}
	if len(section.Cmds) == 0 {
		return nil, nil, nil
	}
	keys, err := jsonObjectKeys(section.Cmds)
	if err != nil {
		return nil, nil, err
	}
	var byLine map[string]json.RawMessage
	if err := json.Unmarshal(section.Cmds, &byLine); err != nil {
		return nil, nil, err
	}
	return keys, byLine, nil
}

// eapiDescend walks down path, each step matching the first key with that
// prefix and descending into its sub-section, returning that sub-section's
// own ordered lines.
func eapiDescend(keys []string, byLine map[string]json.RawMessage, path ...string) ([]string, map[string]json.RawMessage) {
	curKeys, curByLine := keys, byLine
	for _, want := range path {
		var raw json.RawMessage
		found := false
		for _, k := range curKeys {
			if strings.HasPrefix(k, want) {
				raw = curByLine[k]
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
		curKeys, curByLine, _ = eapiConfigLines(raw)
	}
	return curKeys, curByLine
}

var (
	eosReInterface    = regexp.MustCompile(`^interface (\S+)`)
	eosReDescription  = regexp.MustCompile(`^description (.*)$`)
	eosReChannelGroup = regexp.MustCompile(`^channel-group (\S+)`)
	eosReVRF          = regexp.MustCompile(`^vrf (\S+)`)
	eosReVlanID       = regexp.MustCompile(`^vlan id (\S+)`)
	eosReIPAddress    = regexp.MustCompile(`^ip address (\S+)`)
	eosReIPv6Address  = regexp.MustCompile(`^ipv6 address (\S+)`)
	eosRePseudowireID = regexp.MustCompile(`^pseudowire-id (\S+)`)
	eosReMTU          = regexp.MustCompile(`^mtu (\S+)`)
	eosReControlWord  = regexp.MustCompile(`^control-word`)
	eosReNeighbor     = regexp.MustCompile(`^neighbor (\S+)`)
	eosRePseudowire   = regexp.MustCompile(`^pseudowire (\S+)`)
	eosReConnectorIf  = regexp.MustCompile(`^connector \d+ interface (\S+)`)
	eosReConnectorPW  = regexp.MustCompile(`^connector \d+ pseudowire ldp (\S+)`)
	eosRePatch        = regexp.MustCompile(`^patch (\S+)`)
	eosReRD           = regexp.MustCompile(`^rd (\S+)`)
	eosReRTImport     = regexp.MustCompile(`^route-target import (\S+)`)
	eosReRTExport     = regexp.MustCompile(`^route-target export (\S+)`)
	eosReRTBoth       = regexp.MustCompile(`^route-target both (\S+)`)
	eosReVlan         = regexp.MustCompile(`^vlan (\d+)`)
	eosReVRFInstance  = regexp.MustCompile(`^vrf instance (\S+)`)
	eosReVRFLine      = regexp.MustCompile(`^vrf (\S+)$`)

	// L2 switchport lines - see eosParseGlobalVlans for the corresponding
	// top-level "vlan <id>" blocks these VLAN IDs reference.
	eosReVlanName         = regexp.MustCompile(`^name (.+)$`)
	eosReSwitchportMode   = regexp.MustCompile(`^switchport mode (\S+)$`)
	eosReAccessVlan       = regexp.MustCompile(`^switchport access vlan (\d+)$`)
	eosReTrunkNativeVlan  = regexp.MustCompile(`^switchport trunk native vlan (\d+)$`)
	eosReTrunkAllowedVlan = regexp.MustCompile(`^switchport trunk allowed vlan (.+)$`)
)

// eosParseVlanIDList expands EOS's comma-separated VLAN ID list syntax -
// single IDs mixed with hyphenated ranges, e.g. "1-4,6,9-11" - into
// individual IDs. Used for "switchport trunk allowed vlan ...". Malformed
// tokens are skipped rather than aborting the whole line.
func eosParseVlanIDList(s string) []int {
	var ids []int
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if before, after, ok := strings.Cut(tok, "-"); ok {
			start, errStart := strconv.Atoi(before)
			end, errEnd := strconv.Atoi(after)
			if errStart == nil && errEnd == nil {
				for v := start; v <= end; v++ {
					ids = append(ids, v)
				}
			}
			continue
		}
		if v, err := strconv.Atoi(tok); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}

// eosParseGlobalVlans parses top-level "vlan <id>" blocks (EOS's VLAN
// database) into DeviceConfig.GlobalVLANs - distinct from the "vlan <id>"
// blocks eosParseElan reads from inside "router bgp <asn>" (EVPN VLAN-aware
// bundles), since callers pass this only the top-level keys/byLine.
func eosParseGlobalVlans(dc *DeviceConfig, keys []string, byLine map[string]json.RawMessage) {
	for _, line := range keys {
		m := eosReVlan.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var id int
		fmtSscanInt(m[1], &id)
		vlan := &VLAN{ID: id}
		subKeys, _, _ := eapiConfigLines(byLine[line])
		for _, subLine := range subKeys {
			if eosReVlanName.MatchString(subLine) {
				vlan.Name = eosReVlanName.FindStringSubmatch(subLine)[1]
			}
		}
		dc.GlobalVLANs[id] = vlan
	}
}

// eosParseInterfaceAddress parses "ip address"/"ipv6 address"'s captured
// value (a CIDR literal, e.g. "10.0.0.1/24") and appends it to iface -
// invalid/non-CIDR values (like "dhcp") are silently skipped, matching
// driver.add_address's own best-effort handling.
func eosParseInterfaceAddress(iface *Interface, addrStr string) {
	prefix, err := netip.ParsePrefix(addrStr)
	if err != nil {
		return
	}
	iface.IPAddresses = append(iface.IPAddresses, InterfaceAddress{Address: prefix, VRF: iface.VRF})
}

// eosInterfaceType classifies an EOS interface name for Netbox's "type"
// field. Only physical interfaces (Ethernet*, Management*, ...) get "other"
// - Netbox's generic physical type, since the exact PHY/media isn't
// derivable from config alone; Port-Channel is "lag"; subinterfaces and
// logical constructs (Vlan/Loopback/Tunnel/Vxlan) are "virtual". Netbox
// rejects terminating a cable on a "virtual" interface, so misclassifying a
// real port as virtual here silently breaks syncConnections for any
// interface device-sync itself creates.
func eosInterfaceType(ifname string) string {
	switch {
	case strings.Contains(ifname, "."):
		return "virtual"
	case strings.HasPrefix(ifname, "Port-Channel"):
		return "lag"
	case strings.HasPrefix(ifname, "Vlan"), strings.HasPrefix(ifname, "Loopback"),
		strings.HasPrefix(ifname, "Tunnel"), strings.HasPrefix(ifname, "Vxlan"):
		return "virtual"
	default:
		return "other"
	}
}

// eosParseInterfaces makes two passes over each interface's lines: the
// first sets scalar fields (vrf must be known before addresses are parsed,
// since InterfaceAddress carries it).
func eosParseInterfaces(dc *DeviceConfig, keys []string, byLine map[string]json.RawMessage) {
	for _, line := range keys {
		m := eosReInterface.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ifname := m[1]
		iface := &Interface{Name: ifname, Type: eosInterfaceType(ifname)}

		subKeys, subByLine, _ := eapiConfigLines(byLine[line])
		for _, subLine := range subKeys {
			switch {
			case eosReDescription.MatchString(subLine):
				iface.Description = eosReDescription.FindStringSubmatch(subLine)[1]
			case eosReChannelGroup.MatchString(subLine):
				iface.LagID = eosReChannelGroup.FindStringSubmatch(subLine)[1]
				iface.LagParent = "interface Port-Channel " + iface.LagID
			case eosReVRF.MatchString(subLine):
				iface.VRF = eosReVRF.FindStringSubmatch(subLine)[1]
			case eosReVlanID.MatchString(subLine):
				iface.VlanID = eosReVlanID.FindStringSubmatch(subLine)[1]
			case eosReSwitchportMode.MatchString(subLine):
				iface.SwitchportMode = eosReSwitchportMode.FindStringSubmatch(subLine)[1]
			case eosReAccessVlan.MatchString(subLine):
				fmtSscanInt(eosReAccessVlan.FindStringSubmatch(subLine)[1], &iface.UntaggedVLAN)
			case eosReTrunkNativeVlan.MatchString(subLine):
				fmtSscanInt(eosReTrunkNativeVlan.FindStringSubmatch(subLine)[1], &iface.UntaggedVLAN)
			case eosReTrunkAllowedVlan.MatchString(subLine):
				rest := eosReTrunkAllowedVlan.FindStringSubmatch(subLine)[1]
				iface.TaggedVLANs = append(iface.TaggedVLANs, eosParseVlanIDList(rest)...)
			}
		}
		for _, subLine := range subKeys {
			switch {
			case eosReIPAddress.MatchString(subLine):
				eosParseInterfaceAddress(iface, eosReIPAddress.FindStringSubmatch(subLine)[1])
			case eosReIPv6Address.MatchString(subLine):
				eosParseInterfaceAddress(iface, eosReIPv6Address.FindStringSubmatch(subLine)[1])
			}
			_ = subByLine
		}
		// EOS's own default switchport mode is "access" - "switchport
		// access vlan N" alone (no explicit "switchport mode access")
		// is valid running-config meaning access mode, but left
		// SwitchportMode "" above since only an explicit "switchport
		// mode ..." line sets it. Left blank,
		// models.SwitchportModeToNetboxMode maps it to a nil Netbox
		// "mode", which Netbox then rejects the untagged_vlan/
		// tagged_vlans PATCH against ("Interface mode does not support
		// untagged vlan"). Only default when a switchport line was
		// actually seen - an interface with none of these is routed,
		// not an access port with elided config.
		if iface.SwitchportMode == "" && (iface.UntaggedVLAN != 0 || len(iface.TaggedVLANs) > 0) {
			iface.SwitchportMode = "access"
		}
		dc.AddInterface(iface)
	}
}

// eosParseEline parses pseudowires under "mpls ldp"->"pseudowires", then
// patch panels connecting an interface to a pseudowire under "patch panel".
func eosParseEline(dc *DeviceConfig, keys []string, byLine map[string]json.RawMessage) {
	pseudowires := map[string]*Pseudowire{}
	pwKeys, pwByLine := eapiDescend(keys, byLine, "mpls ldp", "pseudowires")
	for _, line := range pwKeys {
		m := eosRePseudowire.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pw := &Pseudowire{Name: m[1]}
		subKeys, _, _ := eapiConfigLines(pwByLine[line])
		for _, subLine := range subKeys {
			switch {
			case eosRePseudowireID.MatchString(subLine):
				fmtSscanInt(eosRePseudowireID.FindStringSubmatch(subLine)[1], &pw.PWID)
			case eosReMTU.MatchString(subLine):
				fmtSscanInt(eosReMTU.FindStringSubmatch(subLine)[1], &pw.MTU)
			case eosReControlWord.MatchString(subLine):
				pw.ControlWord = true
			case eosReNeighbor.MatchString(subLine):
				pw.Neighbor = eosReNeighbor.FindStringSubmatch(subLine)[1]
			}
		}
		pseudowires[pw.Name] = pw
	}

	patchKeys, patchByLine := eapiDescend(keys, byLine, "patch panel")
	for _, line := range patchKeys {
		m := eosRePatch.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		eline := &ELINE{Name: m[1]}
		addConn := func(conn any) {
			if eline.Conn1 == nil {
				eline.Conn1 = conn
			} else {
				eline.Conn2 = conn
			}
		}
		subKeys, _, _ := eapiConfigLines(patchByLine[line])
		for _, subLine := range subKeys {
			switch {
			case eosReConnectorIf.MatchString(subLine):
				ifname := eosReConnectorIf.FindStringSubmatch(subLine)[1]
				addConn(dc.InterfacesByName[ifname])
			case eosReConnectorPW.MatchString(subLine):
				pwName := eosReConnectorPW.FindStringSubmatch(subLine)[1]
				addConn(pseudowires[pwName])
			}
		}
		dc.ELINEs[eline.Name] = eline
	}
}

// eosParseElan parses a BGP VLAN block per ELAN, with interfaces attached
// by matching Interface.VlanID.
func eosParseElan(dc *DeviceConfig, keys []string, byLine map[string]json.RawMessage) {
	bgpKeys, bgpByLine := eapiDescend(keys, byLine, "router bgp ")
	for _, line := range bgpKeys {
		m := eosReVlan.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		elan := &ELAN{Name: m[1]}
		subKeys, _, _ := eapiConfigLines(bgpByLine[line])
		for _, subLine := range subKeys {
			switch {
			case eosReRD.MatchString(subLine):
				elan.RD = eosReRD.FindStringSubmatch(subLine)[1]
			case eosReRTImport.MatchString(subLine):
				elan.RTImport = eosReRTImport.FindStringSubmatch(subLine)[1]
			case eosReRTExport.MatchString(subLine):
				elan.RTExport = eosReRTExport.FindStringSubmatch(subLine)[1]
			case eosReRTBoth.MatchString(subLine):
				both := eosReRTBoth.FindStringSubmatch(subLine)[1]
				elan.RTImport, elan.RTExport = both, both
			}
		}
		for _, iface := range dc.Interfaces {
			if iface.VlanID != "" && iface.VlanID == elan.Name {
				elan.Interfaces = append(elan.Interfaces, iface.Name)
			}
		}
		dc.ELANs[elan.Name] = elan
	}
}

// eosParseVRF parses global "vrf instance <name>" lines, then links each
// parsed interface into its VRF's Interfaces list.
func eosParseVRF(dc *DeviceConfig, keys []string, byLine map[string]json.RawMessage) {
	for _, line := range keys {
		m := eosReVRFInstance.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		vrf := &VRF{Name: m[1]}
		subKeys, _, _ := eapiConfigLines(byLine[line])
		for _, subLine := range subKeys {
			if eosReDescription.MatchString(subLine) {
				vrf.Description = eosReDescription.FindStringSubmatch(subLine)[1]
			}
		}
		dc.VRFs[vrf.Name] = vrf
	}
	for _, iface := range dc.Interfaces {
		if iface.VRF == "" {
			continue
		}
		if vrf, ok := dc.VRFs[iface.VRF]; ok {
			vrf.Interfaces = append(vrf.Interfaces, iface.Name)
		}
	}
}

// eosParseL3VPN parses "router bgp <asn>" -> "vrf <name>" blocks, each
// attached to the VRF of the same name.
func eosParseL3VPN(dc *DeviceConfig, keys []string, byLine map[string]json.RawMessage) {
	bgpKeys, bgpByLine := eapiDescend(keys, byLine, "router bgp ")
	for _, line := range bgpKeys {
		m := eosReVRFLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		vrf, ok := dc.VRFs[name]
		if !ok {
			continue
		}
		l3vpn := &L3VPN{Name: name, VRF: vrf}
		subKeys, _, _ := eapiConfigLines(bgpByLine[line])
		for _, subLine := range subKeys {
			switch {
			case eosReRD.MatchString(subLine):
				vrf.RD = eosReRD.FindStringSubmatch(subLine)[1]
			case eosReRTImport.MatchString(subLine):
				vrf.RTImport = eosReRTImport.FindStringSubmatch(subLine)[1]
			case eosReRTExport.MatchString(subLine):
				vrf.RTExport = eosReRTExport.FindStringSubmatch(subLine)[1]
			case eosReRTBoth.MatchString(subLine):
				both := eosReRTBoth.FindStringSubmatch(subLine)[1]
				vrf.RTImport, vrf.RTExport = both, both
			}
		}
		dc.L3VPNs[l3vpn.Name] = l3vpn
	}
}

// GetDeviceConfig fetches "show running-config" over eAPI and parses it
// into interfaces/VRFs/ELINE/ELAN/L3VPN.
func (driver *AristaDriver) GetDeviceConfig() (*DeviceConfig, error) {
	results, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, []string{"show running-config"}, eapiFormatJSON)
	if err != nil {
		return nil, err
	}
	topKeys, topByLine, err := eapiConfigLines(results[0])
	if err != nil {
		return nil, err
	}

	dc := NewDeviceConfig()
	eosParseGlobalVlans(dc, topKeys, topByLine)
	eosParseInterfaces(dc, topKeys, topByLine)
	eosParseEline(dc, topKeys, topByLine)
	eosParseElan(dc, topKeys, topByLine)
	eosParseVRF(dc, topKeys, topByLine)
	eosParseL3VPN(dc, topKeys, topByLine)
	return dc, nil
}

// eapiLLDPNeighbor is one entry of "show lldp neighbors"'s lldpNeighbors list.
type eapiLLDPNeighbor struct {
	Port           string `json:"port"`
	NeighborDevice string `json:"neighborDevice"`
	NeighborPort   string `json:"neighborPort"`
}

type eapiLLDPNeighbors struct {
	LLDPNeighbors []eapiLLDPNeighbor `json:"lldpNeighbors"`
}

// GetNeighbors returns LLDP neighbors via eAPI.
func (driver *AristaDriver) GetNeighbors() ([]*Neighbor, error) {
	results, err := eapiRunCmds(driver.p.Username, driver.p.Password, driver.p.Name, []string{"show lldp neighbors"}, eapiFormatJSON)
	if err != nil {
		return nil, err
	}
	var reply eapiLLDPNeighbors
	if err := json.Unmarshal(results[0], &reply); err != nil {
		return nil, err
	}
	neighbors := make([]*Neighbor, 0, len(reply.LLDPNeighbors))
	for _, n := range reply.LLDPNeighbors {
		if n.NeighborDevice == "" {
			continue
		}
		neighbors = append(neighbors, &Neighbor{
			LocalInterface:  n.Port,
			RemoteName:      n.NeighborDevice,
			RemoteInterface: n.NeighborPort,
		})
	}
	return neighbors, nil
}
