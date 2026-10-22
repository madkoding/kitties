// Package web carries the built web interface inside the binary, so one executable is the
// gateway and the page that drives it. `make web` copies desktop/frontend/dist into dist/ before
// the build; a binary built without it serves the gateway's small built-in page instead.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the interface and true when the binary was built with it.
func FS() (fs.FS, bool) { return sub(embedded) }

// sub opens dist/ of fsys and reports whether it holds an index.html.
func sub(fsys fs.FS) (fs.FS, bool) {
	dist, _ := fs.Sub(fsys, "dist") // only an invalid path fails, and this one is constant
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return nil, false
	}
	return dist, true
}
