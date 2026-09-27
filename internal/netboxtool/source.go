package netboxtool

import "strings"

// FormatSourceRef is the NetBox "source" custom field value. One text
// field holds every origin as "<system>:<id>" (factum:42, becs:17) so a
// new system does not need its own custom field.
func FormatSourceRef(system, id string) string {
	return system + ":" + id
}

// ParseSourceRef splits a "source" custom field value on the first colon.
// The id may itself contain colons. A value with no colon is an
// unmigrated system name and an empty id.
func ParseSourceRef(value string) (system, id string) {
	system, id, ok := strings.Cut(value, ":")
	if !ok {
		return strings.TrimSpace(value), ""
	}
	return strings.TrimSpace(system), strings.TrimSpace(id)
}
