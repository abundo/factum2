package drivers

// vrpInterfaceVLANsCommands - pure command-list construction, no SSH dial.
// There's no fake-SSH-server tier for VRP any more than there is for SR OS -
// see srosELINECommands' doc comment in driver_nokia_sros_eline_test.go.

import (
	"reflect"
	"testing"
)

func vrpCmdStrings(cmds []sshCmd) []string {
	out := make([]string, len(cmds))
	for ix, c := range cmds {
		out[ix] = c.Cmd
	}
	return out
}

func TestVrpInterfaceVLANsCommands(t *testing.T) {
	cmds, err := vrpInterfaceVLANsCommands(
		[]string{"GigabitEthernet0/0/1", "GigabitEthernet0/0/2"},
		[]*VLANConfig{
			{SwitchportMode: "access", UntaggedVLAN: 10},
			{SwitchportMode: "trunk", UntaggedVLAN: 10, TaggedVLANs: []int{20, 30}},
		})
	if err != nil {
		t.Fatalf("vrpInterfaceVLANsCommands: %v", err)
	}

	want := []string{
		"system-view",
		"vlan batch 10 20 30",
		"interface GigabitEthernet0/0/1", "port link-type access", "port default vlan 10", "quit",
		"interface GigabitEthernet0/0/2", "port link-type trunk",
		"port trunk pvid vlan 10", "port trunk allow-pass vlan 20 30", "quit",
		"quit",
	}
	if got := vrpCmdStrings(cmds); !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

// Removing switchport config (empty SwitchportMode) must clear both the
// default vlan and the trunk allow-pass list, not just leave them as-is.
func TestVrpInterfaceVLANsCommandsRemove(t *testing.T) {
	cmds, err := vrpInterfaceVLANsCommands(
		[]string{"GigabitEthernet0/0/1"},
		[]*VLANConfig{{}})
	if err != nil {
		t.Fatalf("vrpInterfaceVLANsCommands: %v", err)
	}

	want := []string{
		"system-view",
		"interface GigabitEthernet0/0/1",
		"undo port default vlan", "undo port trunk allow-pass vlan all",
		"quit",
		"quit",
	}
	if got := vrpCmdStrings(cmds); !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestVrpInterfaceVLANsCommandsMismatch(t *testing.T) {
	_, err := vrpInterfaceVLANsCommands(
		[]string{"GigabitEthernet0/0/1", "GigabitEthernet0/0/2"},
		[]*VLANConfig{{SwitchportMode: "access", UntaggedVLAN: 10}})
	if err == nil {
		t.Fatal("want error for mismatched name/config counts, got nil")
	}
}

func TestVrpCLISessionCommands(t *testing.T) {
	got := vrpCLISessionCommands([]string{
		"interface GigabitEthernet0/0/1",
		"description ID=CN00570 Acme AB",
		"quit",
	})
	want := []string{
		"system-view",
		"interface GigabitEthernet0/0/1",
		"description ID=CN00570 Acme AB",
		"quit",
		"return",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v\nwant %v", got, want)
	}
}

func TestVrpFindCLIError(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "no error",
			output: "[HUAWEI]interface GigabitEthernet0/0/1\n[HUAWEI-GigabitEthernet0/0/1]description foo\n<HUAWEI>",
			want:   "",
		},
		{
			name:   "unrecognized command",
			output: "interface GigabitEthernet0/0/1x\nError: Unrecognized command found at '^' position.\n[HUAWEI]",
			want:   "Error: Unrecognized command found at '^' position.",
		},
		{
			name:   "vlan missing",
			output: "port default vlan 99\nError: The VLAN does not exist.\n[HUAWEI-GigabitEthernet0/0/1]",
			want:   "Error: The VLAN does not exist.",
		},
		{
			name:   "leading whitespace",
			output: "commit\n  Error: The system is busy now, please try later.\n[HUAWEI]",
			want:   "Error: The system is busy now, please try later.",
		},
		{
			name:   "not a real marker mid-word",
			output: "description Error: never matches mid-line\n[HUAWEI]",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := vrpFindCLIError(c.output)
			if got != c.want {
				t.Errorf("vrpFindCLIError() = %q, want %q", got, c.want)
			}
		})
	}
}
