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
		models.ConfigScopeKindDevice, models.ConfigScopeKindInterface,
		models.ConfigScopeKindParameter, models.ConfigScopeKindCLI,
		models.ConfigScopeKindService, models.ConfigScopeKindServiceEndpoint,
		models.ConfigScopeKindResource:
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

var knownFieldTypes = map[string]bool{
	models.FieldTypeString:     true,
	models.FieldTypeInt:        true,
	models.FieldTypeBool:       true,
	models.FieldTypeEnum:       true,
	models.FieldTypeVLAN:       true,
	models.FieldTypeMAC:        true,
	models.FieldTypeSNPA:       true,
	models.FieldTypeIPv4:       true,
	models.FieldTypeIPv6:       true,
	models.FieldTypeIP:         true,
	models.FieldTypeIPv4Prefix: true,
	models.FieldTypeIPv6Prefix: true,
	models.FieldTypePrefix:     true,
	models.FieldTypeServiceID:  true,
	models.FieldTypeList:       true,
}

var fieldNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func ValidFieldType(t string) bool { return knownFieldTypes[t] }

func NormalizeFieldType(t string) string {
	t = strings.TrimSpace(t)
	if t == models.FieldTypeSNPA {
		return models.FieldTypeMAC
	}
	return t
}

func isPrefixFieldType(t string) bool {
	switch NormalizeFieldType(t) {
	case models.FieldTypePrefix, models.FieldTypeIPv4Prefix, models.FieldTypeIPv6Prefix:
		return true
	}
	return false
}

// FieldEmpty is the fill/required empty rule: missing, JSON null, "", and
// service_id 0. false and [] are present values.
func FieldEmpty(f models.FieldSchema, v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok && s == "" {
		return true
	}
	if NormalizeFieldType(f.Type) == models.FieldTypeServiceID {
		if n, ok := asInt(v); ok && n == 0 {
			return true
		}
	}
	return false
}

// ValidateServiceType checks schema and interfaces.fields (unique names,
// types, constraints) and rewrites snpa to mac.
func ValidateServiceType(st *models.ServiceType) error {
	if st == nil {
		return statusErr(400, "service type is required")
	}
	if err := ValidateInterfacesSpec(st.Interfaces); err != nil {
		return err
	}
	if err := validateNamedFields("schema", st.Schema); err != nil {
		return err
	}
	if err := validateNamedFields("interfaces.fields", st.Interfaces.Fields); err != nil {
		return err
	}
	return nil
}

func validateNamedFields(where string, fields []models.FieldSchema) error {
	seen := map[string]bool{}
	for i := range fields {
		f := &fields[i]
		if err := ValidateFieldSchema(f, true); err != nil {
			return err
		}
		if seen[f.Name] {
			return statusErrf(400, "duplicate %s field %q", where, f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

// ValidateFieldSchema checks one definition field. named is true for
// schema[] and interfaces.fields[]; list items are nameless.
func ValidateFieldSchema(f *models.FieldSchema, named bool) error {
	return validateFieldSchema(f, named, 0)
}

func validateFieldSchema(f *models.FieldSchema, named bool, depth int) error {
	if f == nil {
		return statusErr(400, "field schema is required")
	}
	if depth > maxTypeDepth {
		return statusErr(400, "type nesting too deep")
	}
	if named {
		if !fieldNameRe.MatchString(f.Name) {
			return statusErrf(400, "invalid field name %q", f.Name)
		}
	} else if f.Required {
		return statusErrf(400, "%s list items cannot set required", fieldLabel(*f))
	}
	if !ValidFieldType(f.Type) {
		return statusErrf(400, "unknown field type %q", f.Type)
	}
	f.Type = NormalizeFieldType(f.Type)

	switch f.Type {
	case models.FieldTypeInt, models.FieldTypeVLAN, models.FieldTypeList:
	default:
		if f.Min != nil || f.Max != nil {
			return statusErrf(400, "%s does not take min/max", fieldLabel(*f))
		}
	}
	min, max := f.Min, f.Max
	if f.Type == models.FieldTypeVLAN {
		min, max = vlanBounds(*f)
	}
	if min != nil && max != nil && *min > *max {
		return statusErrf(400, "%s min is greater than max", fieldLabel(*f))
	}
	if f.Type == models.FieldTypeVLAN {
		clampVLANBound(f.Min)
		clampVLANBound(f.Max)
	}
	if f.Unit != "" && f.Type != models.FieldTypeInt {
		return statusErrf(400, "%s does not take unit", fieldLabel(*f))
	}
	if (f.BoolTrueLabel != "" || f.BoolFalseLabel != "") && f.Type != models.FieldTypeBool {
		return statusErrf(400, "%s does not take bool labels", fieldLabel(*f))
	}
	if f.Resource != "" {
		if !isPrefixFieldType(f.Type) {
			return statusErrf(400, "%s cannot set resource", fieldLabel(*f))
		}
	}
	if len(f.Enum) > 0 && f.Type != models.FieldTypeEnum {
		return statusErrf(400, "%s does not take enum values", fieldLabel(*f))
	}

	switch f.Type {
	case models.FieldTypeEnum:
		if len(f.Enum) == 0 {
			return statusErrf(400, "%s enum requires at least one value", fieldLabel(*f))
		}
		seen := map[string]bool{}
		for _, e := range f.Enum {
			if e.Value == "" {
				return statusErrf(400, "%s enum value is required", fieldLabel(*f))
			}
			if seen[e.Value] {
				return statusErrf(400, "%s duplicate enum value %q", fieldLabel(*f), e.Value)
			}
			seen[e.Value] = true
		}
	case models.FieldTypeList:
		if f.Items == nil {
			return statusErrf(400, "%s list requires items", fieldLabel(*f))
		}
		if NormalizeFieldType(f.Items.Type) == models.FieldTypeList {
			return statusErrf(400, "%s nested lists are not allowed", fieldLabel(*f))
		}
		if err := validateFieldSchema(f.Items, false, depth+1); err != nil {
			return err
		}
	default:
		if f.Items != nil {
			return statusErrf(400, "%s does not take items", fieldLabel(*f))
		}
	}
	return validateFieldDefault(f, named)
}

func fieldLabel(f models.FieldSchema) string {
	if f.Name != "" {
		return f.Name
	}
	if f.Type != "" {
		return f.Type
	}
	return "field"
}

// ValidateServiceFields type-checks service-level fields against st.Schema
// and returns canonical JSON (MAC/prefix). Required uses FieldEmpty.
func ValidateServiceFields(st *models.ServiceType, raw json.RawMessage) (json.RawMessage, error) {
	if st == nil {
		return raw, nil
	}
	out, err := checkFields(st.Schema, fieldsMap(raw), "missing required field %q")
	if err != nil {
		if se := AsStatusError(err); se != nil {
			return nil, err
		}
		return nil, statusErr(400, err.Error())
	}
	if len(out) == 0 {
		if len(raw) == 0 || string(raw) == "null" {
			return raw, nil
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func checkFields(schema []models.FieldSchema, fields map[string]any, requiredFmt string) (map[string]any, error) {
	fields = ApplyFieldDefaults(schema, fields)
	out := copyFieldMap(fields)
	for _, f := range schema {
		v, ok := fields[f.Name]
		empty := !ok || FieldEmpty(f, v)
		if f.Required && empty {
			return nil, fmt.Errorf(requiredFmt, f.Name)
		}
		if empty {
			continue
		}
		checked, err := TypeCheckField(f, v)
		if err != nil {
			return nil, err
		}
		out[f.Name] = checked
	}
	return out, nil
}

func validateFieldDefault(f *models.FieldSchema, named bool) error {
	if len(f.Default) == 0 || string(f.Default) == "null" {
		f.Default = nil
		return nil
	}
	if !named {
		return statusErrf(400, "%s list items cannot set default", fieldLabel(*f))
	}
	var v any
	if err := json.Unmarshal(f.Default, &v); err != nil {
		return statusErrf(400, "%s default is not valid JSON", fieldLabel(*f))
	}
	checked, err := TypeCheckField(*f, v)
	if err != nil {
		return statusErr(400, err.Error())
	}
	if FieldEmpty(*f, checked) {
		return statusErrf(400, "%s default is empty", fieldLabel(*f))
	}
	b, err := json.Marshal(checked)
	if err != nil {
		return err
	}
	f.Default = b
	return nil
}

func fieldDefault(f models.FieldSchema) (any, bool) {
	if len(f.Default) == 0 || string(f.Default) == "null" {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(f.Default, &v); err != nil {
		return nil, false
	}
	checked, err := TypeCheckField(f, v)
	if err != nil || FieldEmpty(f, checked) {
		return nil, false
	}
	return checked, true
}

// ApplyFieldDefaults copies schema defaults into empty keys. Missing keys
// are created. Existing non-empty values are left unchanged.
func ApplyFieldDefaults(schema []models.FieldSchema, fields map[string]any) map[string]any {
	out := copyFieldMap(fields)
	for _, f := range schema {
		v, ok := out[f.Name]
		if ok && !FieldEmpty(f, v) {
			continue
		}
		dv, ok := fieldDefault(f)
		if !ok {
			continue
		}
		out[f.Name] = dv
	}
	return out
}

// TypeCheckField coerces and validates v against a definition field.
// MAC is stored as aabb.ccdd.eeff; prefixes as netip Prefix.Masked().
func TypeCheckField(f models.FieldSchema, v any) (any, error) {
	return typeCheckField(fieldLabel(f), f, v, 0)
}

func typeCheckField(name string, f models.FieldSchema, v any, depth int) (any, error) {
	if v == nil {
		return nil, nil
	}
	if depth > maxTypeDepth {
		return nil, fmt.Errorf("%s: type nesting too deep", name)
	}
	typ := NormalizeFieldType(f.Type)
	if typ == "" {
		return v, nil
	}
	if !ValidFieldType(typ) {
		return nil, fmt.Errorf("%s: unknown type %q", name, typ)
	}
	switch typ {
	case models.FieldTypeString:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", name)
		}
		return s, nil
	case models.FieldTypeInt:
		n, ok := asInt(v)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer", name)
		}
		if err := applyMinMaxValue(name, n, f.Min, f.Max); err != nil {
			return nil, err
		}
		return n, nil
	case models.FieldTypeVLAN:
		n, ok := asInt(v)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer VLAN id", name)
		}
		min, max := vlanBounds(f)
		if err := applyMinMaxValue(name, n, min, max); err != nil {
			return nil, err
		}
		return int(n), nil
	case models.FieldTypeBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be a boolean", name)
		}
		return b, nil
	case models.FieldTypeEnum:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", name)
		}
		if len(f.Enum) == 0 {
			return s, nil
		}
		for _, e := range f.Enum {
			if e.Value == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("%s is not one of the allowed values", name)
	case models.FieldTypeMAC:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a MAC address string", name)
		}
		canon, err := canonicalMAC(s)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid MAC address", name)
		}
		return canon, nil
	case models.FieldTypeIPv4, models.FieldTypeIPv6, models.FieldTypeIP:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be an IP address string", name)
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid IP address", name)
		}
		if typ == models.FieldTypeIPv4 && !addr.Is4() {
			return nil, fmt.Errorf("%s must be an IPv4 address", name)
		}
		if typ == models.FieldTypeIPv6 && !addr.Is6() {
			return nil, fmt.Errorf("%s must be an IPv6 address", name)
		}
		return addr.String(), nil
	case models.FieldTypeIPv4Prefix, models.FieldTypeIPv6Prefix, models.FieldTypePrefix:
		s, ok := asString(v)
		if !ok {
			return nil, fmt.Errorf("%s must be a CIDR prefix string", name)
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid prefix", name)
		}
		p = p.Masked()
		if typ == models.FieldTypeIPv4Prefix && !p.Addr().Is4() {
			return nil, fmt.Errorf("%s must be an IPv4 prefix", name)
		}
		if typ == models.FieldTypeIPv6Prefix && !p.Addr().Is6() {
			return nil, fmt.Errorf("%s must be an IPv6 prefix", name)
		}
		return p.String(), nil
	case models.FieldTypeServiceID:
		n, ok := asInt(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("%s must be a service id", name)
		}
		return uint64(n), nil
	case models.FieldTypeList:
		return typeCheckFieldList(name, f, v, depth)
	default:
		return nil, fmt.Errorf("%s: unknown type %q", name, typ)
	}
}

func vlanBounds(f models.FieldSchema) (min, max *float64) {
	one, four := 1.0, 4094.0
	min, max = &one, &four
	if f.Min != nil {
		min = f.Min
	}
	if f.Max != nil {
		max = f.Max
	}
	return min, max
}

func clampVLANBound(v *float64) {
	if v == nil {
		return
	}
	if *v < 1 {
		*v = 1
	}
	if *v > 4094 {
		*v = 4094
	}
}

func applyMinMaxValue(name string, n int64, min, max *float64) error {
	if min != nil && float64(n) < *min {
		return fmt.Errorf("%s is below minimum", name)
	}
	if max != nil && float64(n) > *max {
		return fmt.Errorf("%s is above maximum", name)
	}
	return nil
}

func typeCheckFieldList(name string, f models.FieldSchema, v any, depth int) (any, error) {
	items, ok := asList(v)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", name)
	}
	c := varConstraints{Min: f.Min, Max: f.Max}
	if err := applyMinMaxCount(name, len(items), c); err != nil {
		return nil, err
	}
	if f.Items == nil {
		return items, nil
	}
	out := make([]any, len(items))
	for i, item := range items {
		itemName := fmt.Sprintf("%s[%d]", name, i)
		if FieldEmpty(*f.Items, item) {
			return nil, fmt.Errorf("%s must not be empty", itemName)
		}
		checked, err := typeCheckField(itemName, *f.Items, item, depth+1)
		if err != nil {
			return nil, err
		}
		out[i] = checked
	}
	return out, nil
}

func canonicalMAC(s string) (string, error) {
	hex := make([]byte, 0, 12)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ':' || c == '-' || c == '.':
			continue
		case c >= 'A' && c <= 'F':
			hex = append(hex, c+'a'-'A')
		case c >= 'a' && c <= 'f' || c >= '0' && c <= '9':
			hex = append(hex, c)
		default:
			return "", fmt.Errorf("invalid MAC")
		}
	}
	if len(hex) != 12 {
		return "", fmt.Errorf("invalid MAC")
	}
	return string(hex[0:4]) + "." + string(hex[4:8]) + "." + string(hex[8:12]), nil
}
