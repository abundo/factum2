package docs

import (
	"strings"

	"github.com/abundo/factum2/internal/sbom"
)

// releaseMarkdown, when non-empty, replaces the body of docs/user/sbom.md
// on Get. A release build (`-tags release`) sets this from the generated
// inventory if that file carries sbom.GeneratedMarker; development builds
// leave it empty so the committed stub is served.
var releaseMarkdown string

func overlaySBOM(p Page) Page {
	if p.Slug != "sbom" || releaseMarkdown == "" {
		return p
	}
	body := strings.ReplaceAll(releaseMarkdown, sbom.GeneratedMarker+"\n", "")
	p.Markdown = strings.TrimSpace(body) + "\n"
	return p
}
