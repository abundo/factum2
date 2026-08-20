//go:build release

// Release build (`go build -tags release`): the static Vue frontend
// (web/static/vue, must already be built via `npm run build` before this
// compiles) is embedded into the binary, producing a single self-contained
// executable with no web/ directory needed alongside it.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var rawStaticFS embed.FS

func staticFS() fs.FS { return mustSubFS(rawStaticFS, "static") }

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
