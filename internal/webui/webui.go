// Package webui embeds the built Svelte app (web/ is built into ./dist).
//
// ./dist is generated and not in git. On a fresh clone every Go command fails
// with "pattern all:dist: no matching files found" until the UI is built:
//
//	cd web && pnpm install && pnpm build    (or: make web, or .\build.ps1)
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the UI files rooted at the dist directory.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
