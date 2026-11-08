package models

import (
	"fmt"
	"regexp"
	"strings"
)

// IgnoreOrDefault returns specific when it names at least one entry.
// Otherwise it returns fallback. Lists are newline-separated; a line of
// only spaces does not count. The returned text is the stored value.
func IgnoreOrDefault(specific, fallback string) string {
	if ignoreListSet(specific) {
		return specific
	}
	return fallback
}

func ignoreListSet(list string) bool {
	for line := range strings.SplitSeq(list, "\n") {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

// ListContains reports whether value equals a line in list. An empty value
// never matches.
func ListContains(value, list string) bool {
	if value == "" {
		return false
	}
	for line := range strings.SplitSeq(list, "\n") {
		if strings.TrimSpace(line) == value {
			return true
		}
	}
	return false
}

// RoleMatches reports whether role matches a regex in list. An empty list
// matches every role. A line that does not compile is skipped.
func RoleMatches(role, list string) bool {
	if !ignoreListSet(list) {
		return true
	}
	for line := range strings.SplitSeq(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			continue
		}
		if re.MatchString(role) {
			return true
		}
	}
	return false
}

// ValidateRegexpList rejects a newline-separated list that contains a
// regular expression Go cannot compile.
func ValidateRegexpList(list string) error {
	for line := range strings.SplitSeq(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := regexp.Compile(line); err != nil {
			return fmt.Errorf("roles enabled: %q: %w", line, err)
		}
	}
	return nil
}

// MatchesWholeRegexp reports whether value matches one expression in list
// against the whole string. An empty value never matches.
func MatchesWholeRegexp(value, list string) bool {
	if value == "" || !ignoreListSet(list) {
		return false
	}
	for line := range strings.SplitSeq(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		re, err := regexp.Compile("^(?:" + line + ")$")
		if err != nil {
			continue
		}
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

// DeviceFilter is the effective destination ignore lists. Roles is a
// newline-separated regex list; an empty Roles includes every device role.
type DeviceFilter struct {
	Devices       string
	Manufacturers string
	Models        string
	Platforms     string
	Roles         string
}

// Excludes reports whether device is left out by an ignore list or by a
// non-empty role list that does not match device.Role.
func (f DeviceFilter) Excludes(device *Device) bool {
	if device == nil {
		return true
	}
	if ListContains(device.Name, f.Devices) ||
		ListContains(device.Manufacturer, f.Manufacturers) ||
		ListContains(device.ModelName, f.Models) ||
		ListContains(device.Platform, f.Platforms) ||
		!RoleMatches(device.Role, f.Roles) {
		return true
	}
	return false
}
