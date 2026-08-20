//go:build !release

// Dev build: the static Vue frontend is read straight from disk (relative to
// the process's working directory), so rerunning `npm run build` takes effect
// without a Go rebuild.
package web

import (
	"io/fs"
	"os"
)

func staticFS() fs.FS { return os.DirFS(staticDir) }
