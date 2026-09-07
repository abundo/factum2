package web

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LogEvent is the JSON shape streamed to the frontend log window - one per
// slog record emitted anywhere in the process (see hubHandler below).
type LogEvent struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	// Source is the subsystem that produced the line (a sync target like
	// "librenms"/"netbox", "hub", "job", or "web") so the log window can
	// label mixed streams. Derived from slog attrs, not a separate logger.
	Source string            `json:"source,omitempty"`
	Attrs  map[string]string `json:"attrs,omitempty"`
}

// logHistorySize caps how many past events a newly connected client is
// replayed, so the log window isn't empty right after opening it.
const logHistorySize = 500

// LogHub fans out LogEvents to every connected websocket client and keeps a
// bounded ring buffer so newly-subscribed clients get recent history.
type LogHub struct {
	mu      sync.Mutex
	clients map[chan LogEvent]struct{}
	history []LogEvent
}

func NewLogHub() *LogHub {
	return &LogHub{clients: make(map[chan LogEvent]struct{})}
}

// Publish fans out an event to all subscribers and appends it to history.
// A subscriber whose channel is full is skipped rather than blocked on -
// log production must never stall waiting on a slow/stuck browser tab.
func (h *LogHub) Publish(e LogEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.history = append(h.history, e)
	if len(h.history) > logHistorySize {
		h.history = h.history[len(h.history)-logHistorySize:]
	}
	for ch := range h.clients {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe registers a new client, returning its event channel, a snapshot
// of recent history to replay, and an unsubscribe func the caller must run
// (typically deferred) once it's done reading.
func (h *LogHub) Subscribe() (ch chan LogEvent, history []LogEvent, unsubscribe func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch = make(chan LogEvent, 64)
	h.clients[ch] = struct{}{}
	history = append([]LogEvent(nil), h.history...)

	unsubscribe = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
	}
	return ch, history, unsubscribe
}

// hubHandler wraps the process's existing slog.Handler, publishing every
// record to a LogHub in addition to passing it through unchanged - this is
// how the log window websocket gets its data without duplicating logging
// setup or disturbing the normal stderr output configured in
// cmdbase.SetupLog.
//
// preAttrs holds slog.Logger.With keys: slog puts those on the Handler, not
// the Record, so Handle must merge them itself or the log window would lose
// worker command/source labels attached via slog.With.
type hubHandler struct {
	next     slog.Handler
	hub      *LogHub
	preAttrs []slog.Attr
}

func newHubHandler(next slog.Handler, hub *LogHub) *hubHandler {
	return &hubHandler{next: next, hub: hub}
}

func (h *hubHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *hubHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make(map[string]string, r.NumAttrs()+len(h.preAttrs))
	for _, a := range h.preAttrs {
		if a.Key != "" {
			attrs[a.Key] = a.Value.String()
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "" {
			attrs[a.Key] = a.Value.String()
		}
		return true
	})
	if len(attrs) == 0 {
		attrs = nil
	}
	h.hub.Publish(LogEvent{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
		Source:  logEventSource(r.Message, attrs),
		Attrs:   attrs,
	})
	return h.next.Handle(ctx, r)
}

func (h *hubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	child := newHubHandler(h.next.WithAttrs(attrs), h.hub)
	child.preAttrs = make([]slog.Attr, 0, len(h.preAttrs)+len(attrs))
	child.preAttrs = append(child.preAttrs, h.preAttrs...)
	child.preAttrs = append(child.preAttrs, attrs...)
	return child
}

func (h *hubHandler) WithGroup(name string) slog.Handler {
	return newHubHandler(h.next.WithGroup(name), h.hub)
}

// logEventSource picks a short subsystem label for the log window. Worker
// command output (LogToSlog / EventToSlog) carries "command"; in-process
// reporters use "source"; a few call sites pass "target". Anything else
// from this process is "web", except worker-hub transport lines which all
// share the "worker hub:" message prefix.
func logEventSource(message string, attrs map[string]string) string {
	for _, key := range []string{"command", "source", "target"} {
		if s := attrs[key]; s != "" {
			return s
		}
	}
	if strings.HasPrefix(message, "worker hub:") {
		return "hub"
	}
	return "web"
}
