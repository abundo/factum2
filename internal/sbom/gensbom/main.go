// Command gensbom writes the release software-bill-of-materials markdown
// that factum2-web embeds under -tags release.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/abundo/factum2/internal/sbom"
)

func main() {
	out := flag.String("o", "", "output path (stdout if empty)")
	version := flag.String("version", "", "release version (default: git describe)")
	commit := flag.String("commit", "", "git commit (default: HEAD)")
	date := flag.String("date", "", "build date (default: HEAD committer date)")
	flag.Parse()

	root, err := sbom.FindRepoRoot("")
	if err != nil {
		fatal(err)
	}
	md, err := sbom.Generate(root, sbom.Meta{
		Version: *version,
		Commit:  *commit,
		Date:    *date,
	})
	if err != nil {
		fatal(err)
	}
	if *out == "" {
		if _, err := os.Stdout.WriteString(md); err != nil {
			fatal(err)
		}
		return
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "gensbom: %v\n", err)
	os.Exit(1)
}
