package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The managed package serves the existing Vue UI and API from one process.
func frontendHandler() http.Handler {
	dist, err := fs.Sub(frontendDist, frontendDirectory)
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" || strings.HasPrefix(r.URL.Path, "/ws/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(dist, name); err != nil || info.IsDir() {
			// Missing assets must not return index.html with an HTML MIME type.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			request := r.Clone(r.Context())
			request.URL.Path, request.URL.RawPath = "/", ""
			static.ServeHTTP(w, request)
			return
		}
		static.ServeHTTP(w, r)
	})
}
