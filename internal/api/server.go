// Package api serves the v2 JSON API, the OIDC callback routes and the
// embedded SPA.
package api

import (
	"database/sql"
	"io/fs"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Deps holds everything the API needs. Session, DB and Q are required for the
// auth routes; OIDC and SPA may be nil (SSO off, no SPA served).
type Deps struct {
	DB            *sql.DB
	Q             *gen.Queries
	Session       *scs.SessionManager
	OIDC          *oidc.Provider
	SecureCookies bool
	Version       string
	SPA           fs.FS
}

type server struct {
	d Deps
	// api is the /api sub-router (CSRF applied). Public routes register on it
	// directly; admin-only routes register on protected.
	api       chi.Router
	protected chi.Router
}

func newServer(d Deps) *server {
	s := &server{d: d}
	s.api = chi.NewRouter()
	s.api.Use(CSRF(d.SecureCookies))
	s.api.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not_found") })
	s.api.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusMethodNotAllowed, "bad_request") })
	s.routesAuth(s.api)
	s.protected = s.api.Group(func(r chi.Router) { r.Use(RequireAdmin(d.Session)) })
	return s
}

// handler assembles the full router. Later tasks add resources through
// routesX methods called on s.protected here.
func (s *server) handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	r.Group(func(r chi.Router) {
		r.Use(s.d.Session.LoadAndSave)
		r.Route("/auth/oidc", s.routesOIDC)
		r.Mount("/api", s.api)
	})

	r.NotFound(s.spa())
	return r
}

// NewRouter builds the HTTP handler: /healthz, /auth/oidc/*, /api/* and the
// SPA fallback.
func NewRouter(d Deps) http.Handler {
	s := newServer(d)
	s.routesProtected(s.protected)
	return s.handler()
}

// routesProtected registers every admin-only resource. Add one line per
// resource: s.routesX(r).
func (s *server) routesProtected(r chi.Router) {}

// spa serves the embedded SPA, falling back to index.html for unknown paths.
// Paths under /api/ and /auth/ get a JSON 404 instead.
func (s *server) spa() http.HandlerFunc {
	var files http.Handler
	if s.d.SPA != nil {
		files = http.FileServer(http.FS(s.d.SPA))
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || p == "/api" || strings.HasPrefix(p, "/auth/") {
			writeErr(w, http.StatusNotFound, "not_found")
			return
		}
		if files == nil {
			http.NotFound(w, r)
			return
		}
		clean := strings.TrimPrefix(p, "/")
		if clean != "" {
			if f, err := s.d.SPA.Open(clean); err == nil {
				st, serr := f.Stat()
				_ = f.Close()
				if serr == nil && !st.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	}
}
