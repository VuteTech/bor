// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// FrontendHandler serves embedded frontend static files.
// For any path that doesn't match a static file and isn't an API route,
// it falls back to serving index.html (SPA support).
func FrontendHandler(staticFS fs.FS) http.Handler {
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("critical: failed to initialize embedded static file system: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve frontend for API routes
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the exact file first
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Check if the file exists in the embedded FS
		filePath := strings.TrimPrefix(path, "/")
		if _, err := fs.Stat(subFS, filePath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
