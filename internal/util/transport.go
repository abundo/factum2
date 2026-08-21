package util

import (
	"os"
	"time"
)

// DefaultHubSocket is the unix HTTP path factum-worker listens on and
// co-located CLIs probe. One constant so the two sides cannot drift.
const DefaultHubSocket = "/run/factum-worker/api.sock"

// HubRPCTimeout is the per-request bound for hub RPC (ServeHTTP after the
// websocket dies, CLI HTTP clients). Canonical here so util does not import
// worker.
const HubRPCTimeout = 60 * time.Second

// HubSocketPath resolves the unix socket.
// yamlOverride is ConfigFactum.Socket (CLI) or ConfigWorker.APISocket (listener).
// FACTUM_WORKER_API_SOCKET is the single relocation / disable knob both sides
// honor when the yaml override is empty.
// Values "none" and "0" (yaml or env) mean "no socket" (CLI: force HTTPS;
// worker: Start error).
func HubSocketPath(yamlOverride string) string {
	if yamlOverride == "none" || yamlOverride == "0" {
		return ""
	}
	if yamlOverride != "" {
		return yamlOverride
	}
	switch v := os.Getenv("FACTUM_WORKER_API_SOCKET"); v {
	case "none", "0":
		return ""
	case "":
		return DefaultHubSocket
	default:
		return v
	}
}
