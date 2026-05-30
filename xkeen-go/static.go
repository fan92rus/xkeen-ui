// Package main provides embedded static files for XKEEN-UI.
package main

import (
	"embed"
	"io/fs"
)

//go:embed web/index.html web/login.html web/static
var webFS embed.FS

//go:embed scripts/update.sh
var updateScript string

//go:embed scripts/uninstall.sh
var uninstallScript string

// GetWebFS returns the embedded web filesystem.
func GetWebFS() fs.FS {
	sub, _ := fs.Sub(webFS, "web")
	return sub
}
