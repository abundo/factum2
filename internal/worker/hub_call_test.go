package worker

import (
	"context"
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
