package drivers

import (
	"strings"
	"testing"
)

const sampleEOSConfig = `! device: lab (cEOS, EOS-4.32.0F)
!
hostname lab
banner login
Keep out
!

blank above
EOF
aaa authentication login default group radius local
radius-server host 10.0.0.1
   key 7 abc
management api http-commands
   no shutdown
   vrf MGMT
      no shutdown
!
interface Ethernet1
   description foo
   no shutdown
interface Ethernet2
   description bar
!
router bgp 6782
   router-id 1.1.1.1
   neighbor 10.0.0.2 remote-as 6782
   address-family ipv4
      neighbor 10.0.0.2 activate
      network 10.0.0.0/8
   address-family ipv6
      neighbor 2001:db8::2 activate
   vrf CUST
      rd 6782:1
      neighbor 10.1.0.1 remote-as 65001
      address-family ipv4
         neighbor 10.1.0.1 activate
!
router isis CORE
   net 49.0001.0000.0000.0001.00
   address-family ipv4 unicast
!
mpls ldp
   router-id interface Loopback0
   pseudowires
      pseudowire TEST00001
         neighbor 172.27.250.102
         pseudowire-id 1099999
         mtu 9100
         control-word
   no shutdown
!
end
`

func TestBuildEOSConfigTree(t *testing.T) {
	tree, err := BuildRunningConfigTree("EOS", sampleEOSConfig)
	if err != nil {
		t.Fatal(err)
	}
	root := FindConfigContext(tree, "root")
	if root == nil {
		t.Fatal("missing root")
	}
	if !strings.Contains(root.Body, "aaa authentication login default group radius local") {
		t.Errorf("root missing aaa:\n%s", root.Body)
	}
	if strings.Contains(root.Body, "radius-server") || strings.Contains(root.Body, "management api") || strings.Contains(root.Body, "interface ") || strings.Contains(root.Body, "router bgp") || strings.Contains(root.Body, "router isis") || strings.Contains(root.Body, "banner") || strings.Contains(root.Body, "Keep out") {
		t.Errorf("root contains a context:\n%s", root.Body)
	}
	banner := FindConfigContext(tree, "banner-login")
	if banner == nil || banner.Body != "Keep out\n!\n\nblank above\n" || strings.Join(banner.Enter, "|") != "banner login" {
		t.Fatalf("banner = %+v", banner)
	}
	radius := FindConfigContext(tree, "radius-server-host-10.0.0.1")
	if radius == nil || radius.Body != "key 7 abc\n" || strings.Join(radius.Enter, "|") != "radius-server host 10.0.0.1" {
		t.Fatalf("radius = %+v", radius)
	}
	api := FindConfigContext(tree, "management-api-http-commands")
	if api == nil || api.Body != "no shutdown\nvrf MGMT\n   no shutdown\n" {
		t.Fatalf("management api = %+v", api)
	}
	apiVRF := FindConfigContext(tree, "management-api-http-commands/vrf/MGMT")
	if apiVRF == nil || apiVRF.Body != "no shutdown\n" || strings.Join(apiVRF.Enter, "|") != "management api http-commands|vrf MGMT" {
		t.Fatalf("management vrf = %+v", apiVRF)
	}
	if len(root.Enter) != 0 {
		t.Errorf("root enter = %v", root.Enter)
	}

	eth := FindConfigContext(tree, "if/Ethernet1")
	if eth == nil || eth.Body != "description foo\nno shutdown\n" {
		t.Fatalf("Ethernet1 = %+v", eth)
	}
	if strings.Join(eth.Enter, "|") != "interface Ethernet1" {
		t.Errorf("Ethernet1 enter = %v", eth.Enter)
	}

	bgp := FindConfigContext(tree, "bgp/6782")
	if bgp == nil {
		t.Fatal("missing bgp")
	}
	if !strings.Contains(bgp.Body, "router-id 1.1.1.1\nneighbor 10.0.0.2 remote-as 6782\naddress-family ipv4\n") {
		t.Errorf("bgp body = %q", bgp.Body)
	}
	if !strings.Contains(bgp.Body, "vrf CUST\n   rd 6782:1\n") {
		t.Errorf("bgp body missing vrf:\n%s", bgp.Body)
	}
	af := FindConfigContext(tree, "bgp/6782/af/ipv4")
	if af == nil || !strings.Contains(af.Body, "network 10.0.0.0/8") {
		t.Fatalf("af ipv4 = %+v", af)
	}
	if strings.Join(af.Enter, "|") != "router bgp 6782|address-family ipv4" {
		t.Errorf("af enter = %v", af.Enter)
	}
	if FindConfigContext(tree, "bgp/6782/af/ipv6") == nil {
		t.Fatal("missing af ipv6")
	}
	vrf := FindConfigContext(tree, "bgp/6782/vrf/CUST")
	if vrf == nil || !strings.Contains(vrf.Body, "rd 6782:1\nneighbor 10.1.0.1 remote-as 65001\naddress-family ipv4\n   neighbor 10.1.0.1 activate\n") {
		t.Fatalf("vrf = %+v", vrf)
	}
	vrfAF := FindConfigContext(tree, "bgp/6782/vrf/CUST/af/ipv4")
	if vrfAF == nil || !strings.Contains(vrfAF.Body, "neighbor 10.1.0.1 activate") {
		t.Fatalf("vrf af = %+v", vrfAF)
	}
	if strings.Join(vrfAF.Enter, "|") != "router bgp 6782|vrf CUST|address-family ipv4" {
		t.Errorf("vrf af enter = %v", vrfAF.Enter)
	}
	isis := FindConfigContext(tree, "isis/CORE")
	if isis == nil || !strings.Contains(isis.Body, "net 49.0001") {
		t.Fatalf("isis = %+v", isis)
	}
	ldp := FindConfigContext(tree, "mpls-ldp")
	if ldp == nil || ldp.Body != "router-id interface Loopback0\npseudowires\n   pseudowire TEST00001\n      neighbor 172.27.250.102\n      pseudowire-id 1099999\n      mtu 9100\n      control-word\nno shutdown\n" {
		t.Fatalf("mpls ldp = %+v", ldp)
	}
	pws := FindConfigContext(tree, "mpls-ldp/pseudowires")
	if pws == nil || pws.Body != "pseudowire TEST00001\n   neighbor 172.27.250.102\n   pseudowire-id 1099999\n   mtu 9100\n   control-word\n" {
		t.Fatalf("pseudowires = %+v", pws)
	}
	pw := FindConfigContext(tree, "mpls-ldp/pseudowires/pseudowire-TEST00001")
	if pw == nil || pw.Body != "neighbor 172.27.250.102\npseudowire-id 1099999\nmtu 9100\ncontrol-word\n" {
		t.Fatalf("pseudowire = %+v", pw)
	}

	if _, err := BuildRunningConfigTree("sros", sampleEOSConfig); err == nil {
		t.Fatal("sros should be unsupported")
	}
}

func TestContextCommitBanner(t *testing.T) {
	node := &ConfigContext{Enter: []string{"banner login"}, Body: "Keep out\n"}
	enter, cmds := ContextCommit(node, "Keep out\nline 2\n")
	if strings.Join(enter, "|") != "banner login" || strings.Join(cmds, "|") != "Keep out|line 2|EOF" {
		t.Fatalf("rewrite enter=%v cmds=%v", enter, cmds)
	}
	enter, cmds = ContextCommit(node, "")
	if len(enter) != 0 || strings.Join(cmds, "|") != "no banner login" {
		t.Fatalf("delete enter=%v cmds=%v", enter, cmds)
	}
	_, cmds = ContextCommit(node, "Keep out\n")
	if len(cmds) != 0 {
		t.Fatalf("unchanged cmds=%v", cmds)
	}
}

func TestDiffConfigCommands(t *testing.T) {
	cases := []struct {
		name   string
		before string
		after  string
		want   []string
	}{
		{
			name:   "replace description",
			before: "description foo\nip address 10.0.0.1/24\n",
			after:  "description bar\nip address 10.0.0.1/24\n",
			want:   []string{"no description foo", "description bar"},
		},
		{
			name:   "remove no shutdown",
			before: "description foo\nno shutdown\n",
			after:  "description foo\n",
			want:   []string{"shutdown"},
		},
		{
			name:   "reorder replaces the line that moved",
			before: "description a\nip address 10.0.0.1/24\n",
			after:  "ip address 10.0.0.1/24\ndescription a\n",
			want:   []string{"no description a", "description a"},
		},
		{
			name:   "nested key",
			before: "radius-server host 10.0.0.1\n   key 7 abc\n",
			after:  "radius-server host 10.0.0.1\n   key 7 def\n",
			want:   []string{"radius-server host 10.0.0.1", "no key 7 abc", "key 7 def", "exit"},
		},
		{
			name:   "delete block",
			before: "radius-server host 10.0.0.1\n   key 7 abc\n",
			after:  "",
			want:   []string{"no radius-server host 10.0.0.1"},
		},
		{
			name:   "add address family under vrf",
			before: "rd 6782:1\n",
			after:  "rd 6782:1\naddress-family ipv4\n   network 10.0.0.0/8\n",
			want:   []string{"address-family ipv4", "network 10.0.0.0/8", "exit"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiffConfigCommands(tc.before, tc.after)
			if len(got) != len(tc.want) {
				t.Fatalf("commands = %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("commands = %#v, want %#v", got, tc.want)
				}
			}
		})
	}
}

func TestUnifiedDiff(t *testing.T) {
	if got := UnifiedDiff("hostname lab\n", "hostname lab\n"); got != "" {
		t.Fatalf("identical diff = %q", got)
	}
	got := UnifiedDiff("hostname old\n", "hostname new\n")
	if !strings.Contains(got, "- hostname old") || !strings.Contains(got, "+ hostname new") {
		t.Fatalf("diff = %q", got)
	}
}
