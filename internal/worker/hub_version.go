package worker

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/abundo/factum2/internal/buildinfo"
)

// Handshake identity headers. The primary sets these on the WSS dial; the
// agent rejects a mismatch with HTTP 409 before Upgrade. HelloMsg repeats
// the same pair so the primary can refuse to register a node that slipped
// past the HTTP check (an old agent that does not read the headers).
const (
	hubVersionHeader = "X-Factum-Version"
	hubCommitHeader  = "X-Factum-Commit"
)

// hubDevHandshake is the compose-lab signal (factum-web sets APP_ENV=
// development). --compose restarts only the primary; dest agents keep
// whatever binary they last exec'd. Matching versions is then the
// developer's job, not a 409.
func hubDevHandshake() bool {
	return os.Getenv("APP_ENV") == "development"
}

func hubHandshakeHeaders(token string) http.Header {
	version, commit := buildinfo.Version, buildinfo.Commit
	if hubDevHandshake() {
		// Agents built before this skip still 409 stamped mismatches.
		// Unstamped identity hits their existing IsDev skip.
		version, commit = "dev", "none"
	}
	return http.Header{
		"Authorization":  {"Bearer " + token},
		hubVersionHeader: {version},
		hubCommitHeader:  {commit},
	}
}

func checkHubVersion(remoteVersion, remoteCommit string) error {
	// Unstamped `go run` / `go test` (dev/none) skip the check on either
	// side so a developer GUI can dial installed workers. APP_ENV=
	// development does the same for Makefile-stamped compose binaries.
	// Production (GoReleaser) builds still require the same version and
	// commit (a leading v on the version is ignored).
	if hubDevHandshake() || buildinfo.IsDev(buildinfo.Version, buildinfo.Commit) || buildinfo.IsDev(remoteVersion, remoteCommit) {
		if remoteVersion != buildinfo.Version || remoteCommit != buildinfo.Commit {
			reason := "unstamped/dev build"
			if hubDevHandshake() {
				reason = "APP_ENV=development"
			}
			slog.Warn("worker hub: skipping version check ("+reason+")",
				"peer", hubIdent(remoteVersion),
				"peer_commit", hubIdent(remoteCommit),
				"local", hubIdent(buildinfo.Version),
				"local_commit", hubIdent(buildinfo.Commit),
			)
		}
		return nil
	}
	sameVersion := buildinfo.CanonicalVersion(remoteVersion) == buildinfo.CanonicalVersion(buildinfo.Version)
	if sameVersion && remoteCommit == buildinfo.Commit {
		return nil
	}
	return fmt.Errorf("version mismatch: peer %s (%s) != local %s (%s)",
		hubIdent(remoteVersion), hubIdent(remoteCommit),
		hubIdent(buildinfo.Version), hubIdent(buildinfo.Commit))
}

func hubIdent(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// hubDialError maps a failed WSS handshake onto a version-mismatch error
// when the agent rejected us with 409. resp.Body is closed when non-nil.
func hubDialError(dialErr error, resp *http.Response) error {
	if resp == nil {
		return dialErr
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		return dialErr
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	msg := strings.TrimSpace(string(body))
	if err != nil || msg == "" {
		return fmt.Errorf("version mismatch: %w", dialErr)
	}
	return fmt.Errorf("%s", msg)
}
