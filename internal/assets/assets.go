// Package assets exposes the statically built portal interface.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

var staticFS = mustSub(embedded, "dist")

// FS returns the root of the embedded static site.
func FS() fs.FS {
	return staticFS
}

func mustSub(source fs.FS, directory string) fs.FS {
	sub, err := fs.Sub(source, directory)
	if err != nil {
		panic(err)
	}
	return sub
}
