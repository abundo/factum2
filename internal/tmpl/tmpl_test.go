package tmpl

import (
	"strings"
	"testing"
)

type peer struct {
	NeighborIP string
	Fields     map[string]any
}

type renderData struct {
	Name   string
	Others []peer
	Fields map[string]any
	Vars   map[string]any
}

func TestExecuteIndexAndMap(t *testing.T) {
	out, err := Execute(`{{ .Others[0].NeighborIP }} {{ .Others[0].Fields.vlan }} {{ .Vars.mtu }}`, renderData{
		Others: []peer{{NeighborIP: "10.1.1.1", Fields: map[string]any{"vlan": 9}}},
		Vars:   map[string]any{"mtu": 9100},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out)
	if got != "10.1.1.1 9 9100" {
		t.Fatalf("got %q", got)
	}
}

func TestExecuteIfRangeCompare(t *testing.T) {
	src := `{{ if .Name == "CN1" }}yes{{ else }}no{{ end }}
{{ range .Others }}{{ .NeighborIP }}{{ end }}`
	out, err := Execute(src, renderData{Name: "CN1", Others: []peer{{NeighborIP: "1.2.3.4"}}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	lines := SplitCLI(out)
	if len(lines) != 2 || lines[0] != "yes" || lines[1] != "1.2.3.4" {
		t.Fatalf("got %#v", lines)
	}
}

func TestExecuteMissingStructField(t *testing.T) {
	if _, err := Execute(`{{ .Nope }}`, renderData{}, Options{}); err == nil {
		t.Fatal("missing struct field should error")
	}
}

func TestExecuteNoHTMLEscape(t *testing.T) {
	out, err := Execute(`{{ .Name }}`, renderData{Name: `a<b>&"c`}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != `a<b>&"c` {
		t.Fatalf("escaped? got %q", out)
	}
}

func TestExecuteIncludeMacro(t *testing.T) {
	out, err := Execute(`before {{ include "defaults" }} after`, renderData{Name: "x"}, Options{
		Macros: map[string]string{"defaults": "mac {{ .Name }}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "before mac x after" {
		t.Fatalf("got %q", out)
	}
	out, err = Execute(`{{ include("defaults") }}`, renderData{Name: "y"}, Options{
		Macros: map[string]string{"defaults": "mac {{ .Name }}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "mac y" {
		t.Fatalf("func include got %q", out)
	}
}

func TestExecuteIncludeUnknown(t *testing.T) {
	if _, err := Execute(`{{ include "missing" }}`, nil, Options{}); err == nil {
		t.Fatal("unknown include should error")
	}
}

func TestExecuteBlock(t *testing.T) {
	src := `{{block cleanup()}}
drop {{ .Name }}
{{end}}
keep {{ .Name }}
`
	full, err := Execute(src, renderData{Name: "CN1"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := SplitCLI(full); len(got) != 2 || got[0] != "drop CN1" || got[1] != "keep CN1" {
		t.Fatalf("full %#v", SplitCLI(full))
	}
	only, err := Execute(src, renderData{Name: "CN1"}, Options{Block: "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if got := SplitCLI(only); len(got) != 1 || got[0] != "drop CN1" {
		t.Fatalf("block %#v", SplitCLI(only))
	}
}

func TestJoinEq(t *testing.T) {
	out, err := Execute(`{{ join(",", .Others) }} {{ if eq(.Name, "a") }}y{{ end }}`, struct {
		Name   string
		Others []string
	}{Name: "a", Others: []string{"x", "y"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "x,y y" {
		t.Fatalf("got %q", out)
	}
}
