// Package sbom builds a human-readable software bill of materials for a
// Factum release: every Go module in the module graph plus production npm
// packages locked for the Vue GUI.
package sbom

import (
	"fmt"
	"sort"
	"strings"
)

// GeneratedMarker is written at the top of a generated inventory so a
// release binary can tell a real SBOM from the placeholder file that
// keeps `go build -tags release` compiling between builds.
const GeneratedMarker = "<!-- factum-sbom: generated -->"

// Meta is the release identity stamped into the inventory heading.
type Meta struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
}

// GoModule is one module from `go list -m -json all` (the main module is
// omitted from the table; it is named in the heading).
type GoModule struct {
	Path           string
	Version        string
	Indirect       bool
	ReplacePath    string
	ReplaceVersion string
}

// NPMPackage is one package from package-lock.json (v2/v3 `packages` map).
type NPMPackage struct {
	Name    string
	Version string
	License string
	// Tree is "direct", "transitive", "development", or
	// "development (transitive)".
	Tree string
}

// Markdown renders the operator-facing inventory page (no YAML front
// matter; docs.Get keeps title/order from docs/user/sbom.md).
func Markdown(meta Meta, modules []GoModule, npm []NPMPackage) string {
	var b strings.Builder
	b.WriteString(GeneratedMarker)
	b.WriteString("\n\n")
	b.WriteString("# Software bill of materials\n\n")

	version := display(meta.Version, "dev")
	b.WriteString("Third-party software in Factum **")
	b.WriteString(escapeCell(version))
	b.WriteString("**")
	details := make([]string, 0, 3)
	if c := display(meta.Commit, ""); c != "" && c != "none" {
		details = append(details, "commit `"+c+"`")
	}
	if d := display(meta.Date, ""); d != "" && d != "unknown" {
		details = append(details, "built "+d)
	}
	if g := display(meta.GoVersion, ""); g != "" {
		details = append(details, "Go "+g)
	}
	if len(details) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(details, ", "))
		b.WriteString(")")
	}
	b.WriteString(".\n\n")
	b.WriteString("This list is compiled into the `factum2-web` **release** binary.\n")
	b.WriteString("It covers every Go module in the module graph (all `cmd/*`\n")
	b.WriteString("binaries in the tarball) and every npm package locked for the GUI.\n")
	b.WriteString("The npm *Tree* column is `direct` (a `package.json` dependency),\n")
	b.WriteString("`development` (a `package.json` devDependency), or `transitive` /\n")
	b.WriteString("`development (transitive)` for the rest of the lockfile.\n\n")

	b.WriteString("## Go modules\n\n")
	fmt.Fprintf(&b, "%d modules.\n\n", len(modules))
	writeTable(&b,
		[]string{"Module", "Version", "Scope"},
		goRows(modules),
	)

	b.WriteString("\n## npm packages (GUI)\n\n")
	fmt.Fprintf(&b, "%d packages from `web/frontend/package-lock.json`.\n\n", len(npm))
	writeTable(&b,
		[]string{"Package", "Version", "License", "Tree"},
		npmRows(npm),
	)
	return b.String()
}

func goRows(modules []GoModule) [][]string {
	mods := append([]GoModule(nil), modules...)
	sort.Slice(mods, func(i, j int) bool {
		if mods[i].Path != mods[j].Path {
			return mods[i].Path < mods[j].Path
		}
		return mods[i].Version < mods[j].Version
	})
	rows := make([][]string, 0, len(mods))
	for _, m := range mods {
		ver := display(m.Version, "")
		if m.ReplacePath != "" {
			repl := m.ReplacePath
			if m.ReplaceVersion != "" {
				repl += " " + m.ReplaceVersion
			}
			if ver != "" {
				ver += " (replaced by " + repl + ")"
			} else {
				ver = "replaced by " + repl
			}
		}
		scope := "direct"
		if m.Indirect {
			scope = "indirect"
		}
		rows = append(rows, []string{m.Path, ver, scope})
	}
	return rows
}

func npmRows(pkgs []NPMPackage) [][]string {
	list := append([]NPMPackage(nil), pkgs...)
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].Version < list[j].Version
	})
	rows := make([][]string, 0, len(list))
	for _, p := range list {
		rows = append(rows, []string{p.Name, p.Version, display(p.License, "—"), display(p.Tree, "transitive")})
	}
	return rows
}

func writeTable(b *strings.Builder, headers []string, rows [][]string) {
	b.WriteString("| ")
	b.WriteString(strings.Join(escapeAll(headers), " | "))
	b.WriteString(" |\n| ")
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	b.WriteString(strings.Join(seps, " | "))
	b.WriteString(" |\n")
	for _, row := range rows {
		cells := make([]string, len(headers))
		for i := range headers {
			if i < len(row) {
				cells[i] = escapeCell(row[i])
			}
		}
		b.WriteString("| ")
		b.WriteString(strings.Join(cells, " | "))
		b.WriteString(" |\n")
	}
}

func escapeAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = escapeCell(s)
	}
	return out
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

func display(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}
