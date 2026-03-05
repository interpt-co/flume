package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var frontendFS embed.FS

// FrontendHandler returns an http.Handler that serves the embedded frontend
// assets. Unknown paths fall back to index.html so that Vue Router's
// client-side routing works correctly.
func FrontendHandler() http.Handler {
	distFS, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to stat the requested path inside the embedded FS.
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		if _, err := fs.Stat(distFS, path[1:]); err != nil {
			// File not found – serve index.html for SPA routing.
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}
