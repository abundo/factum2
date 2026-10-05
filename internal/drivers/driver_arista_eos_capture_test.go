package drivers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCapturePlatformsIncludeEOS(t *testing.T) {
	if !CaptureSupported("eos") || !CaptureSupported("EOS") {
		t.Fatal("eos should support packet capture")
	}
	if CaptureSupported("sros") || CaptureSupported("vrp") {
		t.Fatal("only eos implements port mirroring so far")
	}
	found := false
	for _, p := range CapturePlatforms() {
		if p == "eos" {
			found = true
		}
	}
	if !found {
		t.Fatalf("CapturePlatforms() = %v, want eos", CapturePlatforms())
	}
}

func TestNormalizeCaptureRequestRejectsInjection(t *testing.T) {
	cases := []PortMirrorRequest{
		{Interface: "Ethernet1;reload"},
		{Interface: "Ethernet1", Filter: "host 192.0.2.1; reboot"},
		{Interface: "Ethernet1", Filter: "host 192.0.2.1 && id"},
		{Interface: "", Filter: ""},
		{Interface: "Ethernet1", Filter: "host `id`"},
	}
	for _, req := range cases {
		if _, err := NormalizeCaptureRequest(req); err == nil {
			t.Errorf("NormalizeCaptureRequest(%+v) accepted an unsafe request", req)
		}
	}
	ok, err := NormalizeCaptureRequest(PortMirrorRequest{
		Interface: "Ethernet1/1",
		Filter:    "host 192.0.2.1 and (tcp port 80 or udp port 53)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok.MaxPackets != captureDefaultPackets || ok.MaxSeconds != captureDefaultSeconds {
		t.Fatalf("defaults = %d packets / %d seconds", ok.MaxPackets, ok.MaxSeconds)
	}
	clamped, err := NormalizeCaptureRequest(PortMirrorRequest{
		Interface:  "Port-Channel5",
		MaxPackets: 9_000_000,
		MaxSeconds: 99_000,
		Snaplen:    -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if clamped.MaxPackets != captureMaxPackets || clamped.MaxSeconds != captureMaxSeconds || clamped.Snaplen != 0 {
		t.Fatalf("clamp = %+v", clamped)
	}
}

func TestEOSCaptureRejectsSubinterface(t *testing.T) {
	d := &AristaDriver{}
	_, err := d.SetupPortMirror(context.Background(), PortMirrorRequest{Interface: "Ethernet2.210"})
	if err == nil || !strings.Contains(err.Error(), "subinterface") {
		t.Fatalf("SetupPortMirror(Ethernet2.210) = %v", err)
	}
	for _, name := range []string{"Ethernet1", "Ethernet1/1", "Management1", "Port-Channel5", "Vlan100"} {
		if _, _, ok := splitSubinterface(name); ok {
			t.Fatalf("%s should be offered for capture", name)
		}
	}
}

func TestEOSMirrorCommands(t *testing.T) {
	cmds := eosMirrorApplyCommands("Ethernet3")
	want := []string{
		"configure",
		"monitor session Ethernet3 source Ethernet3",
		"monitor session Ethernet3 destination cpu",
		"monitor session Ethernet3 rate-limit per-ingress-chip 1 mbps",
		"end",
	}
	if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
		t.Fatalf("setup = %v", cmds)
	}
	for _, line := range cmds {
		if strings.Contains(line, " both") || strings.Contains(line, "factum2") || strings.Contains(line, ";") {
			t.Fatalf("setup mirrors more than the one interface: %v", cmds)
		}
	}
	down := eosMirrorDeleteCommands("Ethernet3")
	if strings.Join(down, "\n") != "configure\nno monitor session Ethernet3\nend" {
		t.Fatalf("teardown = %v", down)
	}
}

func TestEOSParseMirrorJSON(t *testing.T) {
	raw := json.RawMessage(`{"sessions":{"Ethernet1":{"mirrorDeviceName":"mirror0","targetInterfaces":[{"name":"Cpu","operState":"active"}]}}}`)
	got, err := eosParseMirrorJSON("Ethernet1", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "mirror0" {
		t.Fatalf("iface = %q", got)
	}
	if _, err := eosParseMirrorJSON("Ethernet1", json.RawMessage(`{"sessions":{"Ethernet1":{"mirrorDeviceName":""}}}`)); err == nil {
		t.Fatal("expected an error when the mirror interface is missing")
	}
}

func TestEOSParseMirrorInterface(t *testing.T) {
	show := `
Session factum2
------------------------
Source Ports:
   Both: Et3/1
Destination Ports:
   Cpu : active (mirror0)
`
	got, err := eosParseMirrorInterface("Ethernet3", show)
	if err != nil {
		t.Fatal(err)
	}
	if got != "mirror0" {
		t.Fatalf("iface = %q", got)
	}
	if _, err := eosParseMirrorInterface("Ethernet3", "Session Ethernet3\nCpu : inactive\n"); err == nil {
		t.Fatal("expected an error when the mirror interface is missing")
	}
}

func textResults(req eapiRequest, last string) []json.RawMessage {
	results := make([]json.RawMessage, len(req.Params.Cmds))
	for i := range results {
		results[i] = raw(`{"output":""}`)
	}
	body, _ := json.Marshal(eapiTextResult{Output: last})
	results[len(results)-1] = body
	return results
}

func eapiCLIError(line string) *eapiError {
	return &eapiError{
		Code:    1002,
		Message: "CLI command failed",
		Data: []struct {
			Errors []string `json:"errors"`
		}{{Errors: []string{line}}},
	}
}

func eapiCmds(t *testing.T, fake *fakeEOS) []eapiRequest {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]eapiRequest(nil), fake.requests...)
}

// TestEOSMirrorSetupUsesEAPI checks that the monitor session is created
// and removed with eAPI. tcpdump still dials SSH, which fails against the
// fake eAPI server, and that failure must delete the session again.
func TestEOSMirrorSetupUsesEAPI(t *testing.T) {
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		joined := strings.Join(req.Params.Cmds, "\n")
		if strings.Contains(joined, "no monitor session Ethernet1") {
			return nil, eapiCLIError("% Monitor session Ethernet1 does not exist")
		}
		if strings.Contains(joined, "rate-limit") {
			return textResults(req, ""), nil
		}
		if joined == "show monitor session Ethernet1" {
			return []json.RawMessage{raw(`{"sessions":{"Ethernet1":{"mirrorDeviceName":"mirror2"}}}`)}, nil
		}
		t.Errorf("unexpected commands %v", req.Params.Cmds)
		return nil, eapiCLIError("% unexpected")
	})
	_, err := fake.driver(t).SetupPortMirror(context.Background(), PortMirrorRequest{Interface: "Ethernet1"})
	if err == nil {
		t.Fatal("expected tcpdump's SSH dial to fail against the eAPI test server")
	}
	if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "eAPI") {
		t.Fatalf("setup error = %v, want the SSH dial failure", err)
	}
	got := eapiCmds(t, fake)
	if len(got) != 4 {
		t.Fatalf("eAPI calls = %d, want delete, apply, show, delete", len(got))
	}
	if got[0].Params.Format != eapiFormatText || strings.Join(got[0].Params.Cmds, "\n") != strings.Join(eosMirrorDeleteCommands("Ethernet1"), "\n") {
		t.Fatalf("first call = %+v", got[0].Params)
	}
	if got[1].Params.Format != eapiFormatText || strings.Join(got[1].Params.Cmds, "\n") != strings.Join(eosMirrorApplyCommands("Ethernet1"), "\n") {
		t.Fatalf("apply call = %+v", got[1].Params)
	}
	if got[2].Params.Format != eapiFormatJSON || strings.Join(got[2].Params.Cmds, "\n") != "show monitor session Ethernet1" {
		t.Fatalf("show call = %+v", got[2].Params)
	}
	if strings.Join(got[3].Params.Cmds, "\n") != strings.Join(eosMirrorDeleteCommands("Ethernet1"), "\n") {
		t.Fatalf("cleanup call = %+v", got[3].Params)
	}
}

func TestEOSMirrorShowWaitsUntilCPUIsActive(t *testing.T) {
	shows := 0
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		joined := strings.Join(req.Params.Cmds, "\n")
		if strings.Contains(joined, "no monitor session") {
			return textResults(req, ""), nil
		}
		if strings.Contains(joined, "rate-limit") {
			return textResults(req, ""), nil
		}
		if joined == "show monitor session Ethernet1" {
			shows++
			if shows == 1 {
				return []json.RawMessage{raw(`{"sessions":{"Ethernet1":{"mirrorDeviceName":"","targetInterfaces":[{"name":"Cpu","operState":"unknown"}]}}}`)}, nil
			}
			return []json.RawMessage{raw(`{"sessions":{"Ethernet1":{"mirrorDeviceName":"mirror2"}}}`)}, nil
		}
		t.Errorf("unexpected commands %v", req.Params.Cmds)
		return nil, eapiCLIError("% unexpected")
	})
	_, err := fake.driver(t).SetupPortMirror(context.Background(), PortMirrorRequest{Interface: "Ethernet1"})
	if err == nil || strings.Contains(err.Error(), "no cpu mirror interface") {
		t.Fatalf("setup error = %v, want the SSH dial failure after the mirror appears", err)
	}
	got := eapiCmds(t, fake)
	showsSeen := 0
	for _, req := range got {
		if strings.Join(req.Params.Cmds, "\n") == "show monitor session Ethernet1" {
			showsSeen++
		}
	}
	if showsSeen != 2 {
		t.Fatalf("show calls = %d, want 2 (unknown, then mirror2)", showsSeen)
	}
}

func TestEOSMirrorRateLimitFailureRemovesSession(t *testing.T) {
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		joined := strings.Join(req.Params.Cmds, "\n")
		if strings.Contains(joined, "no monitor session") {
			return textResults(req, ""), nil
		}
		if strings.Contains(joined, "rate-limit") {
			return nil, eapiCLIError("% Invalid input")
		}
		t.Errorf("unexpected commands %v", req.Params.Cmds)
		return nil, eapiCLIError("% unexpected")
	})
	_, err := fake.driver(t).SetupPortMirror(context.Background(), PortMirrorRequest{Interface: "Ethernet1"})
	if err == nil || !strings.Contains(err.Error(), "Invalid input") {
		t.Fatalf("setup error = %v", err)
	}
	got := eapiCmds(t, fake)
	if len(got) != 3 {
		t.Fatalf("eAPI calls = %d, want delete, failed apply, delete", len(got))
	}
	if strings.Join(got[1].Params.Cmds, "\n") != strings.Join(eosMirrorApplyCommands("Ethernet1"), "\n") {
		t.Fatalf("apply = %v", got[1].Params.Cmds)
	}
	if strings.Join(got[2].Params.Cmds, "\n") != strings.Join(eosMirrorDeleteCommands("Ethernet1"), "\n") {
		t.Fatalf("cleanup = %v", got[2].Params.Cmds)
	}
}

func TestEOSTCPDumpCommand(t *testing.T) {
	cmd, err := eosTCPDumpCommand("mirror0", PortMirrorRequest{
		Interface:  "Ethernet1",
		Filter:     "host 192.0.2.1 and port 53",
		Snaplen:    0,
		MaxPackets: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "bash sudo -n tcpdump -n -U -w - -i mirror0 -s 0 -c 100 -- host 192.0.2.1 and port 53"
	if cmd != want {
		t.Fatalf("cmd = %q", cmd)
	}
	if _, err := eosTCPDumpCommand("mirror0;id", PortMirrorRequest{}); err == nil {
		t.Fatal("expected a rejected linux interface")
	}
	if _, err := eosTCPDumpCommand("et1", PortMirrorRequest{}); err == nil {
		t.Fatal("expected a rejected linux interface")
	}
	if _, err := eosTCPDumpCommand("mirror1", PortMirrorRequest{Filter: "port 80; id"}); err == nil {
		t.Fatal("expected a rejected filter")
	}
}
