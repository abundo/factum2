package cfgmgmt

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/factum2/models"
)

var knownVarTypes = map[string]bool{
	models.VarTypeString:       true,
	models.VarTypeInt:          true,
	models.VarTypeBool:         true,
	models.VarTypeEnum:         true,
	models.VarTypeIP:           true,
	models.VarTypePrefix:       true,
	models.VarTypeVLAN:         true,
	models.VarTypeInterfaceRef: true,
	models.VarTypeSecret:       true,
	models.VarTypeList:         true,
	models.VarTypeMap:          true,
}

func ValidVarType(t string) bool { return knownVarTypes[t] }

const maxTypeDepth = 8

func ValidScopeKind(k string) bool {
	switch k {
	case models.ConfigScopeKindFolder, models.ConfigScopeKindSite, models.ConfigScopeKindLocation,
		models.ConfigScopeKindDevice, models.ConfigScopeKindInterface:
		return true
	}
	return false
}

// varConstraints is the JSON object stored on ConfigVariableDef.Constraints.
// Scalar fields (enum, min, max, regex) apply to the value itself. For list,
// items is the entry type (a type name string or a nested constraint object)
// and min/max are length. For map, keys and values work the same way and
// min/max are entry count. JSON object keys are always strings; keys.type
// may be string, int, enum, ip, prefix, vlan, interface_ref, bool, or secret.
type varConstraints struct {
	Type   string          `json:"type"`
	Enum   []string        `json:"enum"`
	Min    *float64        `json:"min"`
	Max    *float64        `json:"max"`
	Regex  string          `json:"regex"`
	Items  json.RawMessage `json:"items"`
	Keys   json.RawMessage `json:"keys"`
	Values json.RawMessage `json:"values"`
}

func parseConstraints(raw json.RawMessage) varConstraints {
	var c varConstraints
	if len(raw) == 0 || string(raw) == "null" {
		return c
	}
	_ = json.Unmarshal(raw, &c)
	return c
}

// nestedConstraints accepts either a type name ("ip") or a full constraint
// object ({"type":"int","min":1}).
func nestedConstraints(raw json.RawMessage) *varConstraints {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var typeName string
	if err := json.Unmarshal(raw, &typeName); err == nil {
		if typeName == "" {
			return nil
		}
		return &varConstraints{Type: typeName}
	}
	c := parseConstraints(raw)
	return &c
}

func parsePlatforms(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var p []string
	_ = json.Unmarshal(raw, &p)
	return p
}

func platformAllowed(def *models.ConfigVariableDef, platform string) bool {
	plats := parsePlatforms(def.Platforms)
	if len(plats) == 0 {
		return true
	}
	p := NormalizePlatform(platform)
	for _, x := range plats {
		if NormalizePlatform(x) == p {
			return true
		}
	}
	return false
}

func jsonValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON value: %w", err)
	}
	return v, nil
}

func asInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(n), true
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	}
	return 0, false
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asList(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out, true
	}
	return nil, false
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

var allowedMapKeyTypes = map[string]bool{
	models.VarTypeString:       true,
	models.VarTypeInt:          true,
	models.VarTypeBool:         true,
	models.VarTypeEnum:         true,
	models.VarTypeIP:           true,
	models.VarTypePrefix:       true,
	models.VarTypeVLAN:         true,
	models.VarTypeInterfaceRef: true,
	models.VarTypeSecret:       true,
}

func validMapKeyType(t string) bool {
	if t == "" {
		return true
	}
	return allowedMapKeyTypes[t]
}

// ValidateConstraints checks nested items/keys/values type names and that
// list/map-only fields are not set on scalar types.
func ValidateConstraints(typ string, raw json.RawMessage) error {
	return validateConstraints(typ, parseConstraints(raw), 0)
}

func validateConstraints(typ string, c varConstraints, depth int) error {
	if depth > maxTypeDepth {
		return fmt.Errorf("type nesting too deep")
	}
	if c.Regex != "" {
		if _, err := regexp.Compile(c.Regex); err != nil {
			return fmt.Errorf("invalid constraint regex: %w", err)
		}
	}
	items := nestedConstraints(c.Items)
	keys := nestedConstraints(c.Keys)
	values := nestedConstraints(c.Values)
	switch typ {
	case models.VarTypeList:
		if keys != nil || values != nil {
			return fmt.Errorf("keys/values constraints only apply to map")
		}
		if items != nil {
			if items.Type != "" && !ValidVarType(items.Type) {
				return fmt.Errorf("unknown list item type %q", items.Type)
			}
			if err := validateConstraints(items.Type, *items, depth+1); err != nil {
				return err
			}
		}
	case models.VarTypeMap:
		if items != nil {
			return fmt.Errorf("items constraint only applies to list")
		}
		if keys != nil {
			if keys.Type != "" && !validMapKeyType(keys.Type) {
				return fmt.Errorf("map keys cannot be type %q", keys.Type)
			}
			if err := validateConstraints(keys.Type, *keys, depth+1); err != nil {
				return err
			}
		}
		if values != nil {
			if values.Type != "" && !ValidVarType(values.Type) {
				return fmt.Errorf("unknown map value type %q", values.Type)
			}
			if err := validateConstraints(values.Type, *values, depth+1); err != nil {
				return err
			}
		}
	default:
		if items != nil || keys != nil || values != nil {
			return fmt.Errorf("items/keys/values constraints only apply to list and map")
		}
	}
	return nil
}

// ValidateVariableDef checks type, constraints, and default_value.
func ValidateVariableDef(def *models.ConfigVariableDef) error {
	if def == nil {
		return fmt.Errorf("variable is required")
	}
	if !ValidVarType(def.Type) {
		return fmt.Errorf("invalid variable type")
	}
	if err := ValidateConstraints(def.Type, def.Constraints); err != nil {
		return err
	}
	if _, err := TypeCheckRaw(def, def.DefaultValue); err != nil {
		return err
	}
	return nil
}

// TypeCheck coerces and validates v against def. Type, constraints, and
// (for vlan) the 1–4094 range. List entries and map keys/values are checked
// recursively when constraints.items / keys / values are set.
func TypeCheck(def *models.ConfigVariableDef, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	c := parseConstraints(def.Constraints)
	c.Type = def.Type
	return typeCheck(def.Name, c, v, 0)
}

func typeCheck(name string, c varConstraints, v any, depth int) (any, error) {
	if v == nil {
		return nil, nil
	}
	if depth > maxTypeDepth {
		return nil, fmt.Errorf("%s: type nesting too deep", name)
	}
	typ := c.Type
	if typ == "" {
		return v, nil
	}
	if !ValidVarType(typ) {
		return nil, fmt.Errorf("%s: unknown type %q", name, typ)
	}
	switch typ {
	case models.VarTypeString, models.VarTypeSecret:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", name)
		}
		if c.Regex != "" {
			re, err := regexp.Compile(c.Regex)
			if err != nil {
				return nil, fmt.Errorf("%s: invalid constraint regex: %w", name, err)
			}
			if !re.MatchString(s) {
				return nil, fmt.Errorf("%s does not match required pattern", name)
			}
		}
		return s, nil
	case models.VarTypeInt, models.VarTypeInterfaceRef:
		n, ok := asInt(v)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer", name)
		}
		if c.Min != nil && float64(n) < *c.Min {
			return nil, fmt.Errorf("%s is below minimum", name)
		}
		if c.Max != nil && float64(n) > *c.Max {
			return nil, fmt.Errorf("%s is above maximum", name)
		}
		return n, nil
	case models.VarTypeVLAN:
		n, ok := asInt(v)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer VLAN id", name)
		}
		if n < 1 || n > 4094 {
			return nil, fmt.Errorf("%s: VLAN must be between 1 and 4094", name)
		}
		return int(n), nil
	case models.VarTypeBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be a boolean", name)
		}
		return b, nil
	case models.VarTypeEnum:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", name)
		}
		if len(c.Enum) == 0 {
			return s, nil
		}
		for _, e := range c.Enum {
			if e == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("%s is not one of the allowed values", name)
	case models.VarTypeIP:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be an IP address string", name)
		}
		if net.ParseIP(s) == nil {
			return nil, fmt.Errorf("%s is not a valid IP address", name)
		}
		return s, nil
	case models.VarTypePrefix:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a CIDR prefix string", name)
		}
		if _, err := netip.ParsePrefix(s); err != nil {
			return nil, fmt.Errorf("%s is not a valid prefix", name)
		}
		return s, nil
	case models.VarTypeList:
		return typeCheckList(name, c, v, depth)
	case models.VarTypeMap:
		return typeCheckMap(name, c, v, depth)
	default:
		return nil, fmt.Errorf("unknown variable type %q", typ)
	}
}

func applyMinMaxCount(name string, n int, c varConstraints) error {
	if c.Min != nil && float64(n) < *c.Min {
		return fmt.Errorf("%s has fewer than the minimum number of entries", name)
	}
	if c.Max != nil && float64(n) > *c.Max {
		return fmt.Errorf("%s has more than the maximum number of entries", name)
	}
	return nil
}

func typeCheckList(name string, c varConstraints, v any, depth int) (any, error) {
	items, ok := asList(v)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", name)
	}
	if err := applyMinMaxCount(name, len(items), c); err != nil {
		return nil, err
	}
	itemC := nestedConstraints(c.Items)
	if itemC == nil {
		return items, nil
	}
	out := make([]any, len(items))
	for i, item := range items {
		checked, err := typeCheck(fmt.Sprintf("%s[%d]", name, i), *itemC, item, depth+1)
		if err != nil {
			return nil, err
		}
		out[i] = checked
	}
	return out, nil
}

func typeCheckMap(name string, c varConstraints, v any, depth int) (any, error) {
	m, ok := asMap(v)
	if !ok {
		return nil, fmt.Errorf("%s must be a map", name)
	}
	if err := applyMinMaxCount(name, len(m), c); err != nil {
		return nil, err
	}
	keyC := nestedConstraints(c.Keys)
	valC := nestedConstraints(c.Values)
	if keyC == nil && valC == nil {
		return m, nil
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if keyC != nil {
			keyType := keyC.Type
			if keyType == "" {
				keyType = models.VarTypeString
			}
			if !validMapKeyType(keyType) {
				return nil, fmt.Errorf("%s: map keys cannot be type %q", name, keyType)
			}
			kc := *keyC
			kc.Type = keyType
			var keyVal any = k
			if keyType == models.VarTypeBool {
				switch strings.ToLower(k) {
				case "true":
					keyVal = true
				case "false":
					keyVal = false
				}
			}
			if _, err := typeCheck(fmt.Sprintf("%s key %q", name, k), kc, keyVal, depth+1); err != nil {
				return nil, err
			}
		}
		checked := val
		if valC != nil {
			var err error
			checked, err = typeCheck(fmt.Sprintf("%s[%q]", name, k), *valC, val, depth+1)
			if err != nil {
				return nil, err
			}
		}
		out[k] = checked
	}
	return out, nil
}

func TypeCheckRaw(def *models.ConfigVariableDef, raw json.RawMessage) (any, error) {
	v, err := jsonValue(raw)
	if err != nil {
		return nil, err
	}
	return TypeCheck(def, v)
}

func NormalizePlatform(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}
