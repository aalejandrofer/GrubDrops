// Package api serves the v2 JSON API, the OIDC callback routes and the
// embedded SPA.
package api

import (
	"context"
	"database/sql"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/discover"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/update"
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
	Hub           *Hub

	// Overview and runners.
	Pool         RunnerPool
	Minutes      *store.WatchMinutes
	Claims       *store.ClaimLog
	Scans        *store.AccountScans
	Zone         func() *time.Location               // nil = time.Local
	Now          func() time.Time                    // nil = time.Now
	UpdateStatus func(current string) (bool, string) // nil = no update known

	// Games.
	Chosen       *store.ChosenGames
	AccountGames *store.AccountGames // per-account lists; nil serves 503
	Board        *collect.Board
	GamesChanged func() // nil-safe; must return at once, the reload runs in the background
	// LookupCategory turns a pasted category link's slug into the game's
	// name; nil rejects links.
	LookupCategory func(ctx context.Context, platform, slug string) (name string, ok bool, err error)

	// Discover lists every current campaign; nil serves 503.
	Discover *discover.Service

	// Force watch. ForceWatch nil serves 503. ForceLive reads the live
	// cache without blocking: nil result = not checked yet; nil func = all
	// unknown.
	ForceWatch   *store.ForceWatch
	ForceLive    func(platform, login string) *bool
	ForceChanged func() // nil-safe; must return at once, the reload runs in the background

	// Accounts and logins.
	Sessions       *store.SessionStore
	Registry       *platform.Registry
	RootCtx        context.Context
	Registrar      KickChannelRegistrar
	AccountChanged func(id string)       // nil-safe
	AccountRemoved func(id string)       // nil-safe
	AccountRenamed func(id, name string) // nil-safe; must not block
	// CheckLogin runs one account's auth probe now; nil serves 503.
	CheckLogin func(ctx context.Context, id string) (authcheck.Result, error)

	// Settings and notifications.
	Settings        *store.Settings
	Notifier        Notifier
	WebhookFallback string // env webhook used when none is stored
	// Env Telegram defaults, each used when its stored value is empty.
	TelegramTokenFallback string
	TelegramChatFallback  string
	OnSettingsUpdate      func()            // nil-safe
	SetZone               func(name string) // nil-safe

	// Health and updates.
	Ring         *mlog.Ring
	StartTime    time.Time
	Commit       string
	KickSidecars func() []string
	RunCanary    func(context.Context) error
	Update       *update.Checker
}

type server struct {
	d      Deps
	bypass bypass
	// api is the /api sub-router (CSRF applied). Public routes register on it
	// directly; admin-only routes register on protected.
	api       chi.Router
	protected chi.Router
	// canaryRunning is set while a run-now canary is in flight.
	canaryRunning atomic.Bool
	// canaryRerun asks for one more canary run once the one in flight ends.
	canaryRerun atomic.Bool
	// twitch holds the in-flight Twitch device logins, account ID -> *twitchFlow.
	twitch sync.Map
}

func newServer(d Deps) *server {
	s := &server{d: d, bypass: readBypass()}
	s.api = chi.NewRouter()
	s.api.Use(CSRF(d.SecureCookies))
	s.api.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not_found") })
	s.api.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusMethodNotAllowed, "bad_request") })
	s.routesAuth(s.api)
	s.protected = s.api.Group(func(r chi.Router) { r.Use(requireAdmin(d.Session, s.bypass)) })
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

	// Rewards was renamed Inventory; keep old bookmarks working.
	r.Get("/rewards", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/inventory", http.StatusMovedPermanently)
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
func (s *server) routesProtected(r chi.Router) {
	s.routesEvents(r)
	s.routesOverview(r)
	s.routesGames(r)
	s.routesForceWatch(r)
	s.routesCampaigns(r)
	s.routesDiscover(r)
	s.routesClaims(r)
	s.routesAccounts(r)
	s.routesSettings(r)
	s.routesHealth(r)
	s.routesAdmin(r)
}

// notBuiltHTML is served with 503 when the embedded SPA has no index.html.
const notBuiltHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>GrubDrops</title>
<style>body{font-family:system-ui,sans-serif;margin:3rem auto;max-width:36rem;padding:0 1rem}</style>
</head>
<body>
<h1>GrubDrops</h1>
<p>UI not built, run <code>npm --prefix web run build</code></p>
</body>
</html>
`

// spa serves the embedded SPA, falling back to index.html for unknown paths.
// Paths under /api/ and /auth/ get a JSON 404 instead, and missing files under
// /_app/ get a plain 404. index.html is never cached; hashed assets are
// immutable. Without an index.html it serves notBuiltHTML with 503.
func (s *server) spa() http.HandlerFunc {
	var files http.Handler
	if s.d.SPA != nil {
		files = http.FileServerFS(s.d.SPA)
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
		if _, err := fs.Stat(s.d.SPA, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuiltHTML))
			return
		}
		clean := strings.TrimPrefix(p, "/")
		if clean != "" {
			if st, err := fs.Stat(s.d.SPA, clean); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "/_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(p, "/_app/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	}
}
