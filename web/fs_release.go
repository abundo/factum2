//go:build release

// Release build (`go build -tags release`): the static Vue frontend
// (web/static/vue, must already be built via `npm run build` before this
// compiles) is embedded into the binary, producing a single self-contained
// executable with no web/ directory needed alongside it.
package web

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed all:static
var rawStaticFS embed.FS

// wiregasmVersion forces a release compile to fail unless `make wiregasm`
// has installed the pinned build. all:static embeds those files with the
// Vue app; this file is the small piece the compiler requires by name.
//
//go:embed static/wiregasm/VERSION
var wiregasmVersion string

func staticFS() fs.FS { return mustSubFS(rawStaticFS, "static") }

func init() {
	if strings.TrimSpace(wiregasmVersion) == "" {
		panic("web/static/wiregasm/VERSION is empty; run make wiregasm before a release build")
	}
}

// mustSubFS re-roots an embed.FS at dir, since //go:embed all:static keeps
// "static/" as a path prefix but callers want the contents directly at root
// (matching the os.DirFS behaviour used in the non-release build).
func mustSubFS(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
