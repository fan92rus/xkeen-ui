// Package main provides embedded static files for XKEEN-GO.
package main

import (
	"embed"
	"io/fs"
)

//go:embed web
var webFS embed.FS

//go:embed scripts/update.sh
var updateScript string

// GetWebFS returns the embedded web filesystem.
func GetWebFS() fs.FS {
	sub, _ := fs.Sub(webFS, "web")
	return sub
}
