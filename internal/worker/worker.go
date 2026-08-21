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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/util"
	"golang.org/x/sync/errgroup"
)

// ErrHubDisconnected is returned by DoHubRequest when no hub session is
// current. The unix server maps it (and every other transport error) to 502.
var ErrHubDisconnected = errors.New("hub disconnected")

type hubSession struct {
	mu      sync.Mutex
	outbox  chan Envelope
	waiters map[string]chan ResponseMsg // each chan is buffered cap 1
}

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

	hubMu sync.Mutex
	hub   *hubSession // latest connected /hub; nil if disconnected
}

func New(cfg *util.ConfigWorker) *Worker {
	return &Worker{cfg: cfg}
}

// Start validates this instance's command allowlist and runs the hub
// listener and unix API until ctx is cancelled or either fails.
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
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return w.runHubListener(ctx) })
	g.Go(func() error { return w.runLocalAPI(ctx) })
	return g.Wait()
}

func (w *Worker) setHubSession(sess *hubSession) {
	w.hubMu.Lock()
	prev := w.hub
	w.hub = sess
	w.hubMu.Unlock()
	if prev != nil {
		failWaiters(prev, "hub connection replaced")
	}
}

func (w *Worker) clearHubSession(sess *hubSession) {
	w.hubMu.Lock()
	if w.hub == sess {
		w.hub = nil
	}
	w.hubMu.Unlock()
	failWaiters(sess, "hub disconnected")
}

func failWaiters(sess *hubSession, errMsg string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	for id, ch := range sess.waiters {
		select {
		case ch <- ResponseMsg{ID: id, Error: errMsg}:
			slog.Warn("worker hub: failing RPC waiter", "id", id, "err", errMsg)
		default:
		}
		delete(sess.waiters, id)
	}
}

// DoHubRequest sends one HTTP-subset RPC over the current hub session and
// waits for the matching response. ResponseMsg.Error is always returned as
// err (unix 502), never as HTTP 200 with an error string.
func (w *Worker) DoHubRequest(ctx context.Context, method, path string, body []byte) (status int, respBody []byte, err error) {
	id, err := newID()
	if err != nil {
		return 0, nil, err
	}
	req := RequestMsg{ID: id, Method: method, Path: path}
	if len(body) > 0 {
		req.Body = json.RawMessage(body)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return 0, nil, err
	}
	env := Envelope{Type: EnvelopeRequest, Payload: payload}
	frame, err := marshalHubFrame(env)
	if err != nil {
		return 0, nil, err
	}
	if len(frame) > hubMaxMessageSize {
		return http.StatusRequestEntityTooLarge, []byte(`{"error":"hub request too large"}`), nil
	}
	env.frame = frame

	ch := make(chan ResponseMsg, 1)
	w.hubMu.Lock()
	sess := w.hub
	if sess == nil {
		w.hubMu.Unlock()
		return 0, nil, ErrHubDisconnected
	}
	sess.mu.Lock()
	sess.waiters[id] = ch
	sess.mu.Unlock()
	w.hubMu.Unlock()
	defer func() {
		sess.mu.Lock()
		delete(sess.waiters, id)
		sess.mu.Unlock()
	}()

	if !trySend(sess.outbox, env) {
		return 0, nil, fmt.Errorf("hub outbox stuck")
	}

	timer := time.NewTimer(hubRPCTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-timer.C:
		return 0, nil, fmt.Errorf("hub RPC timeout")
	case msg := <-ch:
		if msg.Error != "" {
			return 0, nil, fmt.Errorf("%s", msg.Error)
		}
		return msg.Status, msg.Body, nil
	}
}
