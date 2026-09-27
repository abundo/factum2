package netboxtool

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// DestinationField is the NetBox text custom field that stores every
	// downstream system's id for one device. The written form is
	// space-separated "<system>:<id>" pairs (librenms:42 zabbix:7). A JSON
	// object ({"librenms":"42","zabbix":"7"}) is accepted when reading so
	// a hand-edited value is not thrown away.
	DestinationField = "destination"
	// DestinationLibrenms is the destination key LibreNMS writes.
	DestinationLibrenms = "librenms"
)

// ErrDestinationUnreadable means the current destination value is
// non-empty and neither a JSON object nor a list of system:id pairs.
// Callers must leave the field as it is.
var ErrDestinationUnreadable = errors.New("destination value is not a JSON object or a list of system:id pairs")

var destinationSystem = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ParseDestination reads a destination custom-field value into
// system -> id. An empty value is an empty map. System names are
// lowercased. A zero id is omitted.
func ParseDestination(value any) (map[string]string, error) {
	switch v := value.(type) {
	case nil:
		return map[string]string{}, nil
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return map[string]string{}, nil
		}
		if strings.HasPrefix(text, "{") {
			var obj map[string]any
			if err := json.Unmarshal([]byte(text), &obj); err != nil {
				return nil, ErrDestinationUnreadable
			}
			return destinationIDsFromMap(obj)
		}
		return destinationIDsFromTokens(text)
	case map[string]any:
		return destinationIDsFromMap(v)
	case map[string]string:
		raw := make(map[string]any, len(v))
		for key, id := range v {
			raw[key] = id
		}
		return destinationIDsFromMap(raw)
	default:
		return nil, ErrDestinationUnreadable
	}
}

// FormatDestination writes ids as space-separated system:id pairs, sorted
// by system name. An empty map is an empty string.
func FormatDestination(ids map[string]string) string {
	if len(ids) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ids))
	for key := range ids {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+":"+ids[key])
	}
	return strings.Join(parts, " ")
}

// MergeDestination sets system to id and keeps every other system already
// stored in current. It returns the value to write. An unreadable current
// value is returned as ErrDestinationUnreadable and must not be replaced.
func MergeDestination(current any, system, id string) (string, error) {
	system, id, err := normalizeDestinationPair(system, id)
	if err != nil {
		return "", err
	}
	ids, err := ParseDestination(current)
	if err != nil {
		return "", err
	}
	ids[system] = id
	return FormatDestination(ids), nil
}

// LibrenmsDeviceID is the LibreNMS device id stored on a NetBox device.
// destination's librenms pair wins. The legacy integer fields librenms_id
// and librenms_device_id are used when destination has no librenms pair,
// so a device still matches before the migration script has run.
func LibrenmsDeviceID(fields map[string]any) uint {
	if len(fields) == 0 {
		return 0
	}
	if ids, err := ParseDestination(fields[DestinationField]); err == nil {
		if raw, ok := ids[DestinationLibrenms]; ok {
			n, convErr := strconv.ParseUint(raw, 10, 64)
			if convErr != nil || n == 0 {
				return 0
			}
			return uint(n)
		}
	}
	for _, key := range []string{"librenms_id", "librenms_device_id"} {
		if n := legacyLibrenmsID(fields[key]); n != 0 {
			return n
		}
	}
	return 0
}

// SetDestinationID stores one system's id in the destination custom field
// of a device or virtual machine. Other systems already in the field are
// copied into the value that is written. When this system already has the
// same id, NetBox is not updated.
func (nb *NetboxClient) SetDestinationID(vm bool, netboxID uint, system, id string) error {
	if netboxID == 0 {
		return fmt.Errorf("netbox destination: device id is required")
	}
	system, id, err := normalizeDestinationPair(system, id)
	if err != nil {
		return err
	}
	endpoint := "/api/dcim/devices/" + strconv.FormatUint(uint64(netboxID), 10) + "/"
	if vm {
		endpoint = "/api/virtualization/virtual-machines/" + strconv.FormatUint(uint64(netboxID), 10) + "/"
	}
	var body struct {
		CustomFields map[string]any `json:"custom_fields"`
	}
	if err := nb.restGet(endpoint, &body); err != nil {
		return err
	}
	var current any
	if body.CustomFields != nil {
		current = body.CustomFields[DestinationField]
	}
	ids, err := ParseDestination(current)
	if err != nil {
		return fmt.Errorf("netbox destination on %s: %w", endpoint, err)
	}
	if ids[system] == id {
		return nil
	}
	merged, err := MergeDestination(current, system, id)
	if err != nil {
		return fmt.Errorf("netbox destination on %s: %w", endpoint, err)
	}
	return nb.restPatch(endpoint, map[string]any{
		"custom_fields": map[string]any{DestinationField: merged},
	})
}

func normalizeDestinationPair(system, id string) (string, string, error) {
	system = strings.ToLower(strings.TrimSpace(system))
	id = strings.TrimSpace(id)
	if !destinationSystem.MatchString(system) {
		return "", "", fmt.Errorf("destination system %q is empty or not a token", system)
	}
	if id == "" || id == "0" || strings.ContainsAny(id, " \t\r\n") {
		return "", "", fmt.Errorf("destination id for %s is empty", system)
	}
	return system, id, nil
}

func destinationIDsFromMap(raw map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		system := strings.ToLower(strings.TrimSpace(key))
		if !destinationSystem.MatchString(system) {
			return nil, ErrDestinationUnreadable
		}
		id, ok := destinationIDString(value)
		if !ok {
			return nil, ErrDestinationUnreadable
		}
		if id == "" {
			continue
		}
		if prev, exists := out[system]; exists && prev != id {
			return nil, ErrDestinationUnreadable
		}
		out[system] = id
	}
	return out, nil
}

func destinationIDsFromTokens(text string) (map[string]string, error) {
	parts := strings.Fields(text)
	out := make(map[string]string, len(parts))
	for i := 0; i < len(parts); {
		tok := parts[i]
		var system, id string
		switch {
		case strings.HasSuffix(tok, ":") && tok != ":":
			if i+1 >= len(parts) {
				return nil, ErrDestinationUnreadable
			}
			system = strings.TrimSuffix(tok, ":")
			id = parts[i+1]
			i += 2
		case strings.Contains(tok, ":"):
			system, id, _ = strings.Cut(tok, ":")
			i++
		default:
			return nil, ErrDestinationUnreadable
		}
		system = strings.ToLower(strings.TrimSpace(system))
		id = strings.TrimSpace(id)
		if !destinationSystem.MatchString(system) || id == "" || id == "0" || strings.ContainsAny(id, " \t\r\n") {
			return nil, ErrDestinationUnreadable
		}
		if prev, exists := out[system]; exists && prev != id {
			return nil, ErrDestinationUnreadable
		}
		out[system] = id
	}
	return out, nil
}

// destinationIDString returns the id text of a custom-field scalar.
// ok is false when the value is a shape that must not be dropped.
// A missing or zero id returns "", true.
func destinationIDString(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", true
	case string:
		s := strings.TrimSpace(v)
		if s == "" || s == "0" {
			return "", true
		}
		if strings.ContainsAny(s, " \t\r\n") {
			return "", false
		}
		return s, true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return "", false
		}
		return destinationIDString(f)
	case float64:
		if v == 0 {
			return "", true
		}
		if v < 0 || v != math.Trunc(v) || v > float64(math.MaxUint32) {
			return "", false
		}
		return strconv.FormatUint(uint64(v), 10), true
	case int:
		return destinationIDString(float64(v))
	case int64:
		return destinationIDString(float64(v))
	case uint:
		return destinationIDString(float64(v))
	case uint64:
		if v > math.MaxUint32 {
			return "", false
		}
		return destinationIDString(float64(v))
	default:
		return "", false
	}
}

func legacyLibrenmsID(value any) uint {
	s, ok := destinationIDString(value)
	if !ok || s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}
