package drivers

// Running-config contexts for the DCIM Configuration page.
//
// EOS and IOS-XR running-config are indented CLI. Every indented block
// is a context, at every depth. root holds the top-level commands that
// do not open a block (hostname, a one-line aaa statement, and the
// rest). interfaces groups each interface. Anything else with indented
// commands — radius-server, management api, router bgp, a vrf, an
// address-family — is its own node, and the same split applies inside
// it. A banner is a context too: EOS prints it as lines up to EOF, not
// as an indented block. A node's body is that context plus every
// context under it, indented the way that platform prints it (three
// spaces per level on EOS, one space on IOS-XR). Selecting a nested
// context shows that smaller piece.
//
// EOS can also return this tree as eAPI JSON (each command line is a
// key whose "cmds" object is the context under it). The page still
// reads EOS as CLI text, because that is the text the editor commits
// and because a banner is not an indented "cmds" block.
//
// Nokia SROS-MD has the same kind of JSON tree (admin show
// configuration json). The page builds contexts from that JSON and
// shows each one as MD-CLI, which is what edit-config accepts back.
// IOS-XR has no JSON running-config, so it is parsed from CLI text.

import (
	"fmt"
	"slices"
	"strings"
)

// ConfigContext is one node in the configuration tree sent to the GUI.
type ConfigContext struct {
	ID       string          `json:"id"`
	Label    string          `json:"label"`
	Enter    []string        `json:"enter"`
	Body     string          `json:"body"`
	Editable bool            `json:"editable"`
	Syntax   string          `json:"syntax,omitempty"`
	Children []ConfigContext `json:"children,omitempty"`
}

// RunningConfigPlatforms are the platform slugs the Configuration page
// can load. The device picker hides every other platform.
func RunningConfigPlatforms() []string {
	return []string{"eos", "ios-xr", "sros-md"}
}

// normalizeRunningPlatform folds case and the IOS-XR alias. SROS-MD
// stays sros-md; classic "sros" is a different CLI and is not edited here.
func normalizeRunningPlatform(platform string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	if p == "iosxr" {
		return "ios-xr"
	}
	return p
}

// RunningConfigSupported reports whether platform has a context builder
// and a commit transaction. Comparison is case-insensitive.
func RunningConfigSupported(platform string) bool {
	return slices.Contains(RunningConfigPlatforms(), normalizeRunningPlatform(platform))
}

// RunningConfigAsJSON reports whether the page should ask the driver for
// structured JSON instead of CLI text. Nokia's context tree is the JSON
// configuration. EOS and IOS-XR are read as CLI text.
func RunningConfigAsJSON(platform string) bool {
	return normalizeRunningPlatform(platform) == "sros-md"
}

// BuildRunningConfigTree parses a running-config into the context tree
// for platform. EOS and IOS-XR text is CLI ("show running-config").
// SROS-MD text is the JSON document from "admin show configuration json".
func BuildRunningConfigTree(platform, text string) ([]ConfigContext, error) {
	if !RunningConfigSupported(platform) {
		return nil, fmt.Errorf("running configuration is not supported for platform %s", platform)
	}
	switch normalizeRunningPlatform(platform) {
	case "eos":
		return buildEOSConfigTree(text), nil
	case "ios-xr":
		return buildIOSXRConfigTree(text), nil
	case "sros-md":
		return buildSROSConfigTree(text)
	default:
		return nil, fmt.Errorf("running configuration is not supported for platform %s", platform)
	}
}

// FindConfigContext returns the node with id, or nil.
func FindConfigContext(nodes []ConfigContext, id string) *ConfigContext {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if c := FindConfigContext(nodes[i].Children, id); c != nil {
			return c
		}
	}
	return nil
}

// NormalizeConfigText makes two editor snapshots comparable: CRLF to LF,
// trailing spaces stripped, one trailing newline when the text is not
// empty.
func NormalizeConfigText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// ContextCommit is the enter path and commands that turn node's body
// into after. A banner is rewritten whole (the lines, then EOF), or
// removed with "no banner …" when the new text is empty. Every other
// context uses a line diff inside the node's enter path. An empty
// command list means there is nothing to send.
func ContextCommit(node *ConfigContext, after string) (enter []string, commands []string) {
	if node != nil && node.Syntax == "md-cli" {
		return node.Enter, diffMDCLI(node.Body, after)
	}
	if node != nil && len(node.Enter) == 1 && strings.HasPrefix(node.Enter[0], "banner ") {
		body := NormalizeConfigText(after)
		if body == NormalizeConfigText(node.Body) {
			return node.Enter, nil
		}
		if body == "" {
			return nil, []string{"no " + node.Enter[0]}
		}
		lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
		return node.Enter, append(lines, "EOF")
	}
	enter = nil
	if node != nil {
		enter = node.Enter
	}
	before := ""
	if node != nil {
		before = node.Body
	}
	return enter, DiffConfigCommands(before, after)
}

// DiffConfigCommands returns the CLI that turns before into after inside
// an already-entered context. Removed lines become "no <line>" (a removed
// "no …" line becomes the positive command). Nested blocks are entered
// and exited. An empty result means the two texts describe the same tree.
func DiffConfigCommands(before, after string) []string {
	oldNodes := ParseConfigContext(strings.Split(NormalizeConfigText(before), "\n"), "!")
	newNodes := ParseConfigContext(strings.Split(NormalizeConfigText(after), "\n"), "!")
	return diffNodes(oldNodes, newNodes)
}

// UnifiedDiff is a unified diff of two context bodies. Empty when they
// normalize to the same text.
func UnifiedDiff(before, after string) string {
	a := diffLines(before)
	b := diffLines(after)
	if slices.Equal(a, b) {
		return ""
	}
	type op struct {
		kind byte
		line string
	}
	var ops []op
	i, j := 0, 0
	for _, p := range append(lcsLinePairs(a, b), [2]int{len(a), len(b)}) {
		for i < p[0] {
			ops = append(ops, op{kind: '-', line: a[i]})
			i++
		}
		for j < p[1] {
			ops = append(ops, op{kind: '+', line: b[j]})
			j++
		}
		if p[0] == len(a) {
			break
		}
		ops = append(ops, op{kind: ' ', line: a[i]})
		i++
		j++
	}

	const context = 3
	var bld strings.Builder
	bld.WriteString("--- before\n+++ after\n")
	for h := 0; h < len(ops); {
		for h < len(ops) && ops[h].kind == ' ' {
			h++
		}
		if h == len(ops) {
			break
		}
		start := h - context
		if start < 0 {
			start = 0
		}
		end := h + 1
		for end < len(ops) {
			next := end
			for next < len(ops) && ops[next].kind == ' ' {
				next++
			}
			if next == len(ops) || next-end > context*2 {
				break
			}
			end = next + 1
		}
		tail := end
		for tail < len(ops) && tail < end+context && ops[tail].kind == ' ' {
			tail++
		}
		oldStart, newStart := 1, 1
		oldCount, newCount := 0, 0
		for k := 0; k < start; k++ {
			if ops[k].kind != '+' {
				oldStart++
			}
			if ops[k].kind != '-' {
				newStart++
			}
		}
		for k := start; k < tail; k++ {
			if ops[k].kind != '+' {
				oldCount++
			}
			if ops[k].kind != '-' {
				newCount++
			}
		}
		fmt.Fprintf(&bld, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for k := start; k < tail; k++ {
			bld.WriteByte(ops[k].kind)
			bld.WriteByte(' ')
			bld.WriteString(ops[k].line)
			bld.WriteByte('\n')
		}
		h = tail
	}
	return bld.String()
}

func diffLines(s string) []string {
	s = NormalizeConfigText(s)
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func buildEOSConfigTree(text string) []ConfigContext {
	return buildIndentedConfigTree(text, "!", "   ", liftEOSBanners)
}

func buildIOSXRConfigTree(text string) []ConfigContext {
	return buildIndentedConfigTree(trimIOSXRConfig(text), "!", " ", nil)
}

// trimIOSXRConfig drops the SSH echo around "show running-config". The
// captured buffer starts with the command itself and, after the column-0
// end line, the prompt. The config between those is what the tree parses.
func trimIOSXRConfig(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	start := 0
	for start < len(lines) {
		switch strings.TrimSpace(lines[start]) {
		case "", "show running-config", "Building configuration...", "Building configuration ...":
			start++
			continue
		}
		break
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "end" && len(lines[i]) == len(strings.TrimLeft(lines[i], " ")) {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func buildIndentedConfigTree(text, comment, pad string, lift func([]string) ([]string, map[string]*ConfigContext)) []ConfigContext {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	banners := map[string]*ConfigContext{}
	if lift != nil {
		lines, banners = lift(lines)
	}
	nodes := ParseConfigContext(lines, comment)
	var rootLines, ifaces, contexts []*ConfigNode
	for _, n := range nodes {
		switch {
		case n == nil || n.Line == "" || n.Line == "end":
			continue
		case strings.HasPrefix(n.Line, "interface "):
			ifaces = append(ifaces, n)
		case banners[n.Line] != nil || len(n.Children) > 0:
			contexts = append(contexts, n)
		default:
			rootLines = append(rootLines, n)
		}
	}

	ifaceIDs := map[string]int{}
	ifaceNodes := make([]ConfigContext, 0, len(ifaces))
	for _, n := range ifaces {
		name := strings.TrimSpace(strings.TrimPrefix(n.Line, "interface "))
		id := uniqueContextID("if/"+name, ifaceIDs)
		ifaceNodes = append(ifaceNodes, contextNode(n, id, name, nil, pad))
	}
	out := []ConfigContext{
		{
			ID: "root", Label: "root", Enter: []string{},
			Body: renderNodeBody(rootLines, pad), Editable: true,
		},
		{
			ID: "interfaces", Label: "interfaces", Enter: []string{},
			Editable: false, Children: ifaceNodes,
		},
	}
	topIDs := map[string]int{}
	for _, n := range contexts {
		if pre := banners[n.Line]; pre != nil {
			id := uniqueContextID(eosContextPiece("", pre.Label), topIDs)
			pre.ID = id
			out = append(out, *pre)
			continue
		}
		id := uniqueContextID(eosContextPiece("", n.Line), topIDs)
		out = append(out, contextNode(n, id, n.Line, nil, pad))
	}
	return out
}

// liftEOSBanners pulls column-0 "banner …" blocks out of the line list.
// EOS prints the banner text and then a line that is exactly EOF; the
// text is not indented, so the indent parser would leave it in root.
// Each block is replaced by a sentinel line that buildEOSConfigTree
// turns back into the saved context. A banner with no EOF stays put.
func liftEOSBanners(lines []string) ([]string, map[string]*ConfigContext) {
	out := make([]string, 0, len(lines))
	banners := map[string]*ConfigContext{}
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimRight(lines[i], " \t")
		fields := strings.Fields(trimmed)
		indent := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
		if indent != 0 || len(fields) < 2 || fields[0] != "banner" {
			out = append(out, lines[i])
			continue
		}
		body, end, ok := readEOSBanner(lines, i)
		if !ok {
			out = append(out, lines[i])
			continue
		}
		key := fmt.Sprintf("\x00banner-%d", len(banners))
		label := strings.TrimSpace(trimmed)
		banners[key] = &ConfigContext{
			Label: label, Enter: []string{label},
			Body: NormalizeConfigText(strings.Join(body, "\n")), Editable: true,
		}
		out = append(out, key)
		i = end
	}
	return out, banners
}

func readEOSBanner(lines []string, at int) (body []string, end int, ok bool) {
	for j := at + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "EOF" {
			return body, j, true
		}
		body = append(body, strings.TrimRight(lines[j], " \t"))
	}
	return nil, 0, false
}

// contextNode is one indented block. Nested blocks become children, with
// the enter path extended by this line. The body is the whole block
// under this line, including those children, in file order.
func contextNode(n *ConfigNode, id, label string, enter []string, pad string) ConfigContext {
	path := append(append([]string{}, enter...), n.Line)
	_, children := splitChildContexts(n.Children, id, path, pad)
	return ConfigContext{
		ID: id, Label: label, Enter: path,
		Body:     renderNodeBody(n.Children, pad),
		Editable: true,
		Children: children,
	}
}

func splitChildContexts(nodes []*ConfigNode, parentID string, enter []string, pad string) (direct []*ConfigNode, children []ConfigContext) {
	used := map[string]int{}
	for _, n := range nodes {
		if n == nil || n.Line == "" {
			continue
		}
		if len(n.Children) == 0 {
			direct = append(direct, n)
			continue
		}
		id := uniqueContextID(eosContextPiece(parentID, n.Line), used)
		children = append(children, contextNode(n, id, n.Line, enter, pad))
	}
	return direct, children
}

func eosContextPiece(parent, line string) string {
	var piece string
	switch {
	case parent == "" && strings.HasPrefix(line, "interface "):
		piece = "if/" + strings.TrimSpace(strings.TrimPrefix(line, "interface "))
	case strings.HasPrefix(line, "router bgp "):
		piece = "bgp/" + strings.TrimSpace(strings.TrimPrefix(line, "router bgp "))
	case strings.HasPrefix(line, "router isis "):
		piece = "isis/" + strings.TrimSpace(strings.TrimPrefix(line, "router isis "))
	case strings.HasPrefix(line, "address-family "):
		slug := strings.TrimSpace(strings.TrimPrefix(line, "address-family "))
		piece = "af/" + strings.NewReplacer(" ", "-", "/", "-").Replace(slug)
	case strings.HasPrefix(line, "vrf "):
		piece = "vrf/" + strings.TrimSpace(strings.TrimPrefix(line, "vrf "))
	default:
		piece = strings.NewReplacer(" ", "-", "/", "-").Replace(strings.TrimSpace(line))
		if piece == "" {
			piece = "context"
		}
	}
	if parent == "" {
		return piece
	}
	return parent + "/" + piece
}

func uniqueContextID(id string, used map[string]int) string {
	used[id]++
	if used[id] == 1 {
		return id
	}
	return fmt.Sprintf("%s~%d", id, used[id])
}

// renderNodeBody prints nodes and their children. Each nested level is
// indented by pad (three spaces on EOS, one space on IOS-XR).
func renderNodeBody(nodes []*ConfigNode, padUnit string) string {
	var b strings.Builder
	writeConfigNodes(&b, nodes, 0, padUnit)
	return NormalizeConfigText(b.String())
}

func writeConfigNodes(b *strings.Builder, nodes []*ConfigNode, depth int, padUnit string) {
	pad := strings.Repeat(padUnit, depth)
	for _, n := range nodes {
		if n == nil || n.Line == "" {
			continue
		}
		b.WriteString(pad)
		b.WriteString(n.Line)
		b.WriteByte('\n')
		writeConfigNodes(b, n.Children, depth+1, padUnit)
	}
}

func diffNodes(old, new []*ConfigNode) []string {
	var cmds []string
	oi, ni := 0, 0
	for _, p := range append(lcsNodePairs(old, new), [2]int{len(old), len(new)}) {
		for oi < p[0] {
			cmds = append(cmds, negateLine(old[oi].Line))
			oi++
		}
		for ni < p[1] {
			cmds = append(cmds, flattenAdd(new[ni])...)
			ni++
		}
		if p[0] == len(old) {
			break
		}
		if child := diffNodes(old[oi].Children, new[ni].Children); len(child) > 0 {
			cmds = append(cmds, old[oi].Line)
			cmds = append(cmds, child...)
			cmds = append(cmds, "exit")
		}
		oi++
		ni++
	}
	return cmds
}

func negateLine(line string) string {
	if rest, ok := strings.CutPrefix(line, "no "); ok && strings.TrimSpace(rest) != "" {
		return strings.TrimSpace(rest)
	}
	return "no " + line
}

func flattenAdd(n *ConfigNode) []string {
	if n == nil || n.Line == "" {
		return nil
	}
	cmds := []string{n.Line}
	if len(n.Children) == 0 {
		return cmds
	}
	for _, c := range n.Children {
		cmds = append(cmds, flattenAdd(c)...)
	}
	cmds = append(cmds, "exit")
	return cmds
}

func lcsNodePairs(old, new []*ConfigNode) [][2]int {
	n, m := len(old), len(new)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if old[i].Line == new[j].Line {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var pairs [][2]int
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case old[i].Line == new[j].Line:
			pairs = append(pairs, [2]int{i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return pairs
}

func lcsLinePairs(a, b []string) [][2]int {
	old := make([]*ConfigNode, len(a))
	neu := make([]*ConfigNode, len(b))
	for i, line := range a {
		old[i] = &ConfigNode{Line: line}
	}
	for i, line := range b {
		neu[i] = &ConfigNode{Line: line}
	}
	return lcsNodePairs(old, neu)
}
