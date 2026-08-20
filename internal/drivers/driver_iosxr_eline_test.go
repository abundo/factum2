package drivers

import (
	"reflect"
	"testing"
)

// ----------------------------------------------------------------------
// iosxrELINECommands - pure, network-free command construction. Like SR
// OS (driver_nokia_sros_eline_test.go), there's no fake-SSH-server tier for
// IOS-XR's classic CLI transport (DEV.md's Testing section), so this is the
// deepest coverage available short of a real device/lab.
// ----------------------------------------------------------------------

func TestIOSXRELINECommandsCrossDevice(t *testing.T) {
	intent := &ELINEIntent{
		Name:             "CN00570",
		Description:      "ID=CN00570 Acme AB",
		LocalIface:       "TenGigE0/0/0/0",
		LocalVLAN:        100,
		ServiceNumericID: 1000570,
		Remote: &ELINERemotePeer{
			NeighborIP:   "172.27.250.28",
			PseudowireID: 1000570,
			DeviceName:   "KA1-R3",
			RemoteIface:  "Ethernet15",
			RemoteVLAN:   262,
		},
	}

	got, err := iosxrELINECommands(intent)
	if err != nil {
		t.Fatalf("iosxrELINECommands: %v", err)
	}

	want := []string{
		"l2vpn", "xconnect group default", "no p2p CN00570", "root",
		"interface TenGigE0/0/0/0.100 l2transport",
		"description ID=CN00570 Acme AB",
		"mtu 9118",
		"encapsulation dot1q 100",
		"rewrite ingress tag pop 1 symmetric",
		"root",
		"l2vpn",
		"xconnect group default",
		"p2p CN00570",
		"interface TenGigE0/0/0/0.100",
		"description PEER=KA1-R3 interface=Ethernet15.262",
		"neighbor ipv4 172.27.250.28 pw-id 1000570 pw-class cw",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds =\n%v\nwant\n%v", got, want)
	}
}

func TestIOSXRELINECommandsSameDevice(t *testing.T) {
	intent := &ELINEIntent{
		Name:             "CN00600",
		Description:      "ID=CN00600 Beta AB",
		LocalIface:       "TenGigE0/0/0/1",
		LocalVLAN:        50,
		PeerLocalIface:   "TenGigE0/0/0/2",
		PeerLocalVLAN:    60,
		ServiceNumericID: 1000600,
	}

	got, err := iosxrELINECommands(intent)
	if err != nil {
		t.Fatalf("iosxrELINECommands: %v", err)
	}

	want := []string{
		"l2vpn", "xconnect group default", "no p2p CN00600", "root",
		"interface TenGigE0/0/0/1.50 l2transport",
		"description ID=CN00600 Beta AB",
		"mtu 9118",
		"encapsulation dot1q 50",
		"rewrite ingress tag pop 1 symmetric",
		"root",
		"l2vpn",
		"xconnect group default",
		"p2p CN00600",
		"interface TenGigE0/0/0/1.50",
		"interface TenGigE0/0/0/2.60",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds =\n%v\nwant\n%v", got, want)
	}
}

func TestIOSXRELINECommandsWithStaleSubinterfaces(t *testing.T) {
	intent := &ELINEIntent{
		Name:           "CN00650",
		Description:    "ID=CN00650 Acme AB",
		LocalIface:     "TenGigE0/0/0/1",
		LocalVLAN:      50,
		PeerLocalIface: "TenGigE0/0/0/2",
		PeerLocalVLAN:  60,
		StaleSubinterfaces: []ELINEStaleSubinterface{
			{Iface: "TenGigE0/0/0/1", VLAN: 40},
		},
	}

	got, err := iosxrELINECommands(intent)
	if err != nil {
		t.Fatalf("iosxrELINECommands: %v", err)
	}

	want := []string{
		"l2vpn", "xconnect group default", "no p2p CN00650", "root",
		"no interface TenGigE0/0/0/1.40",
		"interface TenGigE0/0/0/1.50 l2transport",
		"description ID=CN00650 Acme AB",
		"mtu 9118",
		"encapsulation dot1q 50",
		"rewrite ingress tag pop 1 symmetric",
		"root",
		"l2vpn",
		"xconnect group default",
		"p2p CN00650",
		"interface TenGigE0/0/0/1.50",
		"interface TenGigE0/0/0/2.60",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds =\n%v\nwant\n%v", got, want)
	}
}

// ----------------------------------------------------------------------
// cleanup define / RemoveELINE's command list
// ----------------------------------------------------------------------

func TestIOSXRELINECleanupDefine(t *testing.T) {
	got, err := renderELINETemplateDefine(iosxrELINETemplate, "cleanup", &ELINERemoval{Name: "CN00570"})
	if err != nil {
		t.Fatalf("render cleanup: %v", err)
	}
	want := []string{"l2vpn", "xconnect group default", "no p2p CN00570", "root"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v, want %v", got, want)
	}
}

func TestIOSXRELINECleanupDefineWithStale(t *testing.T) {
	got, err := renderELINETemplateDefine(iosxrELINETemplate, "cleanup", &ELINERemoval{
		Name: "CN00570",
		StaleSubinterfaces: []ELINEStaleSubinterface{
			{Iface: "TenGigE0/0/0/1", VLAN: 40},
			{Iface: "TenGigE0/0/0/1", VLAN: 41},
		},
	})
	if err != nil {
		t.Fatalf("render cleanup: %v", err)
	}
	want := []string{
		"l2vpn", "xconnect group default", "no p2p CN00570", "root",
		"no interface TenGigE0/0/0/1.40",
		"no interface TenGigE0/0/0/1.41",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v, want %v", got, want)
	}
}

// ----------------------------------------------------------------------
// iosxrFindCLIError
// ----------------------------------------------------------------------

func TestIOSXRFindCLIError(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "no error",
			output: "RP/0/RP0/CPU0:router(config)#commit\nRP/0/RP0/CPU0:router(config)#",
			want:   "",
		},
		{
			name:   "invalid input",
			output: "interface TenGigE0/0/0/0.100 l2transportx\n% Invalid input detected at '^' marker.\nRP/0/RP0/CPU0:router(config)#",
			want:   "% Invalid input detected at '^' marker.",
		},
		{
			name:   "commit failed",
			output: "commit\n% Failed to commit one or more configuration items during a pseudo-atomic operation. All changes made have been reverted.\nRP/0/RP0/CPU0:router(config)#",
			want:   "% Failed to commit one or more configuration items during a pseudo-atomic operation. All changes made have been reverted.",
		},
		{
			name:   "not a real marker mid-word",
			output: "description 100% uptime\nRP/0/RP0/CPU0:router(config)#",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := iosxrFindCLIError(c.output)
			if got != c.want {
				t.Errorf("iosxrFindCLIError() = %q, want %q", got, c.want)
			}
		})
	}
}
