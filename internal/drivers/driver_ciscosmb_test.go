package drivers

import (
	"reflect"
	"testing"

	"github.com/abundo/factum2/internal/netboxtool"
)

func smbCmdStrings(cmds []sshCmd) []string {
	out := make([]string, len(cmds))
	for ix, c := range cmds {
		out[ix] = c.Cmd
	}
	return out
}

func TestCiscoSMBRegister(t *testing.T) {
	if _, ok := SupportedPlatforms["ciscosmb"]; !ok {
		t.Fatal("ciscosmb not in SupportedPlatforms")
	}
}

func TestExpandInterfaceNameCiscoSMB(t *testing.T) {
	cases := map[string]string{
		"gi9":                     "gigabitethernet9",
		"Gi9":                     "gigabitethernet9",
		"gi1/0/10":                "gigabitethernet1/0/10",
		"te1/0/1":                 "tengigabitethernet1/0/1",
		"fa1":                     "fastethernet1",
		"po1":                     "port-channel1",
		"Po1":                     "port-channel1",
		"vl777":                   "vlan777",
		"vlan 777":                "vlan777",
		"vlan777":                 "vlan777",
		"gigabitethernet9":        "gigabitethernet9",
		"GigabitEthernet9":        "GigabitEthernet9",
		"tengigabitethernet1/0/1": "tengigabitethernet1/0/1",
		"ETH1":                    "ETH1",
	}
	for in, want := range cases {
		if got := ExpandInterfaceName("ciscosmb", in); got != want {
			t.Errorf("ExpandInterfaceName(ciscosmb, %q) = %q, want %q", in, got, want)
		}
		if got := ExpandInterfaceName("CiscoSMB", in); got != want {
			t.Errorf("ExpandInterfaceName(CiscoSMB, %q) = %q, want %q", in, got, want)
		}
	}
	if got := ExpandInterfaceName("eos", "gi9"); got != "gi9" {
		t.Errorf("eos gi9 = %q, want unchanged", got)
	}
}

func TestSmbIfMatchKey(t *testing.T) {
	cases := map[string]string{
		"gi1":              "gigabitethernet1",
		"gi1/0/1":          "gigabitethernet1/0/1",
		"gigabitethernet1": "gigabitethernet1",
		"GigabitEthernet1": "gigabitethernet1",
		"te1/0/1":          "tengigabitethernet1/0/1",
		"po1":              "port-channel1",
		"Port-channel1":    "port-channel1",
		"vlan 1":           "vlan1",
		"vlan1":            "vlan1",
		"Vlan10":           "vlan10",
		"fa1":              "fastethernet1",
	}
	for in, want := range cases {
		if got := smbIfMatchKey(in); got != want {
			t.Errorf("smbIfMatchKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSmbResolveIfName(t *testing.T) {
	literals := map[string]string{
		smbIfMatchKey("gigabitethernet1"): "gigabitethernet1",
		smbIfMatchKey("port-channel1"):    "port-channel1",
	}
	cases := map[string]string{
		"gi1":              "gigabitethernet1",
		"GigabitEthernet1": "gigabitethernet1",
		"Po1":              "port-channel1",
		"gi9":              "gi9",
		"vlan 1":           "vlan1",
		"vlan 777":         "vlan777",
		"Vlan10":           "vlan10",
	}
	for in, want := range cases {
		if got := smbResolveIfName(in, literals); got != want {
			t.Errorf("smbResolveIfName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSmbInterfaceVLANsCommands(t *testing.T) {
	cmds, err := smbInterfaceVLANsCommands(
		[]string{"GigabitEthernet1", "GigabitEthernet2"},
		[]*VLANConfig{
			{SwitchportMode: "access", UntaggedVLAN: 10},
			{SwitchportMode: "trunk", UntaggedVLAN: 10, TaggedVLANs: []int{20, 30}},
		})
	if err != nil {
		t.Fatalf("smbInterfaceVLANsCommands: %v", err)
	}

	want := []string{
		"configure",
		"vlan database", "vlan 10,20,30", "exit",
		"interface GigabitEthernet1", "switchport mode access", "switchport access vlan 10", "exit",
		"interface GigabitEthernet2", "switchport mode trunk",
		"switchport trunk native vlan 10", "switchport trunk allowed vlan 20,30", "exit",
		"end",
	}
	if got := smbCmdStrings(cmds); !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestSmbInterfaceVLANsCommandsQinQ(t *testing.T) {
	cmds, err := smbInterfaceVLANsCommands(
		[]string{"GigabitEthernet1"},
		[]*VLANConfig{{SwitchportMode: "dot1q-tunnel", UntaggedVLAN: 100}})
	if err != nil {
		t.Fatalf("smbInterfaceVLANsCommands: %v", err)
	}
	want := []string{
		"configure",
		"vlan database", "vlan 100", "exit",
		"interface GigabitEthernet1", "switchport mode customer", "switchport customer vlan 100", "exit",
		"end",
	}
	if got := smbCmdStrings(cmds); !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestSmbInterfaceVLANsCommandsRemove(t *testing.T) {
	cmds, err := smbInterfaceVLANsCommands(
		[]string{"GigabitEthernet1"},
		[]*VLANConfig{{}})
	if err != nil {
		t.Fatalf("smbInterfaceVLANsCommands: %v", err)
	}
	want := []string{
		"configure",
		"interface GigabitEthernet1",
		"switchport mode access",
		"no switchport access vlan",
		"no switchport trunk native vlan",
		"no switchport trunk allowed vlan",
		"exit",
		"end",
	}
	if got := smbCmdStrings(cmds); !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestSmbInterfaceVLANsCommandsMismatch(t *testing.T) {
	_, err := smbInterfaceVLANsCommands(
		[]string{"GigabitEthernet1", "GigabitEthernet2"},
		[]*VLANConfig{{SwitchportMode: "access", UntaggedVLAN: 10}})
	if err == nil {
		t.Fatal("want error for mismatched name/config counts, got nil")
	}
}

func TestSmbInterfaceDescriptionsCommands(t *testing.T) {
	cmds, err := smbInterfaceDescriptionsCommands(
		[]string{"GigabitEthernet1", "GigabitEthernet2"},
		[]*netboxtool.NBInterface{
			{Description: "ID=CN00570 Acme AB"},
			{Description: ""},
		})
	if err != nil {
		t.Fatalf("smbInterfaceDescriptionsCommands: %v", err)
	}
	want := []string{
		"configure",
		"interface GigabitEthernet1", "description ID=CN00570 Acme AB", "exit",
		"interface GigabitEthernet2", "no description", "exit",
		"end",
	}
	if got := smbCmdStrings(cmds); !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestSmbCLIInterfaceNameInsertsVlanSpace(t *testing.T) {
	cmds, err := smbInterfaceDescriptionsCommands(
		[]string{"vlan777", "gigabitethernet1"},
		[]*netboxtool.NBInterface{{Description: "svi"}, {Description: "uplink"}},
	)
	if err != nil {
		t.Fatalf("smbInterfaceDescriptionsCommands: %v", err)
	}
	got := smbCmdStrings(cmds)
	want := []string{
		"configure",
		"interface vlan 777", "description svi", "exit",
		"interface gigabitethernet1", "description uplink", "exit",
		"end",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestSmbCLISessionCommands(t *testing.T) {
	got := smbCLISessionCommands([]string{
		"interface GigabitEthernet1",
		"description ID=CN00570 Acme AB",
		"exit",
	})
	want := []string{
		"configure",
		"interface GigabitEthernet1",
		"description ID=CN00570 Acme AB",
		"exit",
		"end",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestSmbFindCLIError(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "no error",
			output: "switch#configure\nswitch(config)#interface gi1\nswitch(config-if)#",
			want:   "",
		},
		{
			name:   "unrecognized command",
			output: "terminal len 0\n% Unrecognized command\nswitch#",
			want:   "% Unrecognized command",
		},
		{
			name:   "incomplete",
			output: "switchport access vlan\n% Incomplete command\nswitch(config-if)#",
			want:   "% Incomplete command",
		},
		{
			name:   "not a real marker mid-word",
			output: "description % never matches mid-line\nswitch#",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := smbFindCLIError(c.output)
			if got != c.want {
				t.Errorf("smbFindCLIError() = %q, want %q", got, c.want)
			}
		})
	}
}
