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

func TestCanonicalVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"v1.0.6", "1.0.6"},
		{"1.0.6", "1.0.6"},
		{"V1.0.6", "1.0.6"},
		{" v1.0.6 ", "1.0.6"},
		{"v1.0.6-3-gdeadbee", "1.0.6-3-gdeadbee"},
		{"", ""},
		{"dev", "dev"},
	}
	for _, c := range cases {
		if got := CanonicalVersion(c.in); got != c.want {
			t.Errorf("CanonicalVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsDev(t *testing.T) {
	if !IsDev("dev", "none") {
		t.Fatal("go run defaults should be IsDev")
	}
	if !IsDev("dev", "abc123") {
		t.Fatal("Version=dev should be IsDev")
	}
	if !IsDev("v1.0.0", "none") {
		t.Fatal("Commit=none should be IsDev")
	}
	if IsDev("v1.0.0", "abc123") {
		t.Fatal("stamped release should not be IsDev")
	}
	if IsDev("v1.0.0-3-gdeadbee", "deadbee") {
		t.Fatal("git-describe stamp should not be IsDev")
	}
	if IsDev("", "") {
		t.Fatal("empty handshake identity is missing, not go run")
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
