package drivers

import (
	"strings"
	"testing"
)

func TestBuildIOSXRConfigTree(t *testing.T) {
	text := "show running-config\n" + strings.Join(iosxrTestConfig, "\n") + "\nend\nRP/0/RP0/CPU0:ios#\n"
	tree, err := BuildRunningConfigTree("IOS-XR", text)
	if err != nil {
		t.Fatal(err)
	}
	root := FindConfigContext(tree, "root")
	if root == nil || strings.TrimSpace(root.Body) != "" {
		t.Fatalf("root = %+v", root)
	}
	iface := FindConfigContext(tree, "if/TenGigE0/0/2/1")
	if iface == nil || !strings.Contains(iface.Body, "description CORE PEER=OK1-R2\n") || !strings.Contains(iface.Body, "ipv4 address 172.27.247.23 255.255.255.254\n") {
		t.Fatalf("interface = %+v", iface)
	}
	if strings.Join(iface.Enter, "|") != "interface TenGigE0/0/2/1" {
		t.Errorf("enter = %v", iface.Enter)
	}
	vrf := FindConfigContext(tree, "vrf/POLARIX")
	if vrf == nil || !strings.Contains(vrf.Body, "address-family ipv4 unicast\n import route-target\n  1234:700\n") {
		t.Fatalf("vrf = %+v", vrf)
	}
	af := FindConfigContext(tree, "vrf/POLARIX/af/ipv4-unicast")
	if af == nil || strings.Join(af.Enter, "|") != "vrf POLARIX|address-family ipv4 unicast" {
		t.Fatalf("af = %+v", af)
	}
	p2p := FindConfigContext(tree, "l2vpn/xconnect-group-GC/p2p-CN1927")
	if p2p == nil || !strings.Contains(p2p.Body, "neighbor ipv4 172.27.250.28 pw-id 1001927") {
		t.Fatalf("p2p = %+v", p2p)
	}
	if _, err := BuildRunningConfigTree("iosxr", text); err != nil {
		t.Fatal(err)
	}
}

func TestIOSXRCommitCmds(t *testing.T) {
	got := iosxrCommitCmds(
		[]string{"interface TenGigE0/0/2/1"},
		[]string{"description uplink"},
		"factum configuration TenGigE0/0/2/1 by Ada",
	)
	want := []string{
		"terminal length 0",
		"configure",
		"interface TenGigE0/0/2/1",
		"description uplink",
		"commit comment factum configuration TenGigE0/0/2/1 by Ada",
		"abort",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cmds =\n%s", strings.Join(got, "\n"))
	}
	if err := (&IOSXRDriver{}).CommitRunningContext(nil, nil, ""); err != nil {
		t.Fatal(err)
	}
}

const sampleSROSJSON = `{
  "nokia-conf:configure": {
    "port": [
      {
        "port-id": "1/1/1",
        "description": "uplink one",
        "admin-state": "enable",
        "ethernet": { "mode": "hybrid", "encap-type": "dot1q" }
      }
    ],
    "router": [
      {
        "router-name": "Base",
        "autonomous-system": 6782,
        "interface": [
          {
            "interface-name": "system",
            "ipv4": { "primary": { "address": "10.0.0.1", "prefix-length": 32 } }
          }
        ]
      }
    ],
    "service": {
      "epipe": [
        {
          "service-name": "CN00570",
          "description": "eline",
          "customer": "1",
          "sap": [ { "sap-id": "1/1/1:100", "admin-state": "enable" } ]
        }
      ]
    }
  },
  "nokia-conf:persistent-indices": { "description": "skip me" }
}`

func TestBuildSROSConfigTree(t *testing.T) {
	tree, err := BuildRunningConfigTree("SROS-MD", sampleSROSJSON)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(FindConfigContext(tree, "root").Body, "persistent") || strings.Contains(FindConfigContext(tree, "root").Body, "skip me") {
		t.Fatalf("root kept persistent-indices:\n%s", FindConfigContext(tree, "root").Body)
	}
	ports := FindConfigContext(tree, "port")
	if ports == nil || ports.Editable || len(ports.Children) != 1 {
		t.Fatalf("port folder = %+v", ports)
	}
	port := FindConfigContext(tree, "port/1-1-1")
	if port == nil || port.Syntax != "md-cli" {
		t.Fatalf("port = %+v", port)
	}
	if port.Body != "description \"uplink one\"\nadmin-state enable\nethernet {\n    mode hybrid\n    encap-type dot1q\n}\n" {
		t.Fatalf("port body =\n%s", port.Body)
	}
	if strings.Join(port.Enter, "|") != "/configure port 1/1/1" {
		t.Errorf("port enter = %v", port.Enter)
	}
	eth := FindConfigContext(tree, "port/1-1-1/ethernet")
	if eth == nil || eth.Body != "mode hybrid\nencap-type dot1q\n" || strings.Join(eth.Enter, "|") != "/configure port 1/1/1 ethernet" {
		t.Fatalf("ethernet = %+v", eth)
	}
	sys := FindConfigContext(tree, "router/Base/interface/system")
	if sys == nil || !strings.Contains(sys.Body, "address 10.0.0.1") || strings.Join(sys.Enter, "|") != "/configure router Base interface system" {
		t.Fatalf("system = %+v", sys)
	}
	epipe := FindConfigContext(tree, "service/epipe/CN00570")
	if epipe == nil || !strings.Contains(epipe.Body, "sap 1/1/1:100 {\n") || strings.Join(epipe.Enter, "|") != "/configure service epipe CN00570" {
		t.Fatalf("epipe = %+v body:\n%s", epipe, epipeBody(epipe))
	}
	if _, err := BuildRunningConfigTree("sros", sampleSROSJSON); err == nil {
		t.Fatal("classic sros stays unsupported")
	}
}

func epipeBody(n *ConfigContext) string {
	if n == nil {
		return ""
	}
	return n.Body
}

func TestSROSContextCommit(t *testing.T) {
	tree, err := BuildRunningConfigTree("sros-md", sampleSROSJSON)
	if err != nil {
		t.Fatal(err)
	}
	port := FindConfigContext(tree, "port/1-1-1")
	edited := strings.Replace(port.Body, "description \"uplink one\"", "description \"uplink two\"", 1)
	enter, cmds := ContextCommit(port, edited)
	if strings.Join(enter, "|") != "/configure port 1/1/1" {
		t.Fatalf("enter = %v", enter)
	}
	if strings.Join(cmds, "|") != "delete description|description \"uplink two\"" {
		t.Fatalf("cmds = %#v", cmds)
	}
	eth := FindConfigContext(tree, "port/1-1-1/ethernet")
	enter, cmds = ContextCommit(eth, "mode network\nencap-type dot1q\n")
	if strings.Join(enter, "|") != "/configure port 1/1/1 ethernet" || strings.Join(cmds, "|") != "delete mode|mode network" {
		t.Fatalf("enter=%v cmds=%#v", enter, cmds)
	}
	_, cmds = ContextCommit(port, port.Body)
	if len(cmds) != 0 {
		t.Fatalf("unchanged cmds = %#v", cmds)
	}
}

func TestSROSCommitCmds(t *testing.T) {
	got := srosCommitCmds(
		[]string{"/configure port 1/1/1"},
		[]string{`description "uplink two"`},
		"factum configuration 1/1/1 by Ada",
	)
	want := []string{
		"//environment no more",
		"edit-config exclusive",
		"/configure port 1/1/1",
		`description "uplink two"`,
		`commit comment "factum configuration 1/1/1 by Ada"`,
		"discard",
		"exit all",
		"quit-config",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cmds =\n%s", strings.Join(got, "\n"))
	}
	if err := (&NokiaDriver{}).CommitRunningContext(nil, nil, ""); err != nil {
		t.Fatal(err)
	}
}

func TestDiffMDCLIDeleteBlock(t *testing.T) {
	before := "description uplink\nethernet {\n    mode hybrid\n}\n"
	got := diffMDCLI(before, "description uplink\n")
	if strings.Join(got, "|") != "delete ethernet" {
		t.Fatalf("cmds = %#v", got)
	}
	got = diffMDCLI(before, "description uplink\nethernet {\n    mode network\n}\n")
	if strings.Join(got, "|") != "ethernet|delete mode|mode network|exit" {
		t.Fatalf("cmds = %#v", got)
	}
}
