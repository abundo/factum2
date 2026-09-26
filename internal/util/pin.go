package util

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PinnedPath returns the worker-local path a sync may write or execute.
// The primary's database value is accepted only when it matches this pin,
// so an admin session or a database write cannot redirect a worker onto
// another binary or file.
func PinnedPath(local, remote, name string) (string, error) {
	local = strings.TrimSpace(local)
	remote = strings.TrimSpace(remote)
	if local == "" {
		return "", fmt.Errorf("%s is not set in the worker config; refusing the path from the primary", name)
	}
	if strings.Contains(local, "\x00") || strings.ContainsAny(local, "\r\n") {
		return "", fmt.Errorf("%s contains a control character", name)
	}
	if !filepath.IsAbs(local) {
		return "", fmt.Errorf("%s must be an absolute path", name)
	}
	cleaned := filepath.Clean(local)
	if remote != "" && filepath.Clean(remote) != cleaned {
		return "", fmt.Errorf("%s from the primary (%s) does not match %s (%s)", name, remote, name, cleaned)
	}
	return cleaned, nil
}

// PinnedExecutable is PinnedPath for a command: an absolute path, or a
// single bare name with no directory separator (looked up on PATH).
func PinnedExecutable(local, remote, name string) (string, error) {
	local = strings.TrimSpace(local)
	remote = strings.TrimSpace(remote)
	if local == "" {
		return "", fmt.Errorf("%s is not set in the worker config; refusing the binary from the primary", name)
	}
	if strings.Contains(local, "\x00") || strings.ContainsAny(local, " \t\r\n") {
		return "", fmt.Errorf("%s must be a single path", name)
	}
	if !filepath.IsAbs(local) {
		if strings.ContainsAny(local, `/\`) || local == "." || local == ".." || strings.Contains(local, "..") {
			return "", fmt.Errorf("%s must be an absolute path or a bare command name", name)
		}
	} else {
		local = filepath.Clean(local)
	}
	if remote != "" && remote != local {
		return "", fmt.Errorf("%s from the primary (%s) does not match %s (%s)", name, remote, name, local)
	}
	return local, nil
}
