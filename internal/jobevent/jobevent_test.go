package jobevent

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogReporterIncludesSourceAttr(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	NewSlogReporter("source", "netbox").Emit(Info, "Netbox sync started")
	got := buf.String()
	if !strings.Contains(got, "source=netbox") {
		t.Errorf("log missing source=netbox:\n%s", got)
	}
	if !strings.Contains(got, "Netbox sync started") {
		t.Errorf("log missing message:\n%s", got)
	}
}

func TestSlogReporterNoAttrs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	NewSlogReporter().Emit(Warning, "something odd")
	got := buf.String()
	if !strings.Contains(got, "something odd") {
		t.Errorf("log missing message:\n%s", got)
	}
	if !strings.Contains(got, "level=WARN") {
		t.Errorf("log missing WARN level:\n%s", got)
	}
}
