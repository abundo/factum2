package util

import "testing"

func TestHubSocketPath(t *testing.T) {
	t.Setenv("FACTUM_WORKER_API_SOCKET", "")
	if got := HubSocketPath(""); got != DefaultHubSocket {
		t.Fatalf("empty = %q, want default", got)
	}
	if got := HubSocketPath("/tmp/custom.sock"); got != "/tmp/custom.sock" {
		t.Fatalf("yaml override = %q", got)
	}
	if got := HubSocketPath("none"); got != "" {
		t.Fatalf("yaml none = %q, want empty", got)
	}
	if got := HubSocketPath("0"); got != "" {
		t.Fatalf("yaml 0 = %q, want empty", got)
	}

	t.Setenv("FACTUM_WORKER_API_SOCKET", "/tmp/env.sock")
	if got := HubSocketPath(""); got != "/tmp/env.sock" {
		t.Fatalf("env = %q", got)
	}
	if got := HubSocketPath("/tmp/yaml.sock"); got != "/tmp/yaml.sock" {
		t.Fatalf("yaml beats env: %q", got)
	}

	t.Setenv("FACTUM_WORKER_API_SOCKET", "none")
	if got := HubSocketPath(""); got != "" {
		t.Fatalf("env none = %q, want empty", got)
	}
}
