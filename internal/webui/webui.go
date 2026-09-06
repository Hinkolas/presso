// Package webui serves the SvelteKit single-page app embedded in the binary.
// `task build:web` copies web/build into dist/ before `go build`; a checkout
// without that step still compiles (dist/ holds only .gitkeep) and serves a
// short notice instead of the UI.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the SPA: real files as-is (with long cache headers for the
// hashed _app/ assets), everything else falls back to index.html so client
// side routes work on hard loads.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	index, indexErr := fs.ReadFile(sub, "index.html")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if indexErr != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("presso: the web UI is not built into this binary. Run `task build:web` and rebuild, or use `task dev:web` for the dev server.\n"))
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if f, err := sub.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}
