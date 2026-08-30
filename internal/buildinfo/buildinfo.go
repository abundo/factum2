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
