package drivers

// Hierarchical (indentation-based) config-context parser, for platforms
// whose running config is CLI text organized by indentation (IOS-XR, VRP).
// EOS and SR OS don't need this: both return running config as JSON, which
// already has the tree structure.

import (
	"strconv"
	"strings"
)

// ConfigNode is one line of config plus whatever's indented under it.
type ConfigNode struct {
	Line     string
	Children []*ConfigNode
}

// ParseConfigContext splits lines into a tree by leading-whitespace
// indentation: a line indented more than the line above it becomes that
// line's child, less-or-equal indentation ends the current context. Blank
// lines and lines starting with commentPrefix (once commentPrefix != "")
// are skipped entirely - they neither start nor end a context.
func ParseConfigContext(lines []string, commentPrefix string) []*ConfigNode {
	type indentedLine struct {
		line   string
		indent int
	}
	trimmed := make([]indentedLine, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimLeft(raw, " ")
		if line == "" {
			continue
		}
		if commentPrefix != "" && strings.HasPrefix(line, commentPrefix) {
			continue
		}
		trimmed = append(trimmed, indentedLine{line: line, indent: len(raw) - len(line)})
	}

	pos := 0
	var parse func(indent int) []*ConfigNode
	parse = func(indent int) []*ConfigNode {
		var nodes []*ConfigNode
		for pos < len(trimmed) {
			cur := trimmed[pos]
			if cur.indent < indent {
				return nodes
			}
			pos++
			node := &ConfigNode{Line: cur.line}
			if pos < len(trimmed) && trimmed[pos].indent > cur.indent {
				node.Children = parse(trimmed[pos].indent)
			}
			nodes = append(nodes, node)
		}
		return nodes
	}
	return parse(0)
}

// fmtSscanInt best-effort parses s as a decimal integer into *dst, leaving
// *dst unchanged (zero) on a non-numeric value - used for config fields a
// device might render with a non-numeric suffix or garbage value that
// should just be ignored rather than failing the whole parse.
func fmtSscanInt(s string, dst *int) {
	if v, err := strconv.Atoi(s); err == nil {
		*dst = v
	}
}

// GetContextLevel walks down path, each step matching the first child whose
// Line has that step as a prefix (startswith-based). Returns nil if any
// step isn't found.
func GetContextLevel(nodes []*ConfigNode, path ...string) []*ConfigNode {
	current := nodes
	for _, step := range path {
		node := FindChild(current, step)
		if node == nil {
			return nil
		}
		current = node.Children
	}
	return current
}

// FindChild returns the first node in nodes whose Line has prefix as a
// prefix, or nil if none matches.
func FindChild(nodes []*ConfigNode, prefix string) *ConfigNode {
	for _, node := range nodes {
		if strings.HasPrefix(node.Line, prefix) {
			return node
		}
	}
	return nil
}
