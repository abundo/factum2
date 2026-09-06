package drivers

import (
	"strings"
	"testing"
)

func TestSmbInterfaceType(t *testing.T) {
	cases := map[string]string{
		"GigabitEthernet1":     "other",
		"GigabitEthernet1/0/1": "other",
		"GigabitEthernet1.100": "virtual",
		"Vlan1":                "virtual",
		"Port-channel1":        "lag",
		"range gi1/0/1-4":      "",
	}
	for ifname, want := range cases {
		if got := smbInterfaceType(ifname); got != want {
			t.Errorf("smbInterfaceType(%q) = %q, want %q", ifname, got, want)
		}
	}
}

const smbIndentedConfig = `config-file-header
MAIN-SG350
v2.5.0.79 / RTESLA2.5_930_364_088
CLI v1.0
!
vlan database
vlan 10,20,30,40,100-101
vlan 10 name Computers
exit
!
interface vlan 1
 ip address 10.3.100.1 255.255.255.0
 no ip address dhcp
!
interface vlan 10
 name Computers
 ip address 10.3.10.1 255.255.255.0
!
interface GigabitEthernet1
 description TO_QC_NOPOE
 switchport mode trunk
 switchport trunk native vlan 100
 switchport trunk allowed vlan 10,20,50,100-101
!
interface GigabitEthernet2
 switchport access vlan 10
!
interface GigabitEthernet8
 description qinq-edge
 switchport mode customer
 switchport customer vlan 200
!
interface Port-channel1
 description lag-uplink
 switchport mode trunk
!
`

const smbExitConfig = `config-file-header
AA307-02
v1.2.5.76 / R750_NIK_1_2_584_002
CLI v1.0
!
vlan database
vlan 4,5
exit
!
interface range gi1/0/1-4
speed 1000
exit
interface vlan 1
ip address 1.1.1.1 255.0.0.0
exit
interface gi1/0/10
description access-port
switchport mode access
switchport access vlan 20
exit
interface gi1/0/12
switchport mode trunk
switchport trunk allowed vlan 1,20,30
switchport trunk native vlan 1
exit
`

func TestSmbParseIndentedConfig(t *testing.T) {
	nodes := smbConfigNodes(smbIndentedConfig)
	dc := NewDeviceConfig()
	smbParseGlobalVlans(dc, nodes)
	smbParseInterfaces(dc, nodes)

	for _, id := range []int{10, 20, 30, 40, 100, 101} {
		if _, ok := dc.GlobalVLANs[id]; !ok {
			t.Errorf("VLAN %d not registered: %+v", id, dc.GlobalVLANs)
		}
	}
	if got := dc.GlobalVLANs[10].Name; got != "Computers" {
		t.Errorf("VLAN 10 name = %q, want Computers", got)
	}

	trunk, ok := dc.InterfacesByName["GigabitEthernet1"]
	if !ok {
		t.Fatalf("GigabitEthernet1 not parsed; have %v", dc.InterfacesByName)
	}
	if trunk.Description != "TO_QC_NOPOE" {
		t.Errorf("Description = %q", trunk.Description)
	}
	if trunk.SwitchportMode != "trunk" {
		t.Errorf("SwitchportMode = %q, want trunk", trunk.SwitchportMode)
	}
	if trunk.UntaggedVLAN != 100 {
		t.Errorf("UntaggedVLAN = %d, want 100", trunk.UntaggedVLAN)
	}
	if !reflectInts(trunk.TaggedVLANs, []int{10, 20, 50, 100, 101}) {
		t.Errorf("TaggedVLANs = %v", trunk.TaggedVLANs)
	}

	access, ok := dc.InterfacesByName["GigabitEthernet2"]
	if !ok {
		t.Fatal("GigabitEthernet2 not parsed")
	}
	if access.SwitchportMode != "access" {
		t.Errorf("elided-mode SwitchportMode = %q, want access", access.SwitchportMode)
	}
	if access.UntaggedVLAN != 10 {
		t.Errorf("UntaggedVLAN = %d, want 10", access.UntaggedVLAN)
	}

	qinq, ok := dc.InterfacesByName["GigabitEthernet8"]
	if !ok {
		t.Fatal("GigabitEthernet8 not parsed")
	}
	if qinq.SwitchportMode != "dot1q-tunnel" {
		t.Errorf("customer mode = %q, want dot1q-tunnel", qinq.SwitchportMode)
	}
	if qinq.UntaggedVLAN != 200 {
		t.Errorf("customer vlan = %d, want 200", qinq.UntaggedVLAN)
	}

	svi, ok := dc.InterfacesByName["Vlan1"]
	if !ok {
		t.Fatalf("Vlan1 not parsed; have %v", dc.InterfacesByName)
	}
	if svi.Type != "virtual" {
		t.Errorf("Vlan1 Type = %q, want virtual", svi.Type)
	}
	if len(svi.IPAddresses) != 1 || svi.IPAddresses[0].Address.String() != "10.3.100.1/24" {
		t.Errorf("Vlan1 addresses = %+v", svi.IPAddresses)
	}

	lag, ok := dc.InterfacesByName["Port-channel1"]
	if !ok {
		t.Fatal("Port-channel1 not parsed")
	}
	if lag.Type != "lag" {
		t.Errorf("Port-channel1 Type = %q, want lag", lag.Type)
	}

	if _, ok := dc.InterfacesByName["range gi1/0/1-4"]; ok {
		t.Error("interface range should be skipped")
	}
	if len(dc.VRFs) != 0 || len(dc.ELINEs) != 0 || len(dc.ELANs) != 0 || len(dc.L3VPNs) != 0 {
		t.Errorf("expected empty VRF/ELINE/ELAN/L3VPN maps")
	}
}

func TestSmbParseExitConfig(t *testing.T) {
	nodes := smbConfigNodes(smbExitConfig)
	dc := NewDeviceConfig()
	smbParseGlobalVlans(dc, nodes)
	smbParseInterfaces(dc, nodes)

	if _, ok := dc.GlobalVLANs[4]; !ok {
		t.Errorf("VLAN 4 not registered: %+v", dc.GlobalVLANs)
	}
	for name := range dc.InterfacesByName {
		if strings.Contains(strings.ToLower(name), "range") {
			t.Errorf("interface range leaked as %q", name)
		}
	}
	access, ok := dc.InterfacesByName["GigabitEthernet1/0/10"]
	if !ok {
		t.Fatalf("GigabitEthernet1/0/10 not parsed; have %v", dc.InterfacesByName)
	}
	if access.Description != "access-port" || access.SwitchportMode != "access" || access.UntaggedVLAN != 20 {
		t.Errorf("access = %+v", access)
	}
	svi, ok := dc.InterfacesByName["Vlan1"]
	if !ok {
		t.Fatalf("Vlan1 not parsed; have %v", dc.InterfacesByName)
	}
	if len(svi.IPAddresses) != 1 || svi.IPAddresses[0].Address.String() != "1.1.1.1/8" {
		t.Errorf("Vlan1 addresses = %+v", svi.IPAddresses)
	}
}

func reflectInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSmbParseVersion(t *testing.T) {
	sg300 := strings.Join([]string{
		"SW version    1.4.1.3 ( date  29-Mar-2015 time  16:24:16 )",
		"Boot version    1.3.5.06 ( date  21-Jul-2013 time  15:12:10 )",
		"HW version    V02",
	}, "\n")
	sg350 := strings.Join([]string{
		"Active-image: flash://system/images/image-1.bin",
		"  Version: 2.5.0.79",
		"  MD5 Digest: abcdef",
		"  Date: 16-Jul-2019",
	}, "\n")
	inventory := `NAME: "1"
DESCR: "SG350-28 28-Port Gigabit Managed Switch"
PID: SG350-28-K9     VID: V02     SN: FOC1234ABCD
`
	system := "System Name: MAIN-SG350\nSystem MAC Address: 00:1a:2b:3c:4d:5e\n"

	v := smbParseVersion(sg300, inventory, system)
	if v.Version != "1.4.1.3" {
		t.Errorf("SG300 Version = %q", v.Version)
	}
	if v.ModelName != "SG350-28-K9" || v.HardwareRevision != "V02" || v.SerialNumber != "FOC1234ABCD" {
		t.Errorf("inventory = %+v", v)
	}
	if v.SystemMacAddress != "00:1a:2b:3c:4d:5e" {
		t.Errorf("MAC = %q", v.SystemMacAddress)
	}

	v2 := smbParseVersion(sg350, inventory, system)
	if v2.Version != "2.5.0.79" {
		t.Errorf("SG350 Version = %q", v2.Version)
	}
}

func TestSmbParseInterfaceStatus(t *testing.T) {
	desc := strings.Join([]string{
		"Port        Description",
		"---------   -----------",
		"gi1         TO_QC_NOPOE",
		"gi2",
		"",
		"Ch          Description",
		"---------   -----------",
		"Po1         lag-uplink",
	}, "\n")
	status := strings.Join([]string{
		"                                          Flow Link          Back   Mdix",
		"Port     Type         Duplex  Speed Neg  ctrl State       Pressure Mode",
		"-------- ------------ ------  ----- ---- ---- ----------- -------- -------",
		"gi1      1G-Copper    Full    1000  Off  Off  Up          Disabled off",
		"gi2      1G-Copper      --      --   --   --  Down           --     auto",
	}, "\n")
	config := strings.Join([]string{
		"                                               Flow    Admin     Back   Mdix",
		"Port     Type         Duplex  Speed Neg      ctrl     State   Pressure Mode   Mode",
		"-------- ------------ ------  ----- -------- ----    -------  -------- ------- ----",
		"gi1      1G-Copper      Full    Auto Enabled  Off      Up      Disabled Auto     Auto",
		"gi2      1G-Copper      Full    Auto Enabled  Off      Down    Disabled Auto     Auto",
	}, "\n")

	ifaces := smbParseInterfaceStatus(desc, status, config)
	if len(ifaces) != 3 {
		t.Fatalf("got %d interfaces, want 3: %+v", len(ifaces), ifaces)
	}
	if ifaces[0].Name != "GigabitEthernet1" || ifaces[0].Description != "TO_QC_NOPOE" {
		t.Errorf("ifaces[0] = %+v", ifaces[0])
	}
	if ifaces[0].InterfaceStatus != "UP" || ifaces[0].LineProtocolStatus != "UP" {
		t.Errorf("ifaces[0] status = %+v", ifaces[0])
	}
	if ifaces[1].Name != "GigabitEthernet2" || ifaces[1].InterfaceStatus != "DOWN" || ifaces[1].LineProtocolStatus != "DOWN" {
		t.Errorf("ifaces[1] = %+v", ifaces[1])
	}
	if ifaces[2].Name != "Port-channel1" || ifaces[2].Description != "lag-uplink" {
		t.Errorf("ifaces[2] = %+v", ifaces[2])
	}
}

func TestSmbParseNeighbors(t *testing.T) {
	output := strings.Join([]string{
		"System capability legend:",
		"B - Bridge; R - Router; W - Wlan Access Point; T - telephone;",
		"",
		"Port        Device ID          Port ID         System Name    Capabilities  TTL",
		"--------- ----------------- ----------------- ----------------- ------------ -----",
		"gi3       78:45:aa:15:fa:89 78:45:01:a5:fa:89 somename               H        97",
		"gi7       20:c6:eb:aa:62:2a 20:c6:eb:ef:62:2a                        O        108",
		"gi1          QXN4100162           ETH1                                        113",
	}, "\n")

	neighbors := smbParseNeighbors(output)
	if len(neighbors) != 3 {
		t.Fatalf("got %d neighbors, want 3: %+v", len(neighbors), neighbors)
	}
	n := neighbors[0]
	if n.LocalInterface != "GigabitEthernet3" {
		t.Errorf("LocalInterface = %q", n.LocalInterface)
	}
	if n.SystemID != "78:45:aa:15:fa:89" {
		t.Errorf("SystemID = %q", n.SystemID)
	}
	if n.RemoteInterface != "78:45:01:a5:fa:89" {
		t.Errorf("RemoteInterface = %q", n.RemoteInterface)
	}
	if n.RemoteName != "somename" {
		t.Errorf("RemoteName = %q", n.RemoteName)
	}
	if neighbors[1].RemoteName != "" {
		t.Errorf("empty system name neighbor RemoteName = %q", neighbors[1].RemoteName)
	}
	if neighbors[2].RemoteInterface != "ETH1" || neighbors[2].SystemID != "QXN4100162" {
		t.Errorf("serial-id neighbor = %+v", neighbors[2])
	}
}
