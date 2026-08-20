package drivers

import (
	"encoding/json"
	"testing"
)

// eosTestConfigJSON builds a fixture matching eAPI's "show running-config"
// JSON shape ({"cmds": {"<line>": {"cmds": {...}}, ...}}), deep enough to
// exercise interfaces/eline/elan/vrf/l3vpn parsing without a live device.
func eosTestConfigJSON(t *testing.T) (keys []string, byLine map[string]json.RawMessage) {
	t.Helper()
	config := map[string]any{
		"cmds": map[string]any{
			"interface Ethernet1": map[string]any{
				"cmds": map[string]any{
					"description CORE PEER":       map[string]any{},
					"vrf MGMT":                    map[string]any{},
					"ip address 10.0.0.1/24":      map[string]any{},
					"ipv6 address 2001:db8::1/64": map[string]any{},
				},
			},
			"interface Ethernet2": map[string]any{
				"cmds": map[string]any{
					"vlan id 3100": map[string]any{},
				},
			},
			"interface Ethernet3": map[string]any{
				"cmds": map[string]any{
					"switchport mode access":    map[string]any{},
					"switchport access vlan 10": map[string]any{},
				},
			},
			"interface Ethernet4": map[string]any{
				"cmds": map[string]any{
					"switchport mode trunk":                     map[string]any{},
					"switchport trunk native vlan 1":            map[string]any{},
					"switchport trunk allowed vlan 10,20,30-32": map[string]any{},
				},
			},
			"interface Ethernet5": map[string]any{
				"cmds": map[string]any{
					"switchport access vlan 20": map[string]any{},
				},
			},
			"interface Port-Channel10": map[string]any{
				"cmds": map[string]any{
					"channel-group 10": map[string]any{},
				},
			},
			"vlan 10": map[string]any{
				"cmds": map[string]any{
					"name Data": map[string]any{},
				},
			},
			"vlan 20": map[string]any{
				"cmds": map[string]any{},
			},
			"vrf instance MGMT": map[string]any{
				"cmds": map[string]any{
					"description management": map[string]any{},
				},
			},
			"mpls ldp": map[string]any{
				"cmds": map[string]any{
					"pseudowires": map[string]any{
						"cmds": map[string]any{
							"pseudowire CN00078": map[string]any{
								"cmds": map[string]any{
									"neighbor 172.27.250.28": map[string]any{},
									"pseudowire-id 1000078":  map[string]any{},
									"mtu 9100":               map[string]any{},
									"control-word":           map[string]any{},
								},
							},
						},
					},
				},
			},
			"patch panel": map[string]any{
				"cmds": map[string]any{
					"patch CN00570-13": map[string]any{
						"cmds": map[string]any{
							"connector 1 interface Ethernet1":    map[string]any{},
							"connector 2 pseudowire ldp CN00078": map[string]any{},
						},
					},
				},
			},
			"router bgp 1234": map[string]any{
				"cmds": map[string]any{
					"vlan 3100": map[string]any{
						"cmds": map[string]any{
							"rd 1234:13100":                map[string]any{},
							"route-target both 1234:13100": map[string]any{},
						},
					},
					"vrf MGMT": map[string]any{
						"cmds": map[string]any{
							"rd 1234:1": map[string]any{},
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	keys, byLine, err = eapiConfigLines(raw)
	if err != nil {
		t.Fatalf("eapiConfigLines: %v", err)
	}
	return keys, byLine
}

func eosTestDeviceConfig(t *testing.T) *DeviceConfig {
	t.Helper()
	keys, byLine := eosTestConfigJSON(t)
	dc := NewDeviceConfig()
	eosParseGlobalVlans(dc, keys, byLine)
	eosParseInterfaces(dc, keys, byLine)
	eosParseEline(dc, keys, byLine)
	eosParseElan(dc, keys, byLine)
	eosParseVRF(dc, keys, byLine)
	eosParseL3VPN(dc, keys, byLine)
	return dc
}

func TestEOSInterfaceType(t *testing.T) {
	cases := map[string]string{
		"Ethernet1":      "other", // physical - must stay cable-attachable in Netbox
		"Management1":    "other",
		"Port-Channel10": "lag",
		"Ethernet2.210":  "virtual", // subinterface
		"Vlan100":        "virtual",
		"Loopback0":      "virtual",
		"Tunnel1":        "virtual",
		"Vxlan1":         "virtual",
	}
	for ifname, want := range cases {
		if got := eosInterfaceType(ifname); got != want {
			t.Errorf("eosInterfaceType(%q) = %q, want %q", ifname, got, want)
		}
	}
}

func TestEOSParseInterfaces(t *testing.T) {
	dc := eosTestDeviceConfig(t)

	iface, ok := dc.InterfacesByName["Ethernet1"]
	if !ok {
		t.Fatalf("interface Ethernet1 not parsed; have %v", dc.InterfacesByName)
	}
	if iface.Description != "CORE PEER" {
		t.Errorf("Description = %q", iface.Description)
	}
	if iface.VRF != "MGMT" {
		t.Errorf("VRF = %q", iface.VRF)
	}
	if iface.Type != "other" {
		t.Errorf("Type = %q, want other (physical interface)", iface.Type)
	}
	if len(iface.IPAddresses) != 2 {
		t.Fatalf("got %d addresses, want 2: %+v", len(iface.IPAddresses), iface.IPAddresses)
	}
	if got := iface.IPAddresses[0].Address.String(); got != "10.0.0.1/24" {
		t.Errorf("ipv4 = %q", got)
	}
	if got := iface.IPAddresses[1].Address.String(); got != "2001:db8::1/64" {
		t.Errorf("ipv6 = %q", got)
	}

	po, ok := dc.InterfacesByName["Port-Channel10"]
	if !ok || po.Type != "lag" {
		t.Errorf("Port-Channel10 = %+v, want Type=lag", po)
	}
	if po.LagID != "10" || po.LagParent != "interface Port-Channel 10" {
		t.Errorf("Port-Channel10 LagID/LagParent = %q/%q", po.LagID, po.LagParent)
	}
}

func TestEOSParseVlanIDList(t *testing.T) {
	cases := []struct {
		s    string
		want []int
	}{
		{"10,20,30-32", []int{10, 20, 30, 31, 32}},
		{"1-4,6,9-11", []int{1, 2, 3, 4, 6, 9, 10, 11}},
	}
	for _, c := range cases {
		got := eosParseVlanIDList(c.s)
		if len(got) != len(c.want) {
			t.Fatalf("eosParseVlanIDList(%q) = %v, want %v", c.s, got, c.want)
		}
		for ix := range got {
			if got[ix] != c.want[ix] {
				t.Errorf("eosParseVlanIDList(%q) = %v, want %v", c.s, got, c.want)
			}
		}
	}
}

func TestEOSParseGlobalVlans(t *testing.T) {
	dc := eosTestDeviceConfig(t)
	if len(dc.GlobalVLANs) != 2 {
		t.Fatalf("got %d VLANs, want 2: %+v", len(dc.GlobalVLANs), dc.GlobalVLANs)
	}
	if got := dc.GlobalVLANs[10].Name; got != "Data" {
		t.Errorf("VLAN 10 name = %q, want Data", got)
	}
	if got := dc.GlobalVLANs[20].Name; got != "" {
		t.Errorf("VLAN 20 name = %q, want empty", got)
	}
}

func TestEOSParseInterfaceSwitchport(t *testing.T) {
	dc := eosTestDeviceConfig(t)

	access, ok := dc.InterfacesByName["Ethernet3"]
	if !ok {
		t.Fatalf("interface Ethernet3 not parsed; have %v", dc.InterfacesByName)
	}
	if access.SwitchportMode != "access" {
		t.Errorf("access SwitchportMode = %q, want access", access.SwitchportMode)
	}
	if access.UntaggedVLAN != 10 {
		t.Errorf("access UntaggedVLAN = %d, want 10", access.UntaggedVLAN)
	}
	if len(access.TaggedVLANs) != 0 {
		t.Errorf("access TaggedVLANs = %v, want empty", access.TaggedVLANs)
	}

	trunk, ok := dc.InterfacesByName["Ethernet4"]
	if !ok {
		t.Fatalf("interface Ethernet4 not parsed; have %v", dc.InterfacesByName)
	}
	if trunk.SwitchportMode != "trunk" {
		t.Errorf("trunk SwitchportMode = %q, want trunk", trunk.SwitchportMode)
	}
	if trunk.UntaggedVLAN != 1 {
		t.Errorf("trunk UntaggedVLAN = %d, want 1", trunk.UntaggedVLAN)
	}
	want := []int{10, 20, 30, 31, 32}
	if len(trunk.TaggedVLANs) != len(want) {
		t.Fatalf("trunk TaggedVLANs = %v, want %v", trunk.TaggedVLANs, want)
	}
	for ix := range want {
		if trunk.TaggedVLANs[ix] != want[ix] {
			t.Errorf("trunk TaggedVLANs = %v, want %v", trunk.TaggedVLANs, want)
		}
	}

	// Ethernet5 has "switchport access vlan 20" with no explicit
	// "switchport mode access" line - valid EOS running-config, since
	// access is EOS's own default switchport mode. SwitchportMode must
	// still come out "access" here, not "" (which
	// models.SwitchportModeToNetboxMode maps to a nil Netbox interface
	// mode, and Netbox then rejects the untagged_vlan PATCH against with
	// "Interface mode does not support untagged vlan").
	elided, ok := dc.InterfacesByName["Ethernet5"]
	if !ok {
		t.Fatalf("interface Ethernet5 not parsed; have %v", dc.InterfacesByName)
	}
	if elided.SwitchportMode != "access" {
		t.Errorf("elided-mode SwitchportMode = %q, want access", elided.SwitchportMode)
	}
	if elided.UntaggedVLAN != 20 {
		t.Errorf("elided-mode UntaggedVLAN = %d, want 20", elided.UntaggedVLAN)
	}
}

func TestEOSParseVRF(t *testing.T) {
	dc := eosTestDeviceConfig(t)
	vrf, ok := dc.VRFs["MGMT"]
	if !ok {
		t.Fatalf("VRF MGMT not parsed; have %v", dc.VRFs)
	}
	if vrf.Description != "management" {
		t.Errorf("Description = %q", vrf.Description)
	}
	if len(vrf.Interfaces) != 1 || vrf.Interfaces[0] != "Ethernet1" {
		t.Errorf("vrf.Interfaces = %+v, want [Ethernet1]", vrf.Interfaces)
	}
}

func TestEOSParseL3VPN(t *testing.T) {
	dc := eosTestDeviceConfig(t)
	l3vpn, ok := dc.L3VPNs["MGMT"]
	if !ok {
		t.Fatalf("L3VPN MGMT not parsed; have %v", dc.L3VPNs)
	}
	if l3vpn.VRF == nil || l3vpn.VRF.RD != "1234:1" {
		t.Errorf("l3vpn.VRF = %+v", l3vpn.VRF)
	}
}

func TestEOSParseEline(t *testing.T) {
	dc := eosTestDeviceConfig(t)
	eline, ok := dc.ELINEs["CN00570-13"]
	if !ok {
		t.Fatalf("ELINE CN00570-13 not parsed; have %v", dc.ELINEs)
	}
	iface, ok := eline.Conn1.(*Interface)
	if !ok || iface.Name != "Ethernet1" {
		t.Errorf("Conn1 = %+v, want interface Ethernet1", eline.Conn1)
	}
	pw, ok := eline.Conn2.(*Pseudowire)
	if !ok {
		t.Fatalf("Conn2 = %+v, want *Pseudowire", eline.Conn2)
	}
	if pw.Neighbor != "172.27.250.28" || pw.PWID != 1000078 || pw.MTU != 9100 || !pw.ControlWord {
		t.Errorf("pw = %+v", pw)
	}
}

func TestEOSParseElan(t *testing.T) {
	dc := eosTestDeviceConfig(t)
	elan, ok := dc.ELANs["3100"]
	if !ok {
		t.Fatalf("ELAN 3100 not parsed; have %v", dc.ELANs)
	}
	if elan.RD != "1234:13100" || elan.RTImport != "1234:13100" || elan.RTExport != "1234:13100" {
		t.Errorf("elan rd/rt = %+v", elan)
	}
	if len(elan.Interfaces) != 1 || elan.Interfaces[0] != "Ethernet2" {
		t.Errorf("elan.Interfaces = %+v, want [Ethernet2]", elan.Interfaces)
	}
}

func TestEOSParseNeighborsShape(t *testing.T) {
	raw := json.RawMessage(`{"lldpNeighbors":[
		{"port":"Ethernet1","neighborDevice":"switch1.example.com","neighborPort":"Ethernet5"},
		{"port":"Ethernet2","neighborDevice":"","neighborPort":""}
	]}`)
	var reply eapiLLDPNeighbors
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var neighbors []*Neighbor
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
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1 (empty neighborDevice skipped): %+v", len(neighbors), neighbors)
	}
	if neighbors[0].LocalInterface != "Ethernet1" || neighbors[0].RemoteName != "switch1.example.com" {
		t.Errorf("neighbor = %+v", neighbors[0])
	}
}
