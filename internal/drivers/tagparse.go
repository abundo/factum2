package drivers

// Generic hierarchical config parser: fills a `cfg:"..."` tagged struct
// straight from a *ConfigNode tree (config_context.go's ParseConfigContext),
// as a declarative alternative to the hand-written package-level regexp +
// switch-statement parsing style used throughout driver_iosxr.go/
// driver_vrp.go. Line matching, optional-argument handling and leaf-type
// conversion are implemented once here instead of once per platform.
//
// Tag DSL, space-separated tokens matched against a config line's own
// whitespace-split words:
//   - a bare word is a literal that must match verbatim (case-insensitive)
//   - "{}" captures a single word
//   - "{:toend}" captures the remainder of the line as one string
//   - "[word]" or "[several words]" is an optional literal phrase (spaces
//     allowed inside the brackets) - present or absent, matched greedily
//     against that many consecutive words; either way the line still
//     matches
//
// A field's Go type decides how it's filled:
//   - map[string]T / *map[string]T (T a struct) - the tag's first capture
//     becomes the key, T is filled from the matched line's children
//   - T / *T (T a struct, not implementing encoding.TextUnmarshaler) -
//     filled from the matched line's children, no key capture
//   - []T - appended to, one element per matching line (each following the
//     rule above for T)
//   - string/int/uint/bool, or any type implementing
//     encoding.TextUnmarshaler - a leaf, filled from the tag's captures
//     joined with a single space (so a tag with two "{}" captures, e.g. an
//     IPv4 address and a separate dotted-decimal mask on the same line,
//     can still be parsed by one UnmarshalText call). A bool leaf with no
//     capture in its tag is a presence flag: the line existing sets it
//     true, absence leaves it false - see L2Transport's use below.
//
// A "[word]" that matched is injected as a synthetic child line (just the
// word, e.g. "l2transport") ahead of the real children when recursing into
// a map/struct field, so a nested field can pick it up with an ordinary
// bare-literal tag like `cfg:"l2transport"` - see injectOptionals. This is
// what lets "interface X l2transport" (IOS-XR) / "interface X mode l2"
// (VRP) - the L2/L3 subinterface distinction sitting on the interface line
// itself rather than an indented child - bind into Interface.L2Transport
// without any special-casing in the engine.
//
// Candidate tags on a struct are tried most-specific-first (more literal
// tokens win), not in field declaration order, so e.g. a hypothetical
// `cfg:"ip address dhcp"` would be preferred over a more general
// `cfg:"ip address {}"` regardless of which field comes first in the
// struct - see specificity.

import (
	"cmp"
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const cfgTagName = "cfg"

// sortedKeys returns m's keys in sorted order. Map fields are the natural
// fill target for a repeated block keyed by name (interfaces, VRFs,
// xconnect groups, ...), but Go's map iteration order is randomized -
// callers that need to walk one and build a slice (dc.Interfaces, ELINE/
// ELAN assembly, ...) should go through this instead of `for range m`
// directly, so GetDeviceConfig's output - and anything that diffs or logs
// it - is reproducible across runs rather than shuffled per process.
func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// UnmarshalNodes fills target (a pointer to a struct) from nodes - a
// top-level *ConfigNode slice, e.g. ParseConfigContext's return value, or
// any node's own Children for a partial/scoped parse.
func UnmarshalNodes(nodes []*ConfigNode, target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("UnmarshalNodes: target must be a pointer to a struct, got %T", target)
	}
	unmarshalStruct(nodes, v.Elem())
	return nil
}

// ----------------------------------------------------------------------
// Tag DSL
// ----------------------------------------------------------------------

type tagToken struct {
	literal  string
	capture  bool
	toEnd    bool
	optional bool
}

// parseCfgTag tokenizes on whitespace like strings.Fields, except inside a
// "[...]" group - which may itself contain spaces, e.g. "[mode l2]" for
// VRP's two-word L2 sub-interface marker - so that case has to be handled
// by hand rather than via strings.Fields.
func parseCfgTag(tag string) []tagToken {
	var tokens []tagToken
	i := 0
	for i < len(tag) {
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		if i >= len(tag) {
			break
		}
		if tag[i] == '[' {
			end := strings.IndexByte(tag[i:], ']')
			if end == -1 {
				end = len(tag) - i - 1 // malformed tag - treat the rest as the group
			}
			tokens = append(tokens, tagToken{literal: tag[i+1 : i+end], optional: true})
			i += end + 1
			continue
		}
		start := i
		for i < len(tag) && tag[i] != ' ' {
			i++
		}
		switch word := tag[start:i]; word {
		case "{}":
			tokens = append(tokens, tagToken{capture: true})
		case "{:toend}":
			tokens = append(tokens, tagToken{capture: true, toEnd: true})
		default:
			tokens = append(tokens, tagToken{literal: word})
		}
	}
	return tokens
}

// specificity ranks tags so more specific patterns (more literal tokens,
// required or optional) are tried before more general ones.
func specificity(tokens []tagToken) int {
	score := 0
	for _, tt := range tokens {
		if tt.literal != "" {
			score += 2
		} else {
			score++
		}
	}
	return score
}

// matchLine walks tagTokens against a line's whitespace-split words. It
// only succeeds if the whole line is consumed, so e.g. a `cfg:"ip
// address"` tag can't accidentally match an "ip address-family ..." line.
// Returns the ordered {}/{:toend} captures, plus the optional phrases
// (from "[word]"/"[multi word]" tokens) that were actually present, in tag
// order.
func matchLine(tagTokens []tagToken, lineWords []string) (captures, optionals []string, ok bool) {
	li := 0
	for _, tt := range tagTokens {
		switch {
		case tt.toEnd:
			if li > len(lineWords) {
				return nil, nil, false
			}
			captures = append(captures, strings.Join(lineWords[li:], " "))
			li = len(lineWords)

		case tt.optional:
			// tt.literal may itself be several words (e.g. "mode l2") - all
			// of them must match consecutively for the phrase to count as
			// present.
			words := strings.Fields(tt.literal)
			if li+len(words) <= len(lineWords) {
				matched := true
				for j, w := range words {
					if !strings.EqualFold(w, lineWords[li+j]) {
						matched = false
						break
					}
				}
				if matched {
					optionals = append(optionals, tt.literal)
					li += len(words)
				}
			}

		case tt.capture:
			if li >= len(lineWords) {
				return nil, nil, false
			}
			captures = append(captures, lineWords[li])
			li++

		default:
			if li >= len(lineWords) || !strings.EqualFold(tt.literal, lineWords[li]) {
				return nil, nil, false
			}
			li++
		}
	}
	if li != len(lineWords) {
		return nil, nil, false
	}
	return captures, optionals, true
}

// ----------------------------------------------------------------------
// Reflection engine
// ----------------------------------------------------------------------

type fieldMatcher struct {
	index int
	tag   []tagToken
}

// unmarshalStruct fills v's tagged fields from nodes. Best-effort, same
// convention as every hand-written Parse* function it replaces: a line
// whose value fails to convert (bad int, bad IP literal, ...) is skipped
// rather than aborting the rest of the tree - one malformed/unexpected
// line from an exotic firmware version shouldn't lose everything else in
// the config.
func unmarshalStruct(nodes []*ConfigNode, v reflect.Value) {
	t := v.Type()

	var matchers []fieldMatcher
	for i := 0; i < t.NumField(); i++ {
		tagStr, ok := t.Field(i).Tag.Lookup(cfgTagName)
		if !ok {
			continue
		}
		matchers = append(matchers, fieldMatcher{index: i, tag: parseCfgTag(tagStr)})
	}
	sort.SliceStable(matchers, func(i, j int) bool {
		return specificity(matchers[i].tag) > specificity(matchers[j].tag)
	})

	for _, node := range nodes {
		words := strings.Fields(node.Line)
		for _, m := range matchers {
			captures, optionals, ok := matchLine(m.tag, words)
			if !ok {
				continue
			}
			children := injectOptionals(node.Children, optionals)
			_ = assignField(v.Field(m.index), captures, children)
			break
		}
		// A line matching no tag is unmodeled config - ignored, same as
		// the hand-written switch-statement parsers' default case, so the
		// struct only needs to declare the lines a caller actually uses.
	}
}

// injectOptionals makes "[word]" literals that were present on a line
// (e.g. "l2transport" trailing "interface X") visible to the nested
// struct as if they were ordinary indented child lines.
func injectOptionals(children []*ConfigNode, optionals []string) []*ConfigNode {
	if len(optionals) == 0 {
		return children
	}
	synthetic := make([]*ConfigNode, len(optionals))
	for i, word := range optionals {
		synthetic[i] = &ConfigNode{Line: word}
	}
	return append(synthetic, children...)
}

var textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

// assignField dispatches on fv's kind and fills it from either captures
// (leaf values) or children (nested maps/structs/slices).
func assignField(fv reflect.Value, captures []string, children []*ConfigNode) error {
	ft := fv.Type()

	if ft.Kind() == reflect.Ptr {
		if fv.IsNil() {
			fv.Set(reflect.New(ft.Elem()))
		}
		return assignField(fv.Elem(), captures, children)
	}

	// Any type providing its own text parsing (netip.Prefix/Addr, or a
	// small custom type combining several captures - see iosxrIPv4Prefix)
	// is a leaf, regardless of whether it's a struct under the hood.
	if fv.CanAddr() && fv.Addr().Type().Implements(textUnmarshalerType) {
		if len(captures) == 0 {
			return fmt.Errorf("no value captured for %s", ft)
		}
		return fv.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(strings.Join(captures, " ")))
	}

	switch ft.Kind() {
	case reflect.Map:
		if len(captures) == 0 {
			return fmt.Errorf("map field %s needs a capture for its key", ft)
		}
		if fv.IsNil() {
			fv.Set(reflect.MakeMap(ft))
		}
		key := captures[0]
		elem := reflect.New(ft.Elem())
		unmarshalStruct(children, elem.Elem())
		fv.SetMapIndex(reflect.ValueOf(key), elem.Elem())
		return nil

	case reflect.Slice:
		elem := reflect.New(ft.Elem())
		if err := assignField(elem.Elem(), captures, children); err != nil {
			return err
		}
		fv.Set(reflect.Append(fv, elem.Elem()))
		return nil

	case reflect.Struct:
		unmarshalStruct(children, fv)
		return nil

	default:
		return assignPrimitive(fv, captures)
	}
}

func assignPrimitive(fv reflect.Value, captures []string) error {
	var value string
	if len(captures) > 0 {
		value = strings.Join(captures, " ")
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing int %q: %w", value, err)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing uint %q: %w", value, err)
		}
		fv.SetUint(n)
	case reflect.Bool:
		// A bool leaf's tag typically has no capture (e.g. `cfg:"control-word"`)
		// - the line's mere presence means true.
		fv.SetBool(true)
	default:
		return fmt.Errorf("unsupported field kind %s (add an UnmarshalText method for custom types)", fv.Kind())
	}
	return nil
}
