// Package webui serves the web application embedded in release binaries.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:assets
var assets embed.FS

// Handler serves the built SPA, or a development placeholder for plain go build.
func Handler() http.Handler {
	root, _ := fs.Sub(assets, "assets")
	if _, err := fs.Stat(root, "built/index.html"); err == nil {
		root, _ = fs.Sub(root, "built")
	}
	return handler(root)
}

func handler(root fs.FS) http.Handler {
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(root, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		// Missing chunks and API paths must never return HTML.
		first, _, _ := strings.Cut(name, "/")
		if strings.Contains(path.Base(name), ".") || first == "rpc" || first == "ws" || first == "blobs" || first == "healthz" {
			http.NotFound(w, r)
			return
		}
		raw, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method != http.MethodHead {
			_, _ = w.Write(raw)
		}
	})
}
