package drivers

import (
	"encoding/json"
	"reflect"
	"testing"
)

// ----------------------------------------------------------------------
// ApplyELINE
// ----------------------------------------------------------------------

func TestAristaApplyELINECrossDevice(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	intent := &ELINEIntent{
		Name:        "CN00570",
		Description: "ID=CN00570 Acme AB",
		LocalIface:  "Ethernet1",
		LocalVLAN:   100,
		Remote: &ELINERemotePeer{
			NeighborIP:   "172.27.250.28",
			PseudowireID: 1000570,
			MTU:          9100,
			ControlWord:  true,
		},
	}

	if err := fake.driver(t).ApplyELINE(intent); err != nil {
		t.Fatalf("ApplyELINE: %v", err)
	}

	wantPrecleanup := []string{"no configure session factum-eline-CN00570"}
	wantMain := []string{
		"configure session factum-eline-CN00570",
		"mpls ldp", "pseudowires", "no pseudowire CN00570", "exit", "exit",
		"patch panel", "no patch CN00570", "exit",

		"interface Ethernet1",
		"no switchport",
		"interface Ethernet1.100",
		"description ID=CN00570 Acme AB",
		"encapsulation vlan",
		"client dot1q 100",
		"exit",
		"exit",
		"mpls ldp",
		"pseudowires",
		"pseudowire CN00570",
		"neighbor 172.27.250.28",
		"pseudowire-id 1000570",
		"mtu 9100",
		"control-word",
		"exit",
		"exit",
		"exit",
		"patch panel",
		"patch CN00570",
		"connector 1 interface Ethernet1.100",
		"connector 2 pseudowire ldp CN00570",
		"exit",
		"exit",

		"commit",
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 2 {
		t.Fatalf("got %d requests, want 2 (precleanup, main apply)", len(fake.requests))
	}
	if !reflect.DeepEqual(fake.requests[0].Params.Cmds, wantPrecleanup) {
		t.Errorf("precleanup cmds = %v\nwant %v", fake.requests[0].Params.Cmds, wantPrecleanup)
	}
	if !reflect.DeepEqual(fake.requests[1].Params.Cmds, wantMain) {
		t.Errorf("main cmds =\n%v\nwant\n%v", fake.requests[1].Params.Cmds, wantMain)
	}
	if fake.requests[1].Params.Format != eapiFormatText {
		t.Errorf("format = %q, want %q", fake.requests[1].Params.Format, eapiFormatText)
	}
}

func TestAristaApplyELINESameDevice(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	intent := &ELINEIntent{
		Name:           "CN00600",
		Description:    "ID=CN00600 Beta AB",
		LocalIface:     "Ethernet3",
		LocalVLAN:      50,
		PeerLocalIface: "Ethernet4",
		PeerLocalVLAN:  60,
	}

	if err := fake.driver(t).ApplyELINE(intent); err != nil {
		t.Fatalf("ApplyELINE: %v", err)
	}

	wantMain := []string{
		"configure session factum-eline-CN00600",
		"mpls ldp", "pseudowires", "no pseudowire CN00600", "exit", "exit",
		"patch panel", "no patch CN00600", "exit",

		"interface Ethernet3",
		"no switchport",
		"interface Ethernet3.50",
		"description ID=CN00600 Beta AB",
		"encapsulation vlan",
		"client dot1q 50",
		"exit",
		"exit",
		"interface Ethernet4",
		"no switchport",
		"interface Ethernet4.60",
		"description ID=CN00600 Beta AB",
		"encapsulation vlan",
		"client dot1q 60",
		"exit",
		"exit",
		"patch panel",
		"patch CN00600",
		"connector 1 interface Ethernet3.50",
		"connector 2 interface Ethernet4.60",
		"exit",
		"exit",

		"commit",
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 2 {
		t.Fatalf("got %d requests, want 2 (precleanup, main apply)", len(fake.requests))
	}
	if !reflect.DeepEqual(fake.requests[1].Params.Cmds, wantMain) {
		t.Errorf("main cmds =\n%v\nwant\n%v", fake.requests[1].Params.Cmds, wantMain)
	}
}

// A pseudowire with control-word disabled must omit the line entirely, not
// emit an empty one - eosParseEline treats its mere presence as the flag
// (eosReControlWord), so a blank line would be harmless CLI-wise but is
// still worth pinning down.
func TestAristaApplyELINENoControlWord(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	intent := &ELINEIntent{
		Name:       "CN00700",
		LocalIface: "Ethernet1",
		LocalVLAN:  10,
		Remote: &ELINERemotePeer{
			NeighborIP:   "10.0.0.1",
			PseudowireID: 1000700,
			MTU:          1514,
			ControlWord:  false,
		},
	}
	if err := fake.driver(t).ApplyELINE(intent); err != nil {
		t.Fatalf("ApplyELINE: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 2 {
		t.Fatalf("got %d requests, want 2 (precleanup, main apply)", len(fake.requests))
	}
	for _, c := range fake.requests[1].Params.Cmds {
		if c == "control-word" {
			t.Errorf("cmds contain \"control-word\" despite ControlWord=false: %v", fake.requests[1].Params.Cmds)
		}
	}
}

// StaleSubinterfaces must be removed inside the same cleanup block as the
// pseudowire/patch, before the new body - covers a re-provision that moves
// this service to a different interface/VLAN on the same device (the
// device-changed case is ELINERemoval/RemoveELINE below).
func TestAristaApplyELINEStaleSubinterface(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	intent := &ELINEIntent{
		Name:        "CN00570",
		Description: "ID=CN00570 Acme AB",
		LocalIface:  "Ethernet1",
		LocalVLAN:   200,
		Remote: &ELINERemotePeer{
			NeighborIP:   "172.27.250.28",
			PseudowireID: 1000570,
			MTU:          9100,
			ControlWord:  true,
		},
		StaleSubinterfaces: []ELINEStaleSubinterface{
			{Iface: "Ethernet1", VLAN: 100},
		},
	}

	if err := fake.driver(t).ApplyELINE(intent); err != nil {
		t.Fatalf("ApplyELINE: %v", err)
	}

	wantCleanup := []string{
		"configure session factum-eline-CN00570",
		"mpls ldp", "pseudowires", "no pseudowire CN00570", "exit", "exit",
		"patch panel", "no patch CN00570", "exit",
		"no interface Ethernet1.100",
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 2 {
		t.Fatalf("got %d requests, want 2 (precleanup, main apply)", len(fake.requests))
	}
	got := fake.requests[1].Params.Cmds
	if len(got) < len(wantCleanup) || !reflect.DeepEqual(got[:len(wantCleanup)], wantCleanup) {
		t.Errorf("cmds prefix =\n%v\nwant\n%v", got, wantCleanup)
	}
	if got[len(got)-1] != "commit" {
		t.Errorf("last cmd = %q, want \"commit\"", got[len(got)-1])
	}
}

// ----------------------------------------------------------------------
// RemoveELINE
// ----------------------------------------------------------------------

// RemoveELINE tears down a device's pseudowire/patch/stale subinterfaces
// without configuring anything new - used when an ELINE endpoint moves off
// a device entirely (web/handler_service_eline.go's ApiServiceElinePush).
func TestAristaRemoveELINE(t *testing.T) {
	fake := newFakeEOS(t, respondText(""))

	removal := &ELINERemoval{
		Name: "CN00570",
		StaleSubinterfaces: []ELINEStaleSubinterface{
			{Iface: "Ethernet1", VLAN: 100},
		},
	}

	if err := fake.driver(t).RemoveELINE(removal); err != nil {
		t.Fatalf("RemoveELINE: %v", err)
	}

	wantMain := []string{
		"configure session factum-eline-CN00570",
		"mpls ldp", "pseudowires", "no pseudowire CN00570", "exit", "exit",
		"patch panel", "no patch CN00570", "exit",
		"no interface Ethernet1.100",
		"commit",
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 2 {
		t.Fatalf("got %d requests, want 2 (precleanup, main removal)", len(fake.requests))
	}
	if !reflect.DeepEqual(fake.requests[1].Params.Cmds, wantMain) {
		t.Errorf("main cmds =\n%v\nwant\n%v", fake.requests[1].Params.Cmds, wantMain)
	}
}

// The pre-cleanup "no configure session" is a best-effort, separate
// request: on a service's actual first-ever apply, EOS has never heard of
// the session name and rejects deleting it ("Cannot delete non-existent
// session ..." - confirmed against a real device). That failure must not
// prevent the real apply that follows.
func TestAristaApplyELINEPrecleanupFailureIgnored(t *testing.T) {
	call := 0
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		call++
		if call == 1 {
			return nil, &eapiError{Code: 1000, Message: "Cannot delete non-existent session factum-eline-CN00900"}
		}
		return respondText("")(req)
	})

	intent := &ELINEIntent{
		Name:       "CN00900",
		LocalIface: "Ethernet1",
		LocalVLAN:  10,
		Remote: &ELINERemotePeer{
			NeighborIP:   "10.0.0.1",
			PseudowireID: 1000900,
			MTU:          1514,
			ControlWord:  true,
		},
	}
	if err := fake.driver(t).ApplyELINE(intent); err != nil {
		t.Fatalf("ApplyELINE: want nil (precleanup failure must be ignored), got %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 2 {
		t.Fatalf("got %d requests, want 2 (failed precleanup, successful main apply)", len(fake.requests))
	}
}

// If the session commit fails, ApplyELINE must abort the session and
// release its name (via "no configure session") rather than leave it open/
// reserved on the device - confirmed against a real device that re-entering
// an already-completed session by name is otherwise rejected.
func TestAristaApplyELINEAbortsOnFailure(t *testing.T) {
	call := 0
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		call++
		if call == 2 { // call 1 is the precleanup, which succeeds here
			return nil, &eapiError{Code: 1002, Message: "CLI command failed"}
		}
		return respondText("")(req)
	})

	intent := &ELINEIntent{
		Name:       "CN00800",
		LocalIface: "Ethernet1",
		LocalVLAN:  10,
		Remote: &ELINERemotePeer{
			NeighborIP:   "10.0.0.1",
			PseudowireID: 1000800,
			MTU:          1514,
			ControlWord:  true,
		},
	}
	err := fake.driver(t).ApplyELINE(intent)
	if err == nil {
		t.Fatal("ApplyELINE: want error, got nil")
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 4 {
		t.Fatalf("got %d requests, want 4 (precleanup, main apply, abort, session release)", len(fake.requests))
	}
	wantAbort := []string{"configure session factum-eline-CN00800", "abort"}
	if !reflect.DeepEqual(fake.requests[2].Params.Cmds, wantAbort) {
		t.Errorf("abort request cmds = %v, want %v", fake.requests[2].Params.Cmds, wantAbort)
	}
	wantRelease := []string{"no configure session factum-eline-CN00800"}
	if !reflect.DeepEqual(fake.requests[3].Params.Cmds, wantRelease) {
		t.Errorf("release request cmds = %v, want %v", fake.requests[3].Params.Cmds, wantRelease)
	}
}
