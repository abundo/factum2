package worker

import (
	"fmt"
	"io"
	"net/http"
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

func hubHandshakeHeaders(token string) http.Header {
	return http.Header{
		"Authorization":  {"Bearer " + token},
		hubVersionHeader: {buildinfo.Version},
		hubCommitHeader:  {buildinfo.Commit},
	}
}

func checkHubVersion(remoteVersion, remoteCommit string) error {
	if remoteVersion == buildinfo.Version && remoteCommit == buildinfo.Commit {
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
