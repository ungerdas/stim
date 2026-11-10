// Package webui embeds the built frontend (web/ builds into dist/ here).
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built frontend. It is empty apart from a placeholder until
// `npm run build` has been run in web/.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
