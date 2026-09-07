package web

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func publishOne(t *testing.T, log func(*slog.Logger)) LogEvent {
	t.Helper()
	hub := NewLogHub()
	logger := slog.New(newHubHandler(slog.NewTextHandler(io.Discard, nil), hub))
	log(logger)
	_, history, unsub := hub.Subscribe()
	t.Cleanup(unsub)
	if len(history) != 1 {
		t.Fatalf("history = %d, want 1", len(history))
	}
	return history[0]
}

func TestHubHandlerSourceFromCommandAttr(t *testing.T) {
	got := publishOne(t, func(logger *slog.Logger) {
		logger.Info("synced 3 devices", "command", "librenms", "id", "abc")
	})
	if got.Source != "librenms" {
		t.Errorf("source = %q, want librenms", got.Source)
	}
	if got.Message != "synced 3 devices" {
		t.Errorf("message = %q", got.Message)
	}
	if got.Attrs["command"] != "librenms" || got.Attrs["id"] != "abc" {
		t.Errorf("attrs = %+v", got.Attrs)
	}
}

func TestHubHandlerSourceFromWithAttrs(t *testing.T) {
	got := publishOne(t, func(logger *slog.Logger) {
		logger.With("command", "netbox").Info("Netbox sync started")
	})
	if got.Source != "netbox" {
		t.Errorf("source = %q, want netbox (WithAttrs must be merged into the event)", got.Source)
	}
}

func TestHubHandlerSourceFromSourceAttr(t *testing.T) {
	got := publishOne(t, func(logger *slog.Logger) {
		logger.Info("deleted 1 jobs", "source", "housekeeping")
	})
	if got.Source != "housekeeping" {
		t.Errorf("source = %q, want housekeeping", got.Source)
	}
}

func TestHubHandlerDefaultSourceWeb(t *testing.T) {
	got := publishOne(t, func(logger *slog.Logger) {
		logger.Info("User logged in", "username", "admin")
	})
	if got.Source != "web" {
		t.Errorf("source = %q, want web", got.Source)
	}
}

func TestHubHandlerWorkerHubPrefix(t *testing.T) {
	got := publishOne(t, func(logger *slog.Logger) {
		logger.Warn("worker hub: dial failed", "node", "w1")
	})
	if got.Source != "hub" {
		t.Errorf("source = %q, want hub", got.Source)
	}
}

func TestHubHandlerCommandWinsOverSource(t *testing.T) {
	got := publishOne(t, func(logger *slog.Logger) {
		logger.Info("command finished", "command", "icinga", "source", "job")
	})
	if got.Source != "icinga" {
		t.Errorf("source = %q, want icinga", got.Source)
	}
}

func TestHubHandlerPreservesTimeAndLevel(t *testing.T) {
	before := time.Now().Add(-time.Second)
	got := publishOne(t, func(logger *slog.Logger) {
		logger.Error("boom")
	})
	if got.Level != "ERROR" {
		t.Errorf("level = %q, want ERROR", got.Level)
	}
	if got.Time.Before(before) || got.Time.After(time.Now().Add(time.Second)) {
		t.Errorf("time = %v, out of range", got.Time)
	}
}
