package drivers

import (
	"strings"
	"testing"
)

func TestVrpInterfaceType(t *testing.T) {
	cases := map[string]string{
		"GigabitEthernet0/0/1":     "other",   // physical - must stay cable-attachable in Netbox
		"GigabitEthernet0/0/1.100": "virtual", // subinterface
	}
	for ifname, want := range cases {
		if got := vrpInterfaceType(ifname); got != want {
			t.Errorf("vrpInterfaceType(%q) = %q, want %q", ifname, got, want)
		}
	}
}

func TestVrpParseInterfaces(t *testing.T) {
	lines := strings.Split(`interface GigabitEthernet0/0/1
 description WAN-link
 vrf MGMT
 ip address 10.0.0.1 255.255.255.0
 ipv6 address 2001:db8::1/64
#
interface NULL0
 description should-be-ignored
#
interface GigabitEthernet0/0/1.100 mode l2
 description L2-subif
 encapsulation dot1q vid 100
 bridge-domain 100
#
`, "\n")

	nodes := ParseConfigContext(lines, "#")
	dc := NewDeviceConfig()
	vrpParseInterfaces(dc, nodes)

	if _, ok := dc.InterfacesByName["NULL0"]; ok {
		t.Error("NULL0 should be ignored, per vrpIgnoreInterfaces")
	}
	iface, ok := dc.InterfacesByName["GigabitEthernet0/0/1"]
	if !ok {
		t.Fatalf("interface GigabitEthernet0/0/1 not parsed; have %v", dc.InterfacesByName)
	}
	if iface.Description != "WAN-link" {
		t.Errorf("Description = %q", iface.Description)
	}
	if iface.VRF != "MGMT" {
		t.Errorf("VRF = %q", iface.VRF)
	}
	if len(iface.IPAddresses) != 2 {
		t.Fatalf("got %d addresses, want 2: %+v", len(iface.IPAddresses), iface.IPAddresses)
	}
	if got := iface.IPAddresses[0].Address.String(); got != "10.0.0.1/24" {
		t.Errorf("ipv4 address = %q", got)
	}
	if iface.IPAddresses[0].VRF != "MGMT" {
		t.Errorf("ipv4 address VRF = %q", iface.IPAddresses[0].VRF)
	}
	if got := iface.IPAddresses[1].Address.String(); got != "2001:db8::1/64" {
		t.Errorf("ipv6 address = %q", got)
	}
	if iface.L2Transport {
		t.Error("GigabitEthernet0/0/1 L2Transport = true, want false (no 'mode l2')")
	}

	l2sub, ok := dc.InterfacesByName["GigabitEthernet0/0/1.100"]
	if !ok {
		t.Fatal("interface GigabitEthernet0/0/1.100 not parsed")
	}
	if !l2sub.L2Transport {
		t.Error("GigabitEthernet0/0/1.100 L2Transport = false, want true ('mode l2')")
	}
	if l2sub.Description != "L2-subif" {
		t.Errorf("GigabitEthernet0/0/1.100 Description = %q", l2sub.Description)
	}

	// eline/elan/vrf/l3vpn are no-ops for VRP - nothing parses them.
	if len(dc.VRFs) != 0 || len(dc.ELINEs) != 0 || len(dc.ELANs) != 0 || len(dc.L3VPNs) != 0 {
		t.Errorf("expected empty VRF/ELINE/ELAN/L3VPN maps, got VRFs=%v ELINEs=%v ELANs=%v L3VPNs=%v",
			dc.VRFs, dc.ELINEs, dc.ELANs, dc.L3VPNs)
	}
}

func TestVrpParseVlanIDList(t *testing.T) {
	cases := []struct {
		fields []string
		want   []int
	}{
		{[]string{"994", "2736"}, []int{994, 2736}},
		{[]string{"318", "to", "319", "347", "994", "to", "996"}, []int{318, 319, 347, 994, 995, 996}},
		{[]string{"367", "993"}, []int{367, 993}},
	}
	for _, c := range cases {
		got := vrpParseVlanIDList(c.fields)
		if len(got) != len(c.want) {
			t.Fatalf("vrpParseVlanIDList(%v) = %v, want %v", c.fields, got, c.want)
		}
		for ix := range got {
			if got[ix] != c.want[ix] {
				t.Errorf("vrpParseVlanIDList(%v) = %v, want %v", c.fields, got, c.want)
			}
		}
	}
}

func TestVrpParseGlobalVlans(t *testing.T) {
	lines := strings.Split(`vlan batch 994 2736
vlan batch 318 to 319 347
vlan 994
 name MGMT
#
`, "\n")

	nodes := ParseConfigContext(lines, "#")
	dc := NewDeviceConfig()
	vrpParseGlobalVlans(dc, nodes)

	if len(dc.GlobalVLANs) != 5 {
		t.Fatalf("got %d VLANs, want 5: %+v", len(dc.GlobalVLANs), dc.GlobalVLANs)
	}
	for _, id := range []int{994, 2736, 318, 319, 347} {
		if _, ok := dc.GlobalVLANs[id]; !ok {
			t.Errorf("VLAN %d not registered", id)
		}
	}
	if got := dc.GlobalVLANs[994].Name; got != "MGMT" {
		t.Errorf("VLAN 994 name = %q, want MGMT", got)
	}
	if got := dc.GlobalVLANs[2736].Name; got != "" {
		t.Errorf("VLAN 2736 name = %q, want empty (batch-only)", got)
	}
}

func TestVrpParseInterfaceSwitchport(t *testing.T) {
	lines := strings.Split(`interface GigabitEthernet0/0/1
 port link-type access
 port default vlan 367
#
interface GigabitEthernet0/0/10
 port link-type trunk
 undo port trunk allow-pass vlan 1
 port trunk allow-pass vlan 367 993
 port trunk pvid vlan 100
#
`, "\n")

	nodes := ParseConfigContext(lines, "#")
	dc := NewDeviceConfig()
	vrpParseInterfaces(dc, nodes)

	access := dc.InterfacesByName["GigabitEthernet0/0/1"]
	if access.SwitchportMode != "access" {
		t.Errorf("access SwitchportMode = %q, want access", access.SwitchportMode)
	}
	if access.UntaggedVLAN != 367 {
		t.Errorf("access UntaggedVLAN = %d, want 367", access.UntaggedVLAN)
	}
	if len(access.TaggedVLANs) != 0 {
		t.Errorf("access TaggedVLANs = %v, want empty", access.TaggedVLANs)
	}

	trunk := dc.InterfacesByName["GigabitEthernet0/0/10"]
	if trunk.SwitchportMode != "trunk" {
		t.Errorf("trunk SwitchportMode = %q, want trunk", trunk.SwitchportMode)
	}
	if trunk.UntaggedVLAN != 100 {
		t.Errorf("trunk UntaggedVLAN = %d, want 100", trunk.UntaggedVLAN)
	}
	if len(trunk.TaggedVLANs) != 2 || trunk.TaggedVLANs[0] != 367 || trunk.TaggedVLANs[1] != 993 {
		t.Errorf("trunk TaggedVLANs = %v, want [367 993]", trunk.TaggedVLANs)
	}
}

func TestVrpParseNeighbors(t *testing.T) {
	output := strings.Join([]string{
		"GigabitEthernet0/0/1 has 1 neighbor(s):",
		"",
		"Neighbor index :1",
		"Chassis type   :macAddress",
		"Chassis ID     :bca5-11af-298c",
		"Port ID type   :local",
		"Port ID        :1/g48",
		"System name         :accesssw1_bhus",
		"GigabitEthernet0/0/2 has 0 neighbor(s)",
	}, "\n")

	neighbors := vrpParseNeighbors(output)
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1: %+v", len(neighbors), neighbors)
	}
	n := neighbors[0]
	if n.LocalInterface != "GigabitEthernet0/0/1" {
		t.Errorf("LocalInterface = %q", n.LocalInterface)
	}
	if n.SystemID != "bca5-11af-298c" {
		t.Errorf("SystemID = %q", n.SystemID)
	}
	if n.RemoteInterface != "1/g48" {
		t.Errorf("RemoteInterface = %q", n.RemoteInterface)
	}
	if n.RemoteName != "accesssw1_bhus" {
		t.Errorf("RemoteName = %q", n.RemoteName)
	}
}

func TestVrpParseInterfaceDescriptions(t *testing.T) {
	output := strings.Join([]string{
		"PHY: Physical",
		"*down: administratively down",
		"Interface                   PHY     Protocol Description",
		"GigabitEthernet0/0/1        up      up       WAN-link",
		"GigabitEthernet0/0/2        *down   down",
	}, "\n")

	interfaces := vrpParseInterfaceDescriptions(output)
	if len(interfaces) != 2 {
		t.Fatalf("got %d interfaces, want 2: %+v", len(interfaces), interfaces)
	}
	if interfaces[0].Name != "GigabitEthernet0/0/1" || interfaces[0].Description != "WAN-link" {
		t.Errorf("interfaces[0] = %+v", interfaces[0])
	}
	if interfaces[0].InterfaceStatus != "UP" || interfaces[0].LineProtocolStatus != "UP" {
		t.Errorf("interfaces[0] status = %+v", interfaces[0])
	}
	if interfaces[1].InterfaceStatus != "DOWN" {
		t.Errorf("interfaces[1] InterfaceStatus = %q, want DOWN (admin down)", interfaces[1].InterfaceStatus)
	}
}
