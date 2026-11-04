// Package api is the HTTP layer: routing, request hygiene (Host, Origin,
// bearer token) and JSON helpers. See docs/API.md.
package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/config"
	"zero2dev/internal/events"
	"zero2dev/internal/installer"
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// Deps are the collaborators of the API.
type Deps struct {
	Cfg       *config.Config
	Store     *store.Store
	Hub       *events.Hub
	Sup       *supervisor.Supervisor
	Runtimes  *runtimes.Manager
	Installer *installer.Installer
	Sites     *sites.Manager
	Catalog   func() ([]model.CatalogEntry, error)
	UI        fs.FS
	Version   string
	Started   time.Time
}

// Server is the http.Handler for the whole daemon.
type Server struct {
	Deps
	mux       *http.ServeMux
	tokenHash [32]byte
	hosts     map[string]bool
	origins   map[string]bool
}

const maxBody = 1 << 20

// New builds the handler.
func New(d Deps) *Server {
	s := &Server{Deps: d, mux: http.NewServeMux(), tokenHash: sha256.Sum256([]byte(d.Cfg.Token))}
	p := strconv.Itoa(d.Cfg.Port)
	s.hosts = map[string]bool{"127.0.0.1:" + p: true, "localhost:" + p: true}
	s.origins = map[string]bool{"http://127.0.0.1:" + p: true, "http://localhost:" + p: true}
	if d.Cfg.DevOrigin != "" {
		s.origins[d.Cfg.DevOrigin] = true
	}
	s.routes()
	return s
}

// handler is a handler that reports failures by returning an error.
type handler func(w http.ResponseWriter, r *http.Request) error

func (s *Server) handle(pattern string, h handler) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeErr(w, err)
		}
	})
}

func (s *Server) routes() {
	s.handle("GET /api/status", s.status)
	s.handle("GET /api/services", s.listServices)
	s.handle("POST /api/services", s.createService)
	s.handle("GET /api/services/{id}", s.getService)
	s.handle("PUT /api/services/{id}", s.updateService)
	s.handle("DELETE /api/services/{id}", s.deleteService)
	s.handle("POST /api/services/{id}/start", s.startService)
	s.handle("POST /api/services/{id}/stop", s.stopService)
	s.handle("POST /api/services/{id}/restart", s.restartService)
	s.handle("GET /api/services/{id}/logs", s.serviceLogs)
	s.handle("GET /api/runtimes", s.listRuntimes)
	s.handle("GET /api/runtimes/detect", s.detectRuntimes)
	s.handle("POST /api/runtimes/register", s.registerRuntime)
	s.handle("POST /api/runtimes/install", s.installRuntime)
	s.handle("DELETE /api/runtimes/{id}", s.deleteRuntime)
	s.handle("GET /api/catalog", s.listCatalog)
	s.handle("GET /api/sites", s.listSites)
	s.handle("POST /api/sites", s.createSite)
	s.handle("PUT /api/sites/{id}", s.updateSite)
	s.handle("DELETE /api/sites/{id}", s.deleteSite)
	s.handle("GET /api/jobs/{id}", s.getJob)
	s.handle("GET /api/settings", s.getSettings)
	s.handle("PUT /api/settings", s.putSettings)
	s.handle("POST /api/events/ticket", s.eventTicket)
	s.handle("GET /api/events", s.eventStream)
	s.handle("/api/", func(http.ResponseWriter, *http.Request) error {
		return apperr.NotFound("no such endpoint")
	})
	s.mux.Handle("/", s.static())
}

// ServeHTTP applies the request hygiene rules from docs/API.md before
// dispatching.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")

	// 1. Host check (all routes, static included): blocks DNS rebinding,
	// where an attacker's domain resolves to 127.0.0.1 but the browser still
	// sends the attacker's hostname in Host.
	if !s.hosts[strings.ToLower(r.Host)] {
		writeErr(w, &apperr.Error{Status: http.StatusForbidden, Code: "forbidden_host", Message: "unexpected Host header"})
		return
	}
	if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
		s.mux.ServeHTTP(w, r)
		return
	}
	h.Set("Cache-Control", "no-store")

	// 2. Origin check: browsers always attach Origin to cross-site requests.
	if origin := r.Header.Get("Origin"); origin != "" {
		if !s.origins[origin] {
			writeErr(w, &apperr.Error{Status: http.StatusForbidden, Code: "forbidden_origin", Message: "origin not allowed"})
			return
		}
		if origin == s.Cfg.DevOrigin && origin != "" { // CORS only for the dev server
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	}

	// 3. Bearer token. The SSE endpoint is the one exception: EventSource
	// cannot set headers, so it is authorised by a single-use ticket instead.
	if !(r.Method == http.MethodGet && r.URL.Path == "/api/events") && !s.authorized(r) {
		h.Set("WWW-Authenticate", "Bearer")
		writeErr(w, apperr.Unauthorized("missing or invalid access token"))
		return
	}
	s.mux.ServeHTTP(w, r)
}

// authorized compares the token in constant time (hashing first so length
// differences do not leak either).
func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	a := r.Header.Get("Authorization")
	if len(a) <= len(prefix) || !strings.EqualFold(a[:len(prefix)], prefix) {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(a[len(prefix):])))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

// ---- JSON helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	e := apperr.From(err)
	writeJSON(w, e.Status, map[string]any{"error": map[string]string{"code": e.Code, "message": e.Message}})
}

// decode reads a JSON body strictly: 1 MB cap, unknown fields rejected,
// trailing garbage rejected.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			return apperr.BadRequest("request body too large (max 1 MB)")
		case errors.Is(err, io.EOF):
			return apperr.BadRequest("request body is required")
		}
		return apperr.BadRequest("invalid JSON: %v", err)
	}
	if dec.More() {
		return apperr.BadRequest("invalid JSON: unexpected data after the object")
	}
	return nil
}
