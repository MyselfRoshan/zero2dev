package api

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"zero2dev/internal/apperr"
)

// Explicit types: the stdlib mime table can be overridden by the Windows
// registry (a classic source of .js served as text/plain).
var staticTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json", ".svg": "image/svg+xml", ".map": "application/json",
	".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2", ".woff": "font/woff", ".txt": "text/plain; charset=utf-8",
	".webmanifest": "application/manifest+json", ".wasm": "application/wasm",
}

// static serves the embedded UI with SPA fallback: unknown paths without a
// file extension get index.html; unknown paths with an extension are 404.
func (s *Server) static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeErr(w, &apperr.Error{Status: http.StatusMethodNotAllowed, Code: "bad_request", Message: "method not allowed"})
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(s.UI, name)
		if err != nil || strings.HasSuffix(name, "/") {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			if data, err = fs.ReadFile(s.UI, name); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		ext := strings.ToLower(path.Ext(name))
		ct := staticTypes[ext]
		if ct == "" {
			ct = mime.TypeByExtension(ext)
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		h := w.Header()
		h.Set("Content-Type", ct)
		switch {
		case name == "index.html":
			h.Set("Cache-Control", "no-store")
		case strings.HasPrefix(name, "assets/"):
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			h.Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
