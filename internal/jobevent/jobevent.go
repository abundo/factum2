// Package jobevent lets a sync tool (internal/dns, internal/icinga,
// internal/librenms, internal/netbox, internal/lime) report structured
// info/warning/error progress instead of ad hoc fmt.Println/slog calls.
// Deliberately has zero dependency on internal/worker (which is what
// consumes these events on the primary side, over the hub transport's
// "event" envelope) so neither package needs to import the other.
package jobevent

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

type Level string

const (
	Info    Level = "info"
	Warning Level = "warning"
	Error   Level = "error"
)

type Reporter interface {
	Emit(level Level, format string, args ...any)
	// EmitErr emits err at Error level. A nil err is a no-op. Callers should
	// prefer this over Emit(Error, err.Error()), which mis-renders any '%'
	// in the error message since err.Error() is passed as a format string.
	EmitErr(err error)
}

// StdoutReporter writes one JSON object per line - {"level":...,
// "message":...} - decoded by the agent (internal/worker/hub_agent.go's
// streamOutput) and relayed as a structured event instead of a plain log
// line. Used when a CLI is invoked with --job.
type StdoutReporter struct {
	w io.Writer
}

func NewStdoutReporter(w io.Writer) *StdoutReporter {
	return &StdoutReporter{w: w}
}

func (r *StdoutReporter) Emit(level Level, format string, args ...any) {
	data, err := json.Marshal(struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}{string(level), fmt.Sprintf(format, args...)})
	if err != nil {
		return
	}
	fmt.Fprintln(r.w, string(data))
}

func (r *StdoutReporter) EmitErr(err error) {
	if err == nil {
		return
	}
	r.Emit(Error, "%s", err)
}

// ConsoleReporter writes human-readable text - the default when a CLI is
// run interactively (no --job), preserving roughly today's plain-print
// behavior for Info, with a visible prefix for Warning/Error.
type ConsoleReporter struct {
	w io.Writer
}

func NewConsoleReporter(w io.Writer) *ConsoleReporter {
	return &ConsoleReporter{w: w}
}

func (r *ConsoleReporter) Emit(level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	switch level {
	case Warning:
		fmt.Fprintf(r.w, "WARNING: %s\n", msg)
	case Error:
		fmt.Fprintf(r.w, "ERROR: %s\n", msg)
	default:
		fmt.Fprintln(r.w, msg)
	}
}

func (r *ConsoleReporter) EmitErr(err error) {
	if err == nil {
		return
	}
	r.Emit(Error, "%s", err)
}

// SlogReporter emits via log/slog - for callers already running in-process
// (not as a predefined-command subprocess), e.g. web.ApiNetboxWebhook
// calling internal/netbox.FactumSyncNetbox directly, which has no
// meaningful "stdout" to write JSON lines to.
type SlogReporter struct{}

func NewSlogReporter() SlogReporter {
	return SlogReporter{}
}

func (SlogReporter) Emit(level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	switch level {
	case Warning:
		slog.Warn(msg)
	case Error:
		slog.Error(msg)
	default:
		slog.Info(msg)
	}
}

func (r SlogReporter) EmitErr(err error) {
	if err == nil {
		return
	}
	r.Emit(Error, "%s", err)
}
