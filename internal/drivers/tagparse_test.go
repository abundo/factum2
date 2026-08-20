package drivers

import (
	"net/netip"
	"strings"
	"testing"
)

func TestTagParseBasic(t *testing.T) {
	type ifaceCfg struct {
		Description string        `cfg:"description {:toend}"`
		VRF         string        `cfg:"vrf {}"`
		MTU         int           `cfg:"mtu {}"`
		Shutdown    bool          `cfg:"shutdown"`
		Address     *netip.Prefix `cfg:"ip address {}"`
	}
	type root struct {
		Interfaces map[string]ifaceCfg `cfg:"interface {}"`
	}

	lines := strings.Split(`interface eth1
 description uplink to core
 vrf INTERNET
 mtu 8000
 shutdown
 ip address 192.168.1.1/24
`, "\n")
	nodes := ParseConfigContext(lines, "")

	var got root
	if err := UnmarshalNodes(nodes, &got); err != nil {
		t.Fatalf("UnmarshalNodes: %v", err)
	}

	iface, ok := got.Interfaces["eth1"]
	if !ok {
		t.Fatalf("interface eth1 not parsed; have %v", got.Interfaces)
	}
	if iface.Description != "uplink to core" {
		t.Errorf("Description = %q", iface.Description)
	}
	if iface.VRF != "INTERNET" {
		t.Errorf("VRF = %q", iface.VRF)
	}
	if iface.MTU != 8000 {
		t.Errorf("MTU = %d", iface.MTU)
	}
	if !iface.Shutdown {
		t.Error("Shutdown = false, want true (presence flag)")
	}
	if iface.Address == nil || iface.Address.String() != "192.168.1.1/24" {
		t.Errorf("Address = %v", iface.Address)
	}
}

func TestTagParseOptionalToken(t *testing.T) {
	type ifaceCfg struct {
		Description string `cfg:"description {:toend}"`
		L2Transport bool   `cfg:"l2transport"`
	}
	type root struct {
		Interfaces map[string]ifaceCfg `cfg:"interface {} [l2transport]"`
	}

	lines := strings.Split(`interface Gi0/0/1.100
 description L3-subif
interface Gi0/0/1.200 l2transport
 description L2-subif
`, "\n")
	nodes := ParseConfigContext(lines, "")

	var got root
	if err := UnmarshalNodes(nodes, &got); err != nil {
		t.Fatalf("UnmarshalNodes: %v", err)
	}

	l3, ok := got.Interfaces["Gi0/0/1.100"]
	if !ok || l3.L2Transport {
		t.Errorf("Gi0/0/1.100 L2Transport = %v, want false", l3.L2Transport)
	}
	l2, ok := got.Interfaces["Gi0/0/1.200"]
	if !ok || !l2.L2Transport {
		t.Errorf("Gi0/0/1.200 L2Transport = %v, want true", l2.L2Transport)
	}
	if l2.Description != "L2-subif" {
		t.Errorf("Gi0/0/1.200 Description = %q", l2.Description)
	}
}

func TestTagParseMultiWordOptionalToken(t *testing.T) {
	type ifaceCfg struct {
		Description string `cfg:"description {:toend}"`
		L2Transport bool   `cfg:"mode l2"`
	}
	type root struct {
		Interfaces map[string]ifaceCfg `cfg:"interface {} [mode l2]"`
	}

	lines := strings.Split(`interface GE0/0/1.100
 description L3-subif
interface GE0/0/1.200 mode l2
 description L2-subif
`, "\n")
	nodes := ParseConfigContext(lines, "")

	var got root
	if err := UnmarshalNodes(nodes, &got); err != nil {
		t.Fatalf("UnmarshalNodes: %v", err)
	}
	if len(got.Interfaces) != 2 {
		t.Fatalf("got %d interfaces, want 2: %+v", len(got.Interfaces), got.Interfaces)
	}
	l3, ok := got.Interfaces["GE0/0/1.100"]
	if !ok || l3.L2Transport {
		t.Errorf("GE0/0/1.100 L2Transport = %v, want false", l3.L2Transport)
	}
	l2, ok := got.Interfaces["GE0/0/1.200"]
	if !ok || !l2.L2Transport {
		t.Errorf("GE0/0/1.200 L2Transport = %v, want true", l2.L2Transport)
	}
}

func TestTagParseSliceAndSpecificity(t *testing.T) {
	type vrfCfg struct {
		RTImport []string `cfg:"route-target import {}"`
	}
	type root struct {
		// Deliberately declared in the "wrong" order (general before
		// specific) - specificity sorting, not field order, must decide
		// which matches "ip address dhcp".
		Generic string            `cfg:"ip address {}"`
		DHCP    bool              `cfg:"ip address dhcp"`
		VRFs    map[string]vrfCfg `cfg:"vrf {}"`
	}

	lines := strings.Split(`ip address dhcp
vrf POLARIX
 route-target import 1:1
 route-target import 1:2
`, "\n")
	nodes := ParseConfigContext(lines, "")

	var got root
	if err := UnmarshalNodes(nodes, &got); err != nil {
		t.Fatalf("UnmarshalNodes: %v", err)
	}
	if !got.DHCP {
		t.Error("DHCP = false, want true (more specific tag should win over the generic capture)")
	}
	if got.Generic != "" {
		t.Errorf("Generic = %q, want empty (the dhcp-specific tag should have claimed the line)", got.Generic)
	}
	vrf, ok := got.VRFs["POLARIX"]
	if !ok || len(vrf.RTImport) != 2 || vrf.RTImport[0] != "1:1" || vrf.RTImport[1] != "1:2" {
		t.Errorf("VRFs[POLARIX].RTImport = %+v", vrf.RTImport)
	}
}

func TestTagParseUnmodeledLinesIgnored(t *testing.T) {
	type root struct {
		Description string `cfg:"description {:toend}"`
	}
	lines := strings.Split(`description kept
some-unmodeled-command with args
`, "\n")
	nodes := ParseConfigContext(lines, "")

	var got root
	if err := UnmarshalNodes(nodes, &got); err != nil {
		t.Fatalf("UnmarshalNodes: %v", err)
	}
	if got.Description != "kept" {
		t.Errorf("Description = %q", got.Description)
	}
}
