package worker

// hub_agent.go is the agent-side half of the hub transport (see hub.go for
// the primary-side RemoteManager it's dialed by). factum-worker has never
// run an HTTP server before this - it's a single unauthenticated-by-default
// route deliberately kept off Echo/the web module, since an agent binary
// has no other use for a web framework.

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/util"
	"github.com/gorilla/websocket"
)

const (
	hubWriteWait  = 10 * time.Second
	hubPingPeriod = 30 * time.Second
)

var hubUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The only caller is ever the primary's RemoteManager, never a browser -
	// origin-checking would just add a footgun (mismatched Host on a
	// server-to-server call), not a real protection, here.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// runHubListener runs the agent-side half of the hub transport: accepts the
// primary's connection, validates its bearer token, and reports this
// instance's hostname/roles. Started unconditionally by Start (worker.go) -
// worker.listen is required post-cutover.
func (w *Worker) runHubListener(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(HubPath, w.handleHubConn)

	srv := &http.Server{
		Addr:    w.cfg.Listen,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("worker hub listener started", "listen", w.cfg.Listen)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), hubWriteWait)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("hub listener: %w", err)
		}
		return nil
	}
}

// checkHubToken validates the primary's "Authorization: Bearer <token>"
// against local config - same constant-time-compare, fail-closed-on-empty
// discipline as web/auth.go's checkServiceToken, just with the roles of
// caller/callee reversed (here the agent is the one authenticating the
// inbound connection).
func (w *Worker) checkHubToken(r *http.Request) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	presented := strings.TrimPrefix(auth, prefix)
	if presented == "" || w.cfg.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(w.cfg.Token)) == 1
}

func (w *Worker) handleHubConn(rw http.ResponseWriter, r *http.Request) {
	if !w.checkHubToken(r) {
		slog.Error("worker hub: rejected connection, invalid token", "remote", r.RemoteAddr)
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := hubUpgrader.Upgrade(rw, r, nil)
	if err != nil {
		slog.Error("worker hub: upgrade failed", "err", err)
		return
	}
	defer conn.Close()
	slog.Info("worker hub: hub connected", "remote", r.RemoteAddr)

	outbox := make(chan Envelope, outboxSize)
	done := make(chan struct{})
	defer close(done)
	go runWriter(conn, outbox, done, hubPingPeriod)

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	helloPayload, err := json.Marshal(HelloMsg{Hostname: hostname, Roles: commandNames(w.cfg.Commands)})
	if err != nil {
		slog.Error("worker hub: marshal hello", "err", err)
		return
	}
	if !trySend(outbox, Envelope{Type: EnvelopeHello, Payload: helloPayload}) {
		slog.Error("worker hub: send hello failed, outbox stuck")
		return
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			slog.Error("worker hub: invalid envelope, discarding", "err", err)
			continue
		}
		switch env.Type {
		case EnvelopeCommand:
			var cmdMsg CommandMsg
			if err := json.Unmarshal(env.Payload, &cmdMsg); err != nil {
				slog.Error("worker hub: invalid command envelope, discarding", "err", err)
				continue
			}
			slog.Debug("received command", "id", cmdMsg.ID, "command", cmdMsg.Command, "args", cmdMsg.Args)
			go w.runCommand(w.runCtx, cmdMsg, outbox)
		default:
			slog.Debug("worker hub: unhandled envelope type", "type", env.Type)
		}
	}
}

// commandNames reports the names this instance can run as a command agent -
// the hub's hello is meant to answer "what could a command dispatch to this
// node run", i.e. exactly the ConfigWorker.Commands allowlist.
func commandNames(commands map[string]util.ConfigWorkerCommand) []string {
	out := make([]string, 0, len(commands))
	for name := range commands {
		out = append(out, name)
	}
	return out
}

// runCommand looks up cmdMsg.Command in w.cfg.Commands (the allowlist - a
// command name not in this map is rejected even if received), runs it, and
// streams its output back as log envelopes via outbox. Uses ctx (the
// Worker's own process-lifetime context, passed in as w.runCtx by
// handleHubConn), not a per-connection context, so the command keeps
// running to completion even if the hub connection drops mid-run.
func (w *Worker) runCommand(ctx context.Context, cmdMsg CommandMsg, outbox chan<- Envelope) {
	logger := slog.With("id", cmdMsg.ID, "command", cmdMsg.Command)

	predefined, ok := w.cfg.Commands[cmdMsg.Command]
	if !ok {
		logger.Error("rejected unknown command")
		sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: StreamExit, ExitCode: -1, Err: "unknown command"})
		return
	}

	logger.Info("running command")
	args := append(append([]string{}, predefined.Args...), cmdMsg.Args...)
	cmd := exec.CommandContext(ctx, predefined.Cmd, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("stdout pipe", "err", err)
		sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: StreamExit, ExitCode: -1, Err: err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.Error("stderr pipe", "err", err)
		sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: StreamExit, ExitCode: -1, Err: err.Error()})
		return
	}

	if err := cmd.Start(); err != nil {
		logger.Error("start command", "err", err)
		sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: StreamExit, ExitCode: -1, Err: err.Error()})
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go streamOutput(&wg, cmdMsg, stdout, StreamStdout, outbox)
	go streamOutput(&wg, cmdMsg, stderr, StreamStderr, outbox)
	wg.Wait()

	exitCode := 0
	errMsg := ""
	if err := cmd.Wait(); err != nil {
		errMsg = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: StreamExit, ExitCode: exitCode, Err: errMsg})
	logger.Info("command finished", "exit_code", exitCode)
}

// streamOutput relays r line by line as LogMsg (unchanged behavior). For
// the stdout stream only, it first tries to decode each line as a
// structured jobevent line - {"level":"info"/"warning"/"error",
// "message":...} - and sends an EnvelopeEvent instead when that succeeds;
// anything else (plain text from a tool not invoked with --job, or any
// line whose "level" isn't one of the three known values) falls through to
// the usual LogMsg unchanged. Requiring a known level, not just valid JSON,
// matters: some tools already print well-formed-but-unrelated JSON (e.g.
// internal/icinga dumping a request body), which would otherwise decode
// into a blank {Level:"",Message:""} event and silently swallow the real
// line. Stderr is never sniffed - crashes/unexpected output always stay as
// raw log lines.
func streamOutput(wg *sync.WaitGroup, cmdMsg CommandMsg, r io.Reader, stream string, outbox chan<- Envelope) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if stream == StreamStdout {
			if level, message, ok := decodeEventLine(line); ok {
				sendEvent(outbox, EventMsg{ID: cmdMsg.ID, Target: cmdMsg.Command, Level: level, Message: message})
				continue
			}
		}
		sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: stream, Data: line})
	}
	if err := scanner.Err(); err != nil {
		sendLog(outbox, LogMsg{ID: cmdMsg.ID, Command: cmdMsg.Command, Stream: stream, Data: fmt.Sprintf("[stream read error: %v]", err)})
	}
}

// decodeEventLine reports whether line is a valid jobevent line, i.e.
// {"level":..., "message":...} with level exactly "info"/"warning"/
// "error" - a bare JSON-decode success isn't enough, see streamOutput's
// doc comment.
func decodeEventLine(line string) (level, message string, ok bool) {
	var raw struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return "", "", false
	}
	switch raw.Level {
	case "info", "warning", "error":
		return raw.Level, raw.Message, true
	default:
		return "", "", false
	}
}

func sendEvent(outbox chan<- Envelope, msg EventMsg) {
	payload, err := json.Marshal(msg)
	if err != nil {
		slog.Error("worker hub: marshal event message", "err", err)
		return
	}
	if !trySend(outbox, Envelope{Type: EnvelopeEvent, Payload: payload}) {
		slog.Warn("worker hub: outbox stuck, dropping event", "id", msg.ID, "target", msg.Target)
	}
}

func sendLog(outbox chan<- Envelope, msg LogMsg) {
	payload, err := json.Marshal(msg)
	if err != nil {
		slog.Error("worker hub: marshal log message", "err", err)
		return
	}
	if !trySend(outbox, Envelope{Type: EnvelopeLog, Payload: payload}) {
		slog.Warn("worker hub: outbox stuck, dropping log line", "id", msg.ID, "command", msg.Command, "stream", msg.Stream)
	}
}
