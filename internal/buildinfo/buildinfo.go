package buildinfo

import (
	"runtime"
	"strings"
)

// Set by GoReleaser (and the Makefile) via -ldflags. Unstamped builds
// (`go run`, `go test`) keep these defaults; no VCS probing at runtime.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// IsDev reports an unstamped identity: Version/Commit left at the
// `go run` / `go test` defaults. Makefile and GoReleaser always override
// both. Empty strings are not IsDev — a peer that omitted handshake
// headers is a missing identity, not a dev process.
func IsDev(version, commit string) bool {
	v, c := strings.TrimSpace(version), strings.TrimSpace(commit)
	return v == "dev" || c == "none"
}

// CanonicalVersion strips a leading v/V so git-describe stamps ("v1.0.6")
// match GoReleaser {{.Version}} ("1.0.6"). Other characters are kept.
func CanonicalVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	if v[0] == 'v' || v[0] == 'V' {
		return v[1:]
	}
	return v
}

// Info is the JSON shape of GET /api/version.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	Dirty     bool   `json:"dirty,omitempty"`
}

// Snapshot returns the process identity from ldflags plus the Go runtime
// version. Dirty is true when Version was stamped with git describe --dirty.
func Snapshot() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Dirty:     strings.HasSuffix(Version, "-dirty"),
	}
}
