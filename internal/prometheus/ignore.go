package prometheus

// Ignore interfaces is a newline-separated list of regular expressions
// (Settings.PrometheusIgnoreInterfaces). A scraped sample whose ifName,
// ifDescr, or ifAlias matches one of them is dropped before Prometheus
// stores it, so it is not graphed or alerted.
//
// Prometheus cannot take the pattern from a target label. Sync rewrites
// the factum-if-ignore block in the scrape config next to the file_sd
// file (runtime.yml or prometheus.yml). The expression matches the whole
// name: Vlan.* drops Vlan10. An empty list drops nothing.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ignoreBegin = "# factum-if-ignore:begin"
	ignoreEnd   = "# factum-if-ignore:end"
	// neverMatch is a relabel regex that matches no sample, including a
	// sample with an empty ifName. "$^" is not usable: anchored, it matches
	// the empty string and would drop sysUpTime and the other device metrics.
	neverMatch = `a^`
)

// ErrNoIgnoreConfig means none of the scrape configs next to the file_sd
// file contain the factum-if-ignore markers.
var ErrNoIgnoreConfig = errors.New("prometheus config has no factum-if-ignore marker")

// CompileIgnoreInterfaces checks list and returns the single expression
// written into the scrape config. An empty list returns "".
func CompileIgnoreInterfaces(list string) (string, error) {
	var parts []string
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			return "", fmt.Errorf("ignore interfaces: %q: %w", line, err)
		}
		if re.MatchString("") {
			return "", fmt.Errorf("ignore interfaces: %q matches an empty name", line)
		}
		if _, err := regexp.Compile("^(?:" + line + ")$"); err != nil {
			return "", fmt.Errorf("ignore interfaces: %q: %w", line, err)
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(?:" + strings.Join(parts, ")|(?:") + ")", nil
}

func ignoreConfigCandidates(dest string) []string {
	dir := filepath.Dir(dest)
	return []string{
		filepath.Join(dir, "runtime.yml"),
		filepath.Join(dir, "prometheus.yml"),
		filepath.Join(filepath.Dir(dir), "runtime.yml"),
		filepath.Join(filepath.Dir(dir), "prometheus.yml"),
	}
}

func ignoreBlock(indent, pattern string) string {
	if pattern == "" {
		pattern = neverMatch
	}
	quoted := fmt.Sprintf("%q", pattern)
	var b strings.Builder
	write := func(format string, args ...any) {
		fmt.Fprintf(&b, indent+format+"\n", args...)
	}
	write("%s", ignoreBegin)
	for _, label := range []string{"ifName", "ifDescr", "ifAlias"} {
		write("- source_labels: [%s]", label)
		write("  regex: %s", quoted)
		write("  action: drop")
	}
	write("%s", ignoreEnd)
	return b.String()
}

// replaceIgnoreBlock swaps the marker region for pattern. pattern is the
// combined expression, or empty to drop nothing.
func replaceIgnoreBlock(body []byte, pattern string) ([]byte, bool, error) {
	s := string(body)
	begin := strings.Index(s, ignoreBegin)
	end := strings.Index(s, ignoreEnd)
	if begin < 0 || end < 0 || end < begin {
		return nil, false, ErrNoIgnoreConfig
	}
	lineStart := strings.LastIndex(s[:begin], "\n") + 1
	indent := s[lineStart:begin]
	if strings.TrimSpace(indent) != "" {
		return nil, false, fmt.Errorf("factum-if-ignore marker is not at the start of a line")
	}
	endLine := end + len(ignoreEnd)
	if endLine < len(s) && s[endLine] == '\r' {
		endLine++
	}
	if endLine < len(s) && s[endLine] == '\n' {
		endLine++
	}
	block := ignoreBlock(indent, pattern)
	updated := s[:lineStart] + block + s[endLine:]
	return []byte(updated), updated != s, nil
}

// InstallIgnoreInterfaces compiles list and rewrites the first writable
// scrape config next to destFile that contains the markers. changed is
// false when the block was already up to date.
func InstallIgnoreInterfaces(destFile, list string) (path string, changed bool, err error) {
	pattern, err := CompileIgnoreInterfaces(list)
	if err != nil {
		return "", false, err
	}
	var saw bool
	var writeErr error
	for _, candidate := range ignoreConfigCandidates(destFile) {
		body, rerr := os.ReadFile(candidate)
		if rerr != nil {
			continue
		}
		if !bytes.Contains(body, []byte(ignoreBegin)) {
			continue
		}
		saw = true
		updated, diff, rerr := replaceIgnoreBlock(body, pattern)
		if rerr != nil {
			return candidate, false, rerr
		}
		if !diff {
			return candidate, false, nil
		}
		if werr := writeFile(candidate, updated); werr != nil {
			writeErr = werr
			continue
		}
		return candidate, true, nil
	}
	if !saw {
		return "", false, ErrNoIgnoreConfig
	}
	if writeErr != nil {
		return "", false, writeErr
	}
	return "", false, ErrNoIgnoreConfig
}

func writeFile(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if werr := os.WriteFile(path, body, 0o644); werr != nil {
			os.Remove(tmp)
			return fmt.Errorf("rename %s: %v (write: %w)", path, err, werr)
		}
		os.Remove(tmp)
	}
	return nil
}
