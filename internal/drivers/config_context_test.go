package drivers

import "testing"

func TestParseConfigContext(t *testing.T) {
	lines := []string{
		"interface Ethernet1",
		" description foo",
		" vrf MGMT",
		"interface Ethernet2",
		" description bar",
		"! a comment, ignored",
		"",
		"vrf instance MGMT",
		" description management",
	}
	nodes := ParseConfigContext(lines, "!")
	if len(nodes) != 3 {
		t.Fatalf("got %d top-level nodes, want 3: %+v", len(nodes), nodes)
	}
	if nodes[0].Line != "interface Ethernet1" {
		t.Errorf("nodes[0].Line = %q", nodes[0].Line)
	}
	if len(nodes[0].Children) != 2 {
		t.Fatalf("nodes[0] has %d children, want 2", len(nodes[0].Children))
	}
	if nodes[0].Children[0].Line != "description foo" {
		t.Errorf("nodes[0].Children[0].Line = %q", nodes[0].Children[0].Line)
	}
	if nodes[1].Line != "interface Ethernet2" || len(nodes[1].Children) != 1 {
		t.Errorf("nodes[1] = %+v", nodes[1])
	}
	if nodes[2].Line != "vrf instance MGMT" || len(nodes[2].Children) != 1 {
		t.Errorf("nodes[2] = %+v", nodes[2])
	}
}

func TestParseConfigContextNesting(t *testing.T) {
	lines := []string{
		"l2vpn",
		" xconnect group GC",
		"  p2p CN1927",
		"   interface Bundle-Ether1920.1927",
		"   neighbor ipv4 172.27.250.28 pw-id 1001927",
		" xconnect group GC2",
	}
	nodes := ParseConfigContext(lines, "!")
	if len(nodes) != 1 {
		t.Fatalf("got %d top-level nodes, want 1", len(nodes))
	}
	l2vpn := nodes[0]
	if len(l2vpn.Children) != 2 {
		t.Fatalf("l2vpn has %d children, want 2: %+v", len(l2vpn.Children), l2vpn.Children)
	}
	p2p := l2vpn.Children[0].Children
	if len(p2p) != 1 || p2p[0].Line != "p2p CN1927" {
		t.Fatalf("p2p = %+v", p2p)
	}
	if len(p2p[0].Children) != 2 {
		t.Fatalf("p2p children = %+v", p2p[0].Children)
	}
}

func TestGetContextLevel(t *testing.T) {
	nodes := ParseConfigContext([]string{
		"router bgp 1234",
		" vrf CUSTOMER1",
		"  rd 1234:1",
		" vrf CUSTOMER2",
		"  rd 1234:2",
	}, "!")

	children := GetContextLevel(nodes, "router bgp ")
	if len(children) != 2 {
		t.Fatalf("got %d children, want 2: %+v", len(children), children)
	}
	if children[0].Line != "vrf CUSTOMER1" {
		t.Errorf("children[0].Line = %q", children[0].Line)
	}

	if got := GetContextLevel(nodes, "router bgp ", "vrf CUSTOMER2"); len(got) != 1 || got[0].Line != "rd 1234:2" {
		t.Errorf("nested lookup = %+v", got)
	}

	if got := GetContextLevel(nodes, "no such prefix"); got != nil {
		t.Errorf("expected nil for missing prefix, got %+v", got)
	}
}

func TestFindChild(t *testing.T) {
	nodes := ParseConfigContext([]string{"foo bar", "baz qux"}, "!")
	if got := FindChild(nodes, "foo"); got == nil || got.Line != "foo bar" {
		t.Errorf("FindChild(foo) = %+v", got)
	}
	if got := FindChild(nodes, "nope"); got != nil {
		t.Errorf("FindChild(nope) = %+v, want nil", got)
	}
}
