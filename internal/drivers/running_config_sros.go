package drivers

// SROS-MD contexts come from "admin show configuration json". Each object
// is a context and each list is a folder of contexts, the same way EOS
// eAPI nests a context under a command line. The editor shows that
// context as MD-CLI (the text edit-config accepts). IOS-XR has no JSON
// running-config and does not use this file.
//
// persistent-indices is bookkeeping beside the configure tree. It is not
// a configure context, so it is left out of the page.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	jsonKindNull = iota
	jsonKindBool
	jsonKindNumber
	jsonKindString
	jsonKindArray
	jsonKindObject
)

// jsonVal is one JSON value with object keys kept in document order.
type jsonVal struct {
	kind    int
	literal string
	keys    []string
	vals    map[string]jsonVal
	arr     []jsonVal
}

func extractJSONObject(text string) (string, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("no JSON object found in output")
	}
	return text[start : end+1], nil
}

func parseJSONVal(raw []byte) (jsonVal, error) {
	return parseRaw(json.RawMessage(bytes.TrimSpace(raw)))
}

func parseRaw(raw json.RawMessage) (jsonVal, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return jsonVal{}, fmt.Errorf("empty JSON value")
	}
	switch raw[0] {
	case '{':
		keys, err := jsonObjectKeys(raw)
		if err != nil {
			return jsonVal{}, err
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return jsonVal{}, err
		}
		vals := make(map[string]jsonVal, len(keys))
		for _, k := range keys {
			child, err := parseRaw(obj[k])
			if err != nil {
				return jsonVal{}, err
			}
			vals[k] = child
		}
		return jsonVal{kind: jsonKindObject, keys: keys, vals: vals}, nil
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return jsonVal{}, err
		}
		out := make([]jsonVal, 0, len(arr))
		for _, el := range arr {
			child, err := parseRaw(el)
			if err != nil {
				return jsonVal{}, err
			}
			out = append(out, child)
		}
		return jsonVal{kind: jsonKindArray, arr: out}, nil
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return jsonVal{}, err
		}
		return jsonVal{kind: jsonKindString, literal: s}, nil
	case 't', 'f':
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return jsonVal{}, err
		}
		if b {
			return jsonVal{kind: jsonKindBool, literal: "true"}, nil
		}
		return jsonVal{kind: jsonKindBool, literal: "false"}, nil
	case 'n':
		return jsonVal{kind: jsonKindNull}, nil
	default:
		return jsonVal{kind: jsonKindNumber, literal: string(raw)}, nil
	}
}

func buildSROSConfigTree(text string) ([]ConfigContext, error) {
	raw, err := extractJSONObject(text)
	if err != nil {
		return nil, fmt.Errorf("sros-md configuration: %w", err)
	}
	root, err := parseJSONVal([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("sros-md configuration: %w", err)
	}
	cfg := srosConfigureObject(root)
	if cfg.kind != jsonKindObject {
		return nil, fmt.Errorf("sros-md configuration JSON has no configure object")
	}
	return srosConfigureTree(cfg), nil
}

func srosConfigureObject(root jsonVal) jsonVal {
	if root.kind != jsonKindObject {
		return jsonVal{}
	}
	if v, ok := root.vals["nokia-conf:configure"]; ok {
		return v
	}
	if v, ok := root.vals["configure"]; ok && len(root.keys) == 1 {
		return v
	}
	return root
}

func srosConfigureTree(cfg jsonVal) []ConfigContext {
	out := []ConfigContext{{
		ID: "root", Label: "root", Enter: []string{"/configure"},
		Body: renderMD(srosScalarNodes(cfg, nil)), Editable: true, Syntax: "md-cli",
	}}
	used := map[string]int{}
	for _, b := range srosChildren(cfg, nil) {
		out = append(out, srosBlockContext(b, "/configure", "", used))
	}
	return out
}

type srosBlock struct {
	name  string
	list  bool
	item  bool
	label string
	head  string
	obj   jsonVal
	omit  map[string]bool
}

func srosScalarNodes(obj jsonVal, omit map[string]bool) []*mdNode {
	var nodes []*mdNode
	for _, k := range obj.keys {
		if omit[k] {
			continue
		}
		v := obj.vals[k]
		if v.kind == jsonKindArray && jsonAllScalar(v.arr) {
			nodes = append(nodes, srosValueNodes(k, v)...)
		}
		if jsonIsScalar(v) {
			nodes = append(nodes, srosValueNodes(k, v)...)
		}
	}
	return nodes
}

func srosChildren(obj jsonVal, omit map[string]bool) []srosBlock {
	var blocks []srosBlock
	for _, k := range obj.keys {
		if omit[k] {
			continue
		}
		v := obj.vals[k]
		switch v.kind {
		case jsonKindObject:
			blocks = append(blocks, srosBlock{name: k, label: k, head: k, obj: v})
		case jsonKindArray:
			if len(v.arr) == 0 || jsonAllScalar(v.arr) {
				continue
			}
			blocks = append(blocks, srosBlock{name: k, list: true, label: k, head: k, obj: v})
		}
	}
	return blocks
}

func srosValueNodes(k string, v jsonVal) []*mdNode {
	switch v.kind {
	case jsonKindObject:
		return []*mdNode{{line: k, block: true, children: srosBodyNodes(v, nil)}}
	case jsonKindArray:
		if len(v.arr) == 0 {
			return nil
		}
		if jsonAllScalar(v.arr) {
			if line := mdScalarList(k, v.arr); line != "" {
				return []*mdNode{{line: line}}
			}
			return nil
		}
		var items []*mdNode
		for _, el := range v.arr {
			if el.kind != jsonKindObject {
				continue
			}
			keys := listKeyFields(el)
			_, head := listItemHead(k, el, keys)
			items = append(items, &mdNode{line: head, block: true, children: srosBodyNodes(el, keySet(keys))})
		}
		return items
	case jsonKindNull:
		return nil
	default:
		if s, ok := mdScalarText(v); ok {
			return []*mdNode{{line: k + " " + s}}
		}
		return nil
	}
}

func srosBodyNodes(obj jsonVal, omit map[string]bool) []*mdNode {
	var nodes []*mdNode
	for _, k := range obj.keys {
		if omit[k] {
			continue
		}
		nodes = append(nodes, srosValueNodes(k, obj.vals[k])...)
	}
	return nodes
}

func srosBlockContext(b srosBlock, parentEnter, parentID string, used map[string]int) ConfigContext {
	if b.list {
		id := uniqueContextID(srosContextID(parentID, b.label), used)
		var children []ConfigContext
		itemUsed := map[string]int{}
		for _, el := range b.obj.arr {
			if el.kind != jsonKindObject {
				continue
			}
			keys := listKeyFields(el)
			label, head := listItemHead(b.name, el, keys)
			children = append(children, srosBlockContext(srosBlock{
				name: b.name, item: true, label: label, head: head, obj: el,
				omit: keySet(keys),
			}, parentEnter, id, itemUsed))
		}
		return ConfigContext{ID: id, Label: b.label, Editable: false, Children: children}
	}
	enter := appendMDEnter(parentEnter, b.head)
	id := uniqueContextID(srosContextID(parentID, b.label), used)
	childUsed := map[string]int{}
	var children []ConfigContext
	for _, n := range srosChildren(b.obj, b.omit) {
		children = append(children, srosBlockContext(n, enter, id, childUsed))
	}
	return ConfigContext{
		ID: id, Label: b.label, Enter: []string{enter},
		Body: renderMD(srosBodyNodes(b.obj, b.omit)), Editable: true, Syntax: "md-cli",
		Children: children,
	}
}

func appendMDEnter(parent, piece string) string {
	if parent == "" {
		return piece
	}
	return parent + " " + piece
}

func srosContextID(parent, label string) string {
	var b strings.Builder
	for _, r := range label {
		switch r {
		case ' ', '/', '"', '{', '}', '\\':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	piece := strings.Trim(b.String(), "-")
	if piece == "" {
		piece = "context"
	}
	if parent == "" {
		return piece
	}
	return parent + "/" + piece
}

func keySet(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func listKeyFields(obj jsonVal) []string {
	for _, k := range obj.keys {
		if isListKeyName(k) && jsonIsScalar(obj.vals[k]) {
			return []string{k}
		}
	}
	for _, k := range obj.keys {
		if jsonIsScalar(obj.vals[k]) {
			return []string{k}
		}
	}
	return nil
}

func isListKeyName(k string) bool {
	return k == "name" || k == "id" ||
		strings.HasSuffix(k, "-id") ||
		strings.HasSuffix(k, "-name") ||
		strings.HasSuffix(k, "-instance")
}

func listItemHead(listName string, obj jsonVal, keys []string) (label, head string) {
	var labels, parts []string
	for _, k := range keys {
		s, ok := mdScalarText(obj.vals[k])
		if !ok {
			continue
		}
		labels = append(labels, obj.vals[k].literal)
		parts = append(parts, s)
	}
	if len(labels) == 0 {
		return listName, listName
	}
	return strings.Join(labels, " "), listName + " " + strings.Join(parts, " ")
}

func jsonIsScalar(v jsonVal) bool {
	switch v.kind {
	case jsonKindBool, jsonKindNumber, jsonKindString:
		return true
	default:
		return false
	}
}

func jsonAllScalar(arr []jsonVal) bool {
	for _, v := range arr {
		if !jsonIsScalar(v) {
			return false
		}
	}
	return true
}

var mdToken = regexp.MustCompile(`^[A-Za-z0-9_.:/+-]+$`)

func mdQuote(s string) string {
	if s != "" && mdToken.MatchString(s) {
		return s
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func mdScalarText(v jsonVal) (string, bool) {
	switch v.kind {
	case jsonKindString:
		return mdQuote(v.literal), true
	case jsonKindNumber, jsonKindBool:
		if v.literal == "" {
			return "", false
		}
		return v.literal, true
	default:
		return "", false
	}
}

func mdScalarList(key string, arr []jsonVal) string {
	parts := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := mdScalarText(v)
		if !ok {
			return ""
		}
		parts = append(parts, s)
	}
	return key + " [" + strings.Join(parts, " ") + "]"
}

// mdNode is one MD-CLI statement. A block is "line { ... }".
type mdNode struct {
	line     string
	block    bool
	children []*mdNode
}

func renderMD(nodes []*mdNode) string {
	var b strings.Builder
	writeMD(&b, nodes, 0)
	return NormalizeConfigText(b.String())
}

func writeMD(b *strings.Builder, nodes []*mdNode, depth int) {
	pad := strings.Repeat("    ", depth)
	for _, n := range nodes {
		if n == nil || n.line == "" && !n.block {
			continue
		}
		if !n.block && len(n.children) > 0 {
			writeMD(b, n.children, depth)
			continue
		}
		if !n.block {
			b.WriteString(pad)
			b.WriteString(n.line)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(pad)
		b.WriteString(n.line)
		b.WriteString(" {\n")
		writeMD(b, n.children, depth+1)
		b.WriteString(pad)
		b.WriteString("}\n")
	}
}

func diffMDCLI(before, after string) []string {
	return diffMD(parseMDBody(before), parseMDBody(after))
}

func parseMDBody(text string) []*mdNode {
	norm := NormalizeConfigText(text)
	if norm == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(norm, "\n"), "\n")
	pos := 0
	var parse func() []*mdNode
	parse = func() []*mdNode {
		var nodes []*mdNode
		for pos < len(lines) {
			line := strings.TrimSpace(lines[pos])
			if line == "" || strings.HasPrefix(line, "#") {
				pos++
				continue
			}
			if line == "}" {
				return nodes
			}
			pos++
			if head, ok := mdBlockOpen(line); ok {
				children := parse()
				if pos < len(lines) && strings.TrimSpace(lines[pos]) == "}" {
					pos++
				}
				nodes = append(nodes, &mdNode{line: head, block: true, children: children})
				continue
			}
			nodes = append(nodes, &mdNode{line: line})
		}
		return nodes
	}
	return parse()
}

func mdBlockOpen(line string) (string, bool) {
	inQuote := false
	esc := false
	last := -1
	for i := 0; i < len(line); i++ {
		c := line[i]
		if esc {
			esc = false
			continue
		}
		if c == '\\' && inQuote {
			esc = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && c != ' ' && c != '\t' {
			last = i
		}
	}
	if last < 0 || line[last] != '{' {
		return "", false
	}
	return strings.TrimSpace(line[:last]), true
}

func diffMD(old, neu []*mdNode) []string {
	var cmds []string
	oi, ni := 0, 0
	for _, p := range append(lcsMD(old, neu), [2]int{len(old), len(neu)}) {
		for oi < p[0] {
			cmds = append(cmds, mdDelete(old[oi]))
			oi++
		}
		for ni < p[1] {
			cmds = append(cmds, flattenMD(neu[ni])...)
			ni++
		}
		if p[0] == len(old) {
			break
		}
		if child := diffMD(old[oi].children, neu[ni].children); len(child) > 0 {
			cmds = append(cmds, old[oi].line)
			cmds = append(cmds, child...)
			cmds = append(cmds, "exit")
		}
		oi++
		ni++
	}
	return cmds
}

func lcsMD(old, neu []*mdNode) [][2]int {
	a := make([]*ConfigNode, len(old))
	b := make([]*ConfigNode, len(neu))
	for i, n := range old {
		a[i] = &ConfigNode{Line: n.line}
	}
	for i, n := range neu {
		b[i] = &ConfigNode{Line: n.line}
	}
	return lcsNodePairs(a, b)
}

func mdDelete(n *mdNode) string {
	if n.block {
		return "delete " + n.line
	}
	return "delete " + mdFirstToken(n.line)
}

func mdFirstToken(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return line
	}
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		return line[:i]
	}
	return line
}

func flattenMD(n *mdNode) []string {
	if n == nil {
		return nil
	}
	if !n.block {
		if n.line == "" {
			return nil
		}
		return []string{n.line}
	}
	cmds := []string{n.line + " {"}
	for _, c := range n.children {
		cmds = append(cmds, flattenMD(c)...)
	}
	cmds = append(cmds, "}")
	return cmds
}
