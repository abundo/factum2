// Package worker runs predefined shell commands dispatched by the primary
// over the hub transport (see hub.go/hub_agent.go).
//
// A factum-worker instance activates every entry in ConfigWorker.Commands,
// keyed by name - there's no separate addressing by node name, and any
// instance with a given command name will run it if the primary dispatches
// to that name. If the name is also a valid sync target
// (worker.IsValidSyncTarget), the same predefined command also runs
// whenever someone clicks that target's "Sync" button on the web UI's sync
// overview page - web.ApiSyncTrigger and "factum-worker run" both go
// through the exact same RemoteManager.SendCommand/RunAndWait dispatch path
// on the primary, so there's no separate mechanism to keep in sync.
//
// The primary (web.RemoteManager, hub.go) dials out to this instance's hub
// listener (ConfigWorker.Listen) rather than the other way round - see
// AGENTS.md's "Worker / hub transport" section for why. "Which instances
// are up and what they handle" is answered by the primary's own live
// connection state (web.ApiWorkerStatus), not a broadcast the instance has
// to participate in.
//
// An agent never executes a command line received from the primary
// directly - only the name is used to look up a predefined command, and an
// instance only ever runs commands from its own ConfigWorker.Commands
// allowlist, so a forged or replayed message can at most trigger one of the
// commands the operator already defined for that instance.
package worker

import (
	"context"
	"fmt"

	"github.com/abundo/factum2/internal/util"
)

type Worker struct {
	// cfg is just the worker section of the binary's config (util.
	// ConfigAgentRoot.Worker, or util.ConfigRoot.Worker if the caller still
	// embeds the full root) - internal/worker never reads anything outside
	// ConfigWorker, so it doesn't need to know which root struct it came
	// from.
	cfg *util.ConfigWorker
	// runCtx is the process-lifetime context passed to Start - runCommand
	// (hub_agent.go) uses this, not any per-connection context, so a
	// predefined command keeps running to completion even if the hub
	// connection to the primary drops mid-run.
	runCtx context.Context
}

func New(cfg *util.ConfigWorker) *Worker {
	return &Worker{cfg: cfg}
}

// Start validates this instance's command allowlist and runs the hub
// listener until ctx is cancelled or it fails.
func (w *Worker) Start(ctx context.Context) error {
	if len(w.cfg.Commands) == 0 {
		return fmt.Errorf("worker.commands must configure at least one command")
	}
	fmt.Printf("Handling commands: ")
	for k := range w.cfg.Commands {
		fmt.Printf("%s ", k)
	}
	fmt.Println()
	w.runCtx = ctx

	if w.cfg.Listen == "" {
		return fmt.Errorf("worker.listen must be set - factum-worker has no transport without it")
	}
	return w.runHubListener(ctx)
}
