package drivers

import (
	"reflect"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------
// srosELINECommands - pure, network-free command construction. There's no
// fake-SSH-server tier for SR OS the way driver_arista_eos_eline_test.go
// has for EOS (DEV.md's Testing section: no containerlab image), so this is
// the deepest coverage available short of a real device/lab - see
// driver_nokia_sros_eline.go's package comment.
// ----------------------------------------------------------------------

// want matches driver_nokia_sros_eline.go's package comment's real "info
// inheritance" example (service-id/sdp-id/PEER= shape), just with this
// test's own Name/VLAN/interfaces substituted in.
func TestSROSELINECommandsCrossDevice(t *testing.T) {
	intent := &ELINEIntent{
		Name:             "CN00570",
		Description:      "ID=CN00570 Acme AB",
		LocalIface:       "1/1/1",
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

	got, err := srosELINECommands(intent)
	if err != nil {
		t.Fatalf("srosELINECommands: %v", err)
	}

	want := []string{
		`delete /configure service epipe "CN00570"`,
		`/configure service epipe "CN00570" {`,
		`apply-groups ["eline-defaults"]`,
		`description "ID=CN00570 Acme AB"`,
		`service-id 1000570`,
		`sap 1/1/1:100.* {`,
		`description "ID=CN00570 Acme AB"`,
		`}`,
		`spoke-sdp 28:1000570 {`,
		`description "PEER=KA1-R3 interface=Ethernet15.262"`,
		`}`,
		`}`,
		`/configure service sdp 28 {`,
		`apply-groups ["sdp-default"]`,
		`description "PEER=KA1-R3"`,
		`far-end {`,
		`ip-address 172.27.250.28`,
		`}`,
		`}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds =\n%v\nwant\n%v", got, want)
	}
}

func TestSROSELINECommandsSameDevice(t *testing.T) {
	intent := &ELINEIntent{
		Name:             "CN00600",
		Description:      "ID=CN00600 Beta AB",
		LocalIface:       "1/1/3",
		LocalVLAN:        50,
		PeerLocalIface:   "1/1/4",
		PeerLocalVLAN:    60,
		ServiceNumericID: 1000600,
	}

	got, err := srosELINECommands(intent)
	if err != nil {
		t.Fatalf("srosELINECommands: %v", err)
	}

	want := []string{
		`delete /configure service epipe "CN00600"`,
		`/configure service epipe "CN00600" {`,
		`apply-groups ["eline-defaults"]`,
		`description "ID=CN00600 Beta AB"`,
		`service-id 1000600`,
		`sap 1/1/3:50.* {`,
		`description "ID=CN00600 Beta AB"`,
		`}`,
		`sap 1/1/4:60.* {`,
		`description "ID=CN00600 Beta AB"`,
		`}`,
		`}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds =\n%v\nwant\n%v", got, want)
	}
}

// MTU/ControlWord are EOS-only fields (ELINERemotePeer's doc comment) -
// SR OS's spoke-sdp/sdp inherit both from their apply-groups instead, so
// setting them must not change the rendered commands at all.
func TestSROSELINECommandsIgnoresMTUAndControlWord(t *testing.T) {
	base := func(mtu int, cw bool) *ELINEIntent {
		return &ELINEIntent{
			Name:             "CN00700",
			Description:      "ID=CN00700 Acme AB",
			LocalIface:       "1/1/1",
			LocalVLAN:        10,
			ServiceNumericID: 1000700,
			Remote: &ELINERemotePeer{
				NeighborIP:   "10.0.0.1",
				PseudowireID: 1000700,
				DeviceName:   "peer-r1",
				RemoteIface:  "1/1/2",
				RemoteVLAN:   10,
				MTU:          mtu,
				ControlWord:  cw,
			},
		}
	}

	a, err := srosELINECommands(base(9100, true))
	if err != nil {
		t.Fatalf("srosELINECommands: %v", err)
	}
	b, err := srosELINECommands(base(1514, false))
	if err != nil {
		t.Fatalf("srosELINECommands: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("cmds differ despite only MTU/ControlWord changing:\na=%v\nb=%v", a, b)
	}
	for _, c := range a {
		if strings.Contains(c, "control-word") || strings.Contains(c, "mtu") {
			t.Errorf("cmds should never mention control-word/mtu (inherited from apply-group), got: %v", a)
		}
	}
}

func TestSROSELINECommandsInvalidNeighbor(t *testing.T) {
	intent := &ELINEIntent{
		Name:             "CN00800",
		LocalIface:       "1/1/1",
		LocalVLAN:        10,
		ServiceNumericID: 1000800,
		Remote: &ELINERemotePeer{
			NeighborIP:   "not-an-ip",
			PseudowireID: 1000800,
		},
	}
	if _, err := srosELINECommands(intent); err == nil {
		t.Fatal("srosELINECommands: want error for invalid neighbor address, got nil")
	}
}

// ----------------------------------------------------------------------
// cleanup define / RemoveELINE's command list
// ----------------------------------------------------------------------

func TestSROSELINECleanupDefine(t *testing.T) {
	got, err := renderELINETemplateDefine(srosELINETemplate, "cleanup", &ELINERemoval{Name: "CN00570"})
	if err != nil {
		t.Fatalf("render cleanup: %v", err)
	}
	want := []string{`delete /configure service epipe "CN00570"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmds = %v, want %v", got, want)
	}
}

// ----------------------------------------------------------------------
// srosSDPID
// ----------------------------------------------------------------------

func TestSROSSDPID(t *testing.T) {
	cases := []struct {
		neighbor string
		want     int
		wantErr  bool
	}{
		{neighbor: "172.27.250.28", want: 28},
		{neighbor: "10.0.0.1", want: 1},
		{neighbor: "10.0.0.0", wantErr: true},   // 0: not a usable ID
		{neighbor: "10.0.0.255", wantErr: true}, // 255: not a usable ID
		{neighbor: "not-an-ip", wantErr: true},
		{neighbor: "2001:db8::1", wantErr: true}, // IPv6 not supported
	}
	for _, c := range cases {
		got, err := srosSDPID(c.neighbor)
		if c.wantErr {
			if err == nil {
				t.Errorf("srosSDPID(%q): want error, got %d", c.neighbor, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("srosSDPID(%q): %v", c.neighbor, err)
			continue
		}
		if got != c.want {
			t.Errorf("srosSDPID(%q) = %d, want %d", c.neighbor, got, c.want)
		}
	}
}

// ----------------------------------------------------------------------
// srosFindCLIError
// ----------------------------------------------------------------------

func TestSROSFindCLIError(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "no error",
			output: "configure service epipe \"CN00570\"\n[gl:configure service epipe \"CN00570\"]\nA:admin@sros#",
			want:   "",
		},
		{
			name:   "minor error",
			output: "ok\nsap 1/1/1:100.* description \"x\"\nMINOR: CLI Invalid sap syntax\nA:admin@sros#",
			want:   "MINOR: CLI Invalid sap syntax",
		},
		{
			name:   "error prefix",
			output: "commit\nERROR: CLI Commit failed: validation error\nA:admin@sros#",
			want:   "ERROR: CLI Commit failed: validation error",
		},
		{
			name:   "not a real marker mid-word",
			output: "description \"MINORITY REPORT\"\nA:admin@sros#",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := srosFindCLIError(c.output)
			if got != c.want {
				t.Errorf("srosFindCLIError() = %q, want %q", got, c.want)
			}
		})
	}
}

// ----------------------------------------------------------------------
// srosSDPFarEnd - the pure matching logic behind srosCheckSDPConflict's
// collision guard. See driver_nokia_sros_eline.go's package comment: an
// early manual test against a real device repointed a real, shared sdp's
// far-end because nothing checked for this before ApplyELINE existed.
// ----------------------------------------------------------------------

func TestSROSSDPFarEnd(t *testing.T) {
	configure := map[string]any{
		"service": map[string]any{
			"sdp": []any{
				map[string]any{"sdp-id": float64(28), "far-end": map[string]any{"ip-address": "172.27.249.28"}},
				map[string]any{"sdp-id": float64(127), "far-end": map[string]any{"ip-address": "172.27.249.127"}},
			},
		},
	}

	if got := srosSDPFarEnd(configure, 28); got != "172.27.249.28" {
		t.Errorf("srosSDPFarEnd(28) = %q, want 172.27.249.28", got)
	}
	if got := srosSDPFarEnd(configure, 127); got != "172.27.249.127" {
		t.Errorf("srosSDPFarEnd(127) = %q, want 172.27.249.127", got)
	}
	if got := srosSDPFarEnd(configure, 999); got != "" {
		t.Errorf("srosSDPFarEnd(999) = %q, want \"\" (no such sdp)", got)
	}
	if got := srosSDPFarEnd(map[string]any{}, 28); got != "" {
		t.Errorf("srosSDPFarEnd on empty configure = %q, want \"\"", got)
	}
}
