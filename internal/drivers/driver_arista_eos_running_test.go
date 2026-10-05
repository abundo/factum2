package drivers

import (
	"encoding/json"
	"strings"
	"testing"
)

func runningTextResults(n int) []json.RawMessage {
	results := make([]json.RawMessage, n)
	for i := range results {
		results[i] = raw(`{"output":""}`)
	}
	return results
}

func TestAristaCommitRunningContext(t *testing.T) {
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		return runningTextResults(len(req.Params.Cmds)), nil
	})
	err := fake.driver(t).CommitRunningContext(
		[]string{"router bgp 6782", "address-family ipv4"},
		[]string{"network 10.0.0.0/8"},
		"factum configuration address-family ipv4 by Ada",
	)
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	reqs := append([]eapiRequest(nil), fake.requests...)
	fake.mu.Unlock()
	if len(reqs) != 3 {
		t.Fatalf("got %d requests, want 3", len(reqs))
	}
	if got := reqs[0].Params.Cmds; len(got) != 1 || !strings.HasPrefix(got[0], "no configure session factum-cfg-") {
		t.Fatalf("preclean = %#v", got)
	}
	cmds := reqs[1].Params.Cmds
	if len(cmds) != 5 {
		t.Fatalf("apply = %#v", cmds)
	}
	if !strings.HasPrefix(cmds[0], "configure session factum-cfg-") || !strings.Contains(cmds[0], `description "factum configuration address-family ipv4 by Ada"`) {
		t.Fatalf("open = %q", cmds[0])
	}
	if cmds[1] != "router bgp 6782" || cmds[2] != "address-family ipv4" || cmds[3] != "network 10.0.0.0/8" || cmds[4] != "commit" {
		t.Fatalf("apply = %#v", cmds)
	}
	if got := reqs[2].Params.Cmds; len(got) != 1 || !strings.HasPrefix(got[0], "no configure session factum-cfg-") {
		t.Fatalf("cleanup = %#v", got)
	}
}

func TestAristaCommitRunningContextAborts(t *testing.T) {
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		for _, cmd := range req.Params.Cmds {
			if cmd == "description nope" {
				return nil, &eapiError{Code: 1002, Message: "CLI command failed"}
			}
		}
		return runningTextResults(len(req.Params.Cmds)), nil
	})
	err := fake.driver(t).CommitRunningContext(
		[]string{"interface Ethernet1"},
		[]string{"description nope"},
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "CLI command failed") {
		t.Fatalf("err = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 4 {
		t.Fatalf("got %d requests, want preclean, apply, abort, release", len(fake.requests))
	}
	abort := fake.requests[2].Params.Cmds
	if len(abort) != 2 || !strings.HasPrefix(abort[0], "configure session factum-cfg-") || abort[1] != "abort" {
		t.Fatalf("abort = %#v", abort)
	}
}

func TestAristaCommitRunningContextNoCommands(t *testing.T) {
	fake := newFakeEOS(t, func(req eapiRequest) ([]json.RawMessage, *eapiError) {
		t.Errorf("unexpected eAPI call: %#v", req.Params.Cmds)
		return runningTextResults(len(req.Params.Cmds)), nil
	})
	if err := fake.driver(t).CommitRunningContext(nil, nil, ""); err != nil {
		t.Fatal(err)
	}
}
