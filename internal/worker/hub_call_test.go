package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCallRoleNoNode(t *testing.T) {
	t.Parallel()
	m := NewRemoteManager(nil)
	_, err := m.CallRole(context.Background(), StorageRole, "GET", "/health", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no connected worker") {
		t.Fatalf("got %v", err)
	}
}

func TestCallRoleNilManager(t *testing.T) {
	t.Parallel()
	var m *RemoteManager
	_, err := m.CallRole(context.Background(), StorageRole, "GET", "/health", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleCallDhcpLeasesNoKea(t *testing.T) {
	w := New(nil)
	outbox := make(chan Envelope, 1)
	w.handleCall(CallMsg{ID: "1", Method: http.MethodGet, Path: "/dhcp/leases"}, outbox)
	env := <-outbox
	if env.Type != EnvelopeCallResult {
		t.Fatalf("type %s", env.Type)
	}
	var res CallResultMsg
	if err := json.Unmarshal(env.Payload, &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusBadGateway {
		t.Fatalf("status %d body=%s err=%s", res.Status, res.Body, res.Error)
	}
	if !strings.Contains(res.Error, "Kea control sockets") {
		t.Fatalf("error %q", res.Error)
	}
}

func TestHandleCallDhcpLeasesMethodNotAllowed(t *testing.T) {
	w := New(nil)
	outbox := make(chan Envelope, 1)
	w.handleCall(CallMsg{ID: "1", Method: http.MethodPost, Path: "/dhcp/leases"}, outbox)
	env := <-outbox
	var res CallResultMsg
	if err := json.Unmarshal(env.Payload, &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", res.Status)
	}
}
