//go:build release

package docs

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/sbom"
)

func TestReleaseSBOMFile(t *testing.T) {
	p, err := Get("sbom")
	if err != nil {
		t.Fatalf("Get(sbom): %v", err)
	}
	if strings.Contains(generatedSBOM, sbom.GeneratedMarker) {
		if releaseMarkdown == "" {
			t.Fatal("generated marker present but overlay empty")
		}
		if strings.Contains(p.Markdown, "This page is the placeholder") {
			t.Fatal("generated inventory did not replace the stub")
		}
		if !strings.Contains(p.Markdown, "## Go modules") {
			t.Fatal("missing Go modules section")
		}
		if !strings.Contains(p.Markdown, "## npm packages (GUI)") {
			t.Fatal("missing npm section")
		}
		return
	}
	if releaseMarkdown != "" {
		t.Fatal("placeholder sbom.md set releaseMarkdown")
	}
	if !strings.Contains(p.Markdown, "This page is the placeholder") {
		t.Fatal("placeholder build should serve the stub")
	}
}
