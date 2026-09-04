package docs

import (
	"errors"
	"strings"
	"testing"
)

func TestList(t *testing.T) {
	pages, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("List returned no pages")
	}
	seen := map[string]bool{}
	prevOrder := -1
	for i, p := range pages {
		if p.Slug == "" || p.Title == "" {
			t.Errorf("page %d missing slug/title: %+v", i, p)
		}
		if p.Markdown != "" {
			t.Errorf("List included markdown for %q", p.Slug)
		}
		if !validSlug(p.Slug) {
			t.Errorf("List returned invalid slug %q", p.Slug)
		}
		if seen[p.Slug] {
			t.Errorf("duplicate slug %q", p.Slug)
		}
		seen[p.Slug] = true
		if p.Order < prevOrder {
			t.Errorf("pages not ordered: %q order %d after %d", p.Slug, p.Order, prevOrder)
		}
		prevOrder = p.Order
	}
	if !seen["index"] {
		t.Error("List missing index")
	}
	if !seen["sbom"] {
		t.Error("List missing sbom")
	}
}

func TestGetIndex(t *testing.T) {
	p, err := Get("index")
	if err != nil {
		t.Fatalf("Get(index): %v", err)
	}
	if p.Title == "" {
		t.Error("empty title")
	}
	if !strings.Contains(p.Markdown, "# ") {
		t.Errorf("markdown missing heading: %q", p.Markdown[:min(80, len(p.Markdown))])
	}
	if strings.HasPrefix(p.Markdown, "---") {
		t.Error("Get left front matter in markdown")
	}
}

func TestGetRejectsTraversal(t *testing.T) {
	for _, slug := range []string{"", "Index", "index.md", "../user", "foo/bar", "user/index", ".", "..", "a b"} {
		_, err := Get(slug)
		if !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("Get(%q) err = %v, want ErrInvalidSlug", slug, err)
		}
	}
}

func TestGetSBOMStub(t *testing.T) {
	if releaseMarkdown != "" {
		t.Skip("release overlay is active")
	}
	p, err := Get("sbom")
	if err != nil {
		t.Fatalf("Get(sbom): %v", err)
	}
	if p.Title != "Software bill of materials" {
		t.Errorf("title = %q", p.Title)
	}
	if !strings.Contains(p.Markdown, "This page is the placeholder") {
		t.Errorf("stub markdown missing placeholder note: %q", p.Markdown[:min(120, len(p.Markdown))])
	}
	if strings.Contains(p.Markdown, "factum-sbom: generated") {
		t.Error("dev Get(sbom) served a generated inventory")
	}
}

func TestGetSBOMReleaseOverlay(t *testing.T) {
	orig := releaseMarkdown
	releaseMarkdown = "<!-- factum-sbom: generated -->\n\n# Software bill of materials\n\n| Module | Version |\n| --- | --- |\n| example.com/foo | v1.0.0 |\n"
	t.Cleanup(func() { releaseMarkdown = orig })

	p, err := Get("sbom")
	if err != nil {
		t.Fatalf("Get(sbom): %v", err)
	}
	if p.Title != "Software bill of materials" {
		t.Errorf("title = %q, overlay should keep stub title", p.Title)
	}
	if strings.Contains(p.Markdown, "factum-sbom: generated") {
		t.Error("generated marker leaked into markdown")
	}
	if !strings.Contains(p.Markdown, "example.com/foo") {
		t.Errorf("overlay missing inventory: %q", p.Markdown)
	}
	if strings.Contains(p.Markdown, "This page is the placeholder") {
		t.Error("overlay left stub body in place")
	}
}

func TestGetMissing(t *testing.T) {
	_, err := Get("no-such-page")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(no-such-page) err = %v, want ErrNotFound", err)
	}
}

func TestParseFrontMatter(t *testing.T) {
	p := parsePage("sample", []byte("---\ntitle: Hello\norder: 7\n---\n\n# Ignored heading\n\nbody\n"))
	if p.Title != "Hello" {
		t.Errorf("title = %q, want Hello", p.Title)
	}
	if p.Order != 7 {
		t.Errorf("order = %d, want 7", p.Order)
	}
	if strings.Contains(p.Markdown, "title:") {
		t.Errorf("front matter leaked into body: %q", p.Markdown)
	}
	if !strings.Contains(p.Markdown, "# Ignored heading") {
		t.Errorf("body heading stripped: %q", p.Markdown)
	}
}
