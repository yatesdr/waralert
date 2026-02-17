// Package www provides a browser-based management UI with htmx.
package www

import (
	"embed"
	"io/fs"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// StaticFS returns the embedded static files filesystem.
func StaticFS() fs.FS {
	sub, _ := fs.Sub(staticFS, "static")
	return sub
}
