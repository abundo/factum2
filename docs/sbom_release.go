//go:build release

package docs

import (
	_ "embed"
	"strings"

	"github.com/abundo/factum2/internal/sbom"
)

//go:embed generated/sbom.md
var generatedSBOM string

func init() {
	if strings.Contains(generatedSBOM, sbom.GeneratedMarker) {
		releaseMarkdown = generatedSBOM
	}
}
