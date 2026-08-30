package buildinfo

import (
	"strings"
	"testing"
)

func TestSnapshotUsesLdflags(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origV, origC, origD
	})

	Version = "v1.2.3"
	Commit = "abc123def456"
	Date = "2026-01-02T03:04:05Z"

	got := Snapshot()
	if got.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", got.Version)
	}
	if got.Commit != "abc123def456" {
		t.Errorf("Commit = %q, want abc123def456", got.Commit)
	}
	if got.Date != "2026-01-02T03:04:05Z" {
		t.Errorf("Date = %q, want 2026-01-02T03:04:05Z", got.Date)
	}
	if got.GoVersion == "" || !strings.HasPrefix(got.GoVersion, "go") {
		t.Errorf("GoVersion = %q, want a goX.Y runtime version", got.GoVersion)
	}
}

func TestSnapshotMarksDirtyVersion(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origV, origC, origD
	})

	Version = "v1.2.3-4-gabcdef-dirty"
	Commit = "abcdef"
	Date = "2026-01-02T03:04:05Z"

	got := Snapshot()
	if !got.Dirty {
		t.Fatal("Dirty = false, want true when version ends in -dirty")
	}
}

func TestSnapshotAlwaysHasGoVersion(t *testing.T) {
	got := Snapshot()
	if got.GoVersion == "" {
		t.Fatal("GoVersion is empty")
	}
	if got.Version == "" {
		t.Fatal("Version is empty")
	}
}
