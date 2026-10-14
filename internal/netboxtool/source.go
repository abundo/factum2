package netboxtool

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

const (
	// SourceField is the NetBox text custom field that stores one origin
	// as "<system>:<id>". Tenants, contacts, devices, interfaces, IP
	// addresses, and virtual machines share it.
	SourceField = "source"
	// SourceBecs is the system name BECS writes into SourceField.
	SourceBecs = "becs"
	// LegacyBecsOIDField is the integer custom field BECS used before
	// SourceField. Readers still accept it when source does not name BECS.
	LegacyBecsOIDField = "becs_oid"
)

// FormatSourceRef is the NetBox "source" custom field value. One text
// field holds every origin as "<system>:<id>" (factum:42, becs:3641645)
// so a new system does not need its own custom field.
func FormatSourceRef(system, id string) string {
	return system + ":" + id
}

// FormatBecsSource is the source value for one BECS element OID.
func FormatBecsSource(oid uint) string {
	return FormatSourceRef(SourceBecs, strconv.FormatUint(uint64(oid), 10))
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

// BecsOID returns the BECS element OID stored in a NetBox custom_fields
// object. A source value of becs:<oid> wins. The legacy integer becs_oid
// is used when source does not name BECS, so a device still matches
// before becs_oid has been copied.
func BecsOID(fields map[string]any) uint {
	if fields == nil {
		return 0
	}
	system, id := ParseSourceRef(customFieldText(fields[SourceField]))
	if strings.EqualFold(system, SourceBecs) {
		if n := positiveUint(id); n != 0 {
			return n
		}
	}
	return positiveUint(customFieldText(fields[LegacyBecsOIDField]))
}

func customFieldText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return ""
		}
		if v == math.Trunc(v) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func positiveUint(text string) uint {
	n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil || n == 0 {
		return 0
	}
	return uint(n)
}
