package drivers

import (
	"strings"
	"testing"
)

var iosxrTestConfig = strings.Split(`interface TenGigE0/0/2/1
 description CORE PEER=OK1-R2
 mtu 9216
 vrf POLARIX
 ipv4 address 172.27.247.23 255.255.255.254
 ipv6 address 2a00:ff40:ffff:fffe::17/127
!
interface Bundle-Ether1920
 bundle id 1920
!
interface Bundle-Ether1920.1927
 description CN1927
!
interface TenGigE0/0/0/0.500
 description CN00500
!
interface TenGigE0/0/0/0.501 l2transport
 description CN00501-L2
!
vrf POLARIX
 address-family ipv4 unicast
  import route-target
   1234:700
  !
  export route-target
   1234:700
  !
 !
!
router bgp 1234
 vrf POLARIX
  rd 1234:700
 !
!
l2vpn
 xconnect group GC
  p2p CN1927
   interface Bundle-Ether1920.1927
   neighbor ipv4 172.27.250.28 pw-id 1001927
  !
 !
 bridge group TEST
  bridge-domain CN00500
   interface TenGigE0/0/0/0.500
   evi 500
  !
 !
!
evpn
 evi 500
  bgp
   rd 1234:500
   route-target import 1234:500
   route-target export 1234:500
  !
 !
!
`, "\n")

func iosxrTestDeviceConfig(t *testing.T) *DeviceConfig {
	t.Helper()
	nodes := ParseConfigContext(iosxrTestConfig, "!")
	dc := NewDeviceConfig()
	iosxrParseInterfaces(dc, nodes)
	iosxrParseEline(dc, nodes)
	iosxrParseElan(dc, nodes)
	iosxrParseVRF(dc, nodes)
	iosxrParseL3VPN(dc, nodes)
	return dc
}

func TestIOSXRInterfaceType(t *testing.T) {
	cases := map[string]string{
		"TenGigE0/0/2/1":         "other", // physical - must stay cable-attachable in Netbox
		"GigabitEthernet0/0/0/0": "other",
		"HundredGigE0/0/0/0":     "other",
		"Bundle-Ether1920":       "lag",
		"Bundle-Ether1920.1927":  "virtual", // subinterface
		"BVI100":                 "virtual",
		"Loopback0":              "virtual",
	}
	for ifname, want := range cases {
		if got := iosxrInterfaceType(ifname); got != want {
			t.Errorf("iosxrInterfaceType(%q) = %q, want %q", ifname, got, want)
		}
	}
}

func TestIOSXRParseInterfaces(t *testing.T) {
	dc := iosxrTestDeviceConfig(t)

	iface, ok := dc.InterfacesByName["TenGigE0/0/2/1"]
	if !ok {
		t.Fatalf("interface TenGigE0/0/2/1 not parsed; have %v", dc.InterfacesByName)
	}
	if iface.Description != "CORE PEER=OK1-R2" {
		t.Errorf("Description = %q", iface.Description)
	}
	if iface.VRF != "POLARIX" {
		t.Errorf("VRF = %q", iface.VRF)
	}
	if iface.Type != "other" {
		t.Errorf("Type = %q, want other (physical interface)", iface.Type)
	}
	if len(iface.IPAddresses) != 2 {
		t.Fatalf("got %d addresses, want 2: %+v", len(iface.IPAddresses), iface.IPAddresses)
	}
	if got := iface.IPAddresses[0].Address.String(); got != "172.27.247.23/31" {
		t.Errorf("ipv4 address = %q", got)
	}
	if got := iface.IPAddresses[1].Address.String(); got != "2a00:ff40:ffff:fffe::17/127" {
		t.Errorf("ipv6 address = %q", got)
	}

	bundle, ok := dc.InterfacesByName["Bundle-Ether1920"]
	if !ok {
		t.Fatal("interface Bundle-Ether1920 not parsed")
	}
	if bundle.LagID != "1920" || bundle.LagParent != "interface Bundle-Ether 1920" {
		t.Errorf("LagID/LagParent = %q/%q", bundle.LagID, bundle.LagParent)
	}
	if bundle.Type != "lag" {
		t.Errorf("Bundle-Ether1920 Type = %q, want lag", bundle.Type)
	}

	sub, ok := dc.InterfacesByName["Bundle-Ether1920.1927"]
	if !ok || sub.Type != "virtual" {
		t.Errorf("subinterface not parsed as virtual: %+v", sub)
	}
	if sub.L2Transport {
		t.Errorf("Bundle-Ether1920.1927 L2Transport = true, want false (no l2transport keyword)")
	}

	l3sub, ok := dc.InterfacesByName["TenGigE0/0/0/0.500"]
	if !ok || l3sub.L2Transport {
		t.Errorf("TenGigE0/0/0/0.500 L2Transport = %v, want false", l3sub.L2Transport)
	}

	l2sub, ok := dc.InterfacesByName["TenGigE0/0/0/0.501"]
	if !ok {
		t.Fatal("interface TenGigE0/0/0/0.501 not parsed")
	}
	if !l2sub.L2Transport {
		t.Error("TenGigE0/0/0/0.501 L2Transport = false, want true (l2transport keyword)")
	}
	if l2sub.Description != "CN00501-L2" {
		t.Errorf("TenGigE0/0/0/0.501 Description = %q", l2sub.Description)
	}
}

func TestIOSXRParseVRF(t *testing.T) {
	dc := iosxrTestDeviceConfig(t)
	vrf, ok := dc.VRFs["POLARIX"]
	if !ok {
		t.Fatalf("VRF POLARIX not parsed; have %v", dc.VRFs)
	}
	if vrf.RTImport != "1234:700" || vrf.RTExport != "1234:700" {
		t.Errorf("RTImport/RTExport = %q/%q", vrf.RTImport, vrf.RTExport)
	}
}

func TestIOSXRParseL3VPN(t *testing.T) {
	dc := iosxrTestDeviceConfig(t)
	l3vpn, ok := dc.L3VPNs["POLARIX"]
	if !ok {
		t.Fatalf("L3VPN POLARIX not parsed; have %v", dc.L3VPNs)
	}
	if l3vpn.VRF == nil || l3vpn.VRF.RD != "1234:700" {
		t.Errorf("L3VPN.VRF = %+v", l3vpn.VRF)
	}
}

func TestIOSXRParseEline(t *testing.T) {
	dc := iosxrTestDeviceConfig(t)
	eline, ok := dc.ELINEs["CN1927"]
	if !ok {
		t.Fatalf("ELINE CN1927 not parsed; have %v", dc.ELINEs)
	}
	iface, ok := eline.Conn1.(*Interface)
	if !ok || iface.Name != "Bundle-Ether1920.1927" {
		t.Errorf("Conn1 = %+v, want interface Bundle-Ether1920.1927", eline.Conn1)
	}
	pw, ok := eline.Conn2.(*Pseudowire)
	if !ok {
		t.Fatalf("Conn2 = %+v, want *Pseudowire", eline.Conn2)
	}
	if pw.Neighbor != "172.27.250.28" || pw.PWID != 1001927 {
		t.Errorf("pw = %+v", pw)
	}
}

func TestIOSXRParseElan(t *testing.T) {
	dc := iosxrTestDeviceConfig(t)
	elan, ok := dc.ELANs["CN00500"]
	if !ok {
		t.Fatalf("ELAN CN00500 not parsed; have %v", dc.ELANs)
	}
	if elan.RD != "1234:500" || elan.RTImport != "1234:500" || elan.RTExport != "1234:500" {
		t.Errorf("elan rd/rt = %+v", elan)
	}
	if len(elan.Interfaces) != 1 || elan.Interfaces[0] != "TenGigE0/0/0/0.500" {
		t.Errorf("elan.Interfaces = %+v", elan.Interfaces)
	}
}

func TestIOSXRParseNeighbors(t *testing.T) {
	output := strings.Join([]string{
		"Local Interface: GigabitEthernet100/0/0/7",
		"Chassis id: 744a.a40e.457c",
		"Port id: gei-0/1/1/23",
		"System Name: MIR-EXT-SW01",
		"---------------------------------------------------------------------",
		"Local Interface: GigabitEthernet100/0/0/8.100",
		"Port id: gei-0/1/1/24",
		"System Name: SUBIF-IGNORED",
		"---------------------------------------------------------------------",
		"",
	}, "\n")

	neighbors := iosxrParseNeighbors(output)
	if len(neighbors) != 1 {
		t.Fatalf("got %d neighbors, want 1 (subinterface should be skipped): %+v", len(neighbors), neighbors)
	}
	n := neighbors[0]
	if n.LocalInterface != "GigabitEthernet100/0/0/7" || n.RemoteInterface != "gei-0/1/1/23" || n.RemoteName != "MIR-EXT-SW01" {
		t.Errorf("neighbor = %+v", n)
	}
}
