package drivers

import (
	"reflect"
	"testing"

	"github.com/abundo/netboxtool"
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

func TestSmbCanonicalIfName(t *testing.T) {
	cases := map[string]string{
		"gi1":              "GigabitEthernet1",
		"gi1/0/1":          "GigabitEthernet1/0/1",
		"GigabitEthernet1": "GigabitEthernet1",
		"te1/0/1":          "TenGigabitEthernet1/0/1",
		"po1":              "Port-channel1",
		"Port-channel1":    "Port-channel1",
		"vlan 1":           "Vlan1",
		"vlan1":            "Vlan1",
		"Vlan10":           "Vlan10",
		"fa1":              "FastEthernet1",
	}
	for in, want := range cases {
		if got := smbCanonicalIfName(in); got != want {
			t.Errorf("smbCanonicalIfName(%q) = %q, want %q", in, got, want)
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
