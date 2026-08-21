package buildinfo

// Set by GoReleaser (and the Makefile) via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
