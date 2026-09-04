package sbom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGoListJSON(t *testing.T) {
	const input = `
{"Path":"github.com/abundo/factum2","Main":true}
{"Path":"github.com/labstack/echo/v5","Version":"v5.3.1"}
{"Path":"golang.org/x/sys","Version":"v0.1.0","Indirect":true}
{"Path":"github.com/abundo/limetool","Version":"v1.0.0","Replace":{"Path":"../limetool"}}
`
	mods, err := ParseGoListJSON(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseGoListJSON: %v", err)
	}
	if len(mods) != 3 {
		t.Fatalf("got %d modules, want 3 (main skipped): %+v", len(mods), mods)
	}
	byPath := map[string]GoModule{}
	for _, m := range mods {
		byPath[m.Path] = m
	}
	if byPath["github.com/labstack/echo/v5"].Indirect {
		t.Error("echo marked indirect")
	}
	if !byPath["golang.org/x/sys"].Indirect {
		t.Error("sys not marked indirect")
	}
	limetool := byPath["github.com/abundo/limetool"]
	if limetool.ReplacePath != "../limetool" {
		t.Errorf("limetool replace = %q", limetool.ReplacePath)
	}
}

func TestParseNPMLockFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := ParseNPMLock(data)
	if err != nil {
		t.Fatalf("ParseNPMLock: %v", err)
	}
	byName := map[string]NPMPackage{}
	for _, p := range pkgs {
		byName[p.Name] = p
	}
	if _, ok := byName["skip-link"]; ok {
		t.Fatal("link entry included")
	}
	vue, ok := byName["vue"]
	if !ok || vue.Version != "3.5.38" || vue.Tree != "direct" || vue.License != "MIT" {
		t.Errorf("vue = %+v", vue)
	}
	if p := byName["@nuxt/ui"]; p.Tree != "direct" || p.Version != "4.10.0" {
		t.Errorf("@nuxt/ui = %+v", p)
	}
	if p := byName["vite"]; p.Tree != "development" || p.Version != "8.0.16" {
		t.Errorf("vite = %+v", p)
	}
	nested := byName["foo|bar"]
	if nested.Version != "1.2.3" || nested.Tree != "transitive" {
		t.Errorf("nested foo|bar = %+v", nested)
	}
	if p := byName["licensed-obj"]; p.License != "Apache-2.0" {
		t.Errorf("license object = %q", p.License)
	}
	if len(pkgs) != 5 {
		t.Errorf("got %d packages, want 5: %+v", len(pkgs), pkgs)
	}
}

func TestParseNPMLockRealFrontend(t *testing.T) {
	root, err := FindRepoRoot("")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "web", "frontend", "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := ParseNPMLock(data)
	if err != nil {
		t.Fatalf("ParseNPMLock(real): %v", err)
	}
	if len(pkgs) < 10 {
		t.Fatalf("too few production packages: %d", len(pkgs))
	}
	foundVue, foundVite, foundESLint := false, false, false
	for _, p := range pkgs {
		switch {
		case p.Name == "vue" && p.Tree == "direct":
			foundVue = true
		case p.Name == "vite" && p.Tree == "development":
			foundVite = true
		case p.Name == "eslint" && strings.HasPrefix(p.Tree, "development"):
			foundESLint = true
		}
	}
	if !foundVue {
		t.Error("missing direct vue")
	}
	if !foundVite {
		t.Error("missing development vite")
	}
	if !foundESLint {
		t.Error("missing development eslint")
	}
}

func TestMarkdownContainsMarkerAndEscapesPipes(t *testing.T) {
	md := Markdown(Meta{
		Version:   "v1.2.3",
		Commit:    "abc123",
		Date:      "2026-01-02T03:04:05Z",
		GoVersion: "go1.25.0",
	}, []GoModule{
		{Path: "example.com/foo", Version: "v1.0.0"},
		{Path: "example.com/bar", Version: "v2.0.0", Indirect: true, ReplacePath: "../bar"},
	}, []NPMPackage{
		{Name: "foo|bar", Version: "1.2.3", License: "ISC", Tree: "transitive"},
		{Name: "vue", Version: "3.5.38", License: "MIT", Tree: "direct"},
	})
	if !strings.HasPrefix(md, GeneratedMarker) {
		t.Fatal("missing generated marker")
	}
	for _, want := range []string{
		"# Software bill of materials",
		"**v1.2.3**",
		"commit `abc123`",
		"example.com/foo",
		"replaced by ../bar",
		"foo\\|bar",
		"| vue | 3.5.38 | MIT | direct |",
		"2 modules.",
		"2 packages from",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n%s", want, md)
		}
	}
}

func TestGenerateFromThisRepo(t *testing.T) {
	root, err := FindRepoRoot("")
	if err != nil {
		t.Fatal(err)
	}
	md, err := Generate(root, Meta{Version: "test", Commit: "none", Date: "unknown", GoVersion: "go1.25.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, GeneratedMarker) {
		t.Fatal("generated markdown missing marker")
	}
	if !strings.Contains(md, "github.com/labstack/echo/v5") {
		t.Fatal("missing echo module")
	}
	if !strings.Contains(md, "github.com/abundo/limetool") {
		t.Fatal("missing limetool")
	}
	if !strings.Contains(md, "replaced by") {
		t.Fatal("missing replace annotation")
	}
	if !strings.Contains(md, "| vue |") {
		t.Fatal("missing vue npm package")
	}
	if !strings.Contains(md, "| vite |") {
		t.Fatal("missing vite npm package")
	}
}
