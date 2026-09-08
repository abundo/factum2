package util

import (
	"os"
	"time"
)

// DefaultHubSocket is the unix HTTP path factum2-worker listens on and
// co-located CLIs probe. One constant so the two sides cannot drift.
const DefaultHubSocket = "/run/factum2-worker/api.sock"

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
// WithoutHubSocket returns a copy of cfg that never probes the worker unix
// socket. Primary-side CLIs (device-sync and anything else that runs next
// to factum2-web) must use this: a co-located factum2-worker still listens
// on the socket, and FactumHTTP would otherwise send every call over hub
// RPC. Dest-host CLIs (dns, icinga, …) keep the default probe.
func WithoutHubSocket(cfg ConfigFactum) ConfigFactum {
	cfg.Socket = "none"
	return cfg
}

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
