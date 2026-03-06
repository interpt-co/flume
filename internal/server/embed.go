package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
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
		// Clean the path to prevent traversal attacks.
		cleaned := path.Clean(r.URL.Path)
		if cleaned != "/" {
			if _, err := fs.Stat(distFS, cleaned[1:]); err != nil {
				// File not found – serve index.html for SPA routing.
				r.URL.Path = "/"
			} else {
				r.URL.Path = cleaned
			}
		}

		fileServer.ServeHTTP(w, r)
	})
}
