package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/scheduler"
	"github.com/chano-fernandez/rust-drops-miner/internal/store"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

type Deps struct {
	DB            *sql.DB
	Q             *gen.Queries
	Templates     Renderer
	Session       *scs.SessionManager
	Scheduler     *scheduler.Scheduler
	Reload        func(context.Context) error
	Sessions      *store.SessionStore
	Registry      *platform.Registry
	RootCtx       context.Context
	BrowserClient KickBrowserClient
	Registrar     KickChannelRegistrar
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	if d.Session == nil {
		// Skeleton mode (used by TestHealthz) — no business routes.
		return r
	}

	setup := setupDeps{q: d.Q, t: d.Templates, sm: d.Session}
	authH := authDeps{q: d.Q, t: d.Templates, sm: d.Session}
	dash := dashboardDeps{q: d.Q, t: d.Templates, sch: d.Scheduler}
	accs := accountsDeps{q: d.Q, t: d.Templates, sm: d.Session}
	loginTwitch := newLoginTwitchDeps(d, d.RootCtx)
	loginKick := &loginKickDeps{
		q:         d.Q,
		t:         d.Templates,
		sm:        d.Session,
		sessions:  d.Sessions,
		browser:   d.BrowserClient,
		registrar: d.Registrar,
	}

	withSession := func(h http.Handler) http.Handler { return d.Session.LoadAndSave(h) }

	// Public (no auth required, but still session + CSRF)
	r.Method(http.MethodGet, "/setup", withSession(CSRF(http.HandlerFunc(setup.get))))
	r.Method(http.MethodPost, "/setup", withSession(CSRF(http.HandlerFunc(setup.post))))
	r.Method(http.MethodGet, "/login", withSession(CSRF(http.HandlerFunc(authH.loginGet))))
	r.Method(http.MethodPost, "/login", withSession(CSRF(http.HandlerFunc(authH.loginPost))))

	// Authed area
	authed := chi.NewRouter()
	authed.Use(RequireAdmin(d.Session))
	authed.Get("/", dash.page)
	authed.Get("/dashboard/cards", dash.cards)
	authed.Get("/accounts", accs.list)
	authed.Get("/accounts/new", accs.newGet)
	authed.Post("/accounts/new", accs.newPost)
	authed.Get("/accounts/{id}", accs.detail)
	authed.Get("/accounts/{id}/login", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		acc, err := d.Q.GetAccount(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch acc.Platform {
		case "twitch":
			loginTwitch.get(w, r)
		case "kick":
			loginKick.get(w, r)
		default:
			http.Error(w, "platform does not need login", http.StatusBadRequest)
		}
	})
	authed.Get("/accounts/{id}/login/poll", loginTwitch.status)
	authed.Post("/accounts/{id}/login", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		acc, err := d.Q.GetAccount(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch acc.Platform {
		case "kick":
			loginKick.post(w, r)
		default:
			http.Error(w, "platform does not accept login POST", http.StatusBadRequest)
		}
	})
	authed.Post("/accounts/{id}/update", accs.update)
	authed.Post("/accounts/{id}/delete", accs.delete)
	authed.Post("/accounts/apply", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.Session.Put(r.Context(), "flash", "watchers reloaded")
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
	})
	authed.Post("/logout", authH.logoutPost)

	r.Mount("/", withSession(CSRF(authed)))
	return r
}
