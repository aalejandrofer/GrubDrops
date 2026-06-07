package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	mlog "github.com/aalejandrofer/dropsminer/internal/log"
	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/scheduler"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
	"github.com/aalejandrofer/dropsminer/internal/web"
)

// applyRedirectTarget picks the post-/accounts/apply landing page from
// the Referer header. The dashboard also has a Reload button, so we
// avoid the old behavior of always bouncing the user to /accounts:
//   - if the referer mentions /accounts, return that path (sticks the
//     user on whichever /accounts page they were on);
//   - otherwise, return the referer path so they stay put (e.g.
//     dashboard "/");
//   - fall back to "/" when the header is missing or unparseable.
// Only the path+query of the referer is honored — never the host — to
// avoid open-redirect vectors.
func applyRedirectTarget(r *http.Request) string {
	ref := strings.TrimSpace(r.Header.Get("Referer"))
	if ref == "" {
		return "/"
	}
	u, err := url.Parse(ref)
	if err != nil || u.Path == "" {
		return "/"
	}
	target := u.Path
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return target
}

type Deps struct {
	DB              *sql.DB
	Q               *gen.Queries
	Templates       Renderer
	Session         *scs.SessionManager
	Scheduler       *scheduler.Scheduler
	Reload          func(context.Context) error
	Sessions        *store.SessionStore
	Registry        *platform.Registry
	RootCtx         context.Context
	BrowserClient   KickBrowserClient
	Registrar       KickChannelRegistrar
	SettingsStore   *store.Settings
	OnSettingsUpdate func()
	// TwitchBrowser indicates the Twitch backend is the browser-routed
	// variant; the login handler redirects to the cookie-paste page
	// instead of the device-code flow when true.
	TwitchBrowser bool
	LogRing       *mlog.Ring
	StartTime     time.Time

	// Diagnostics surfaced on /settings (read-only).
	LogLevelEnv       string // MINER_LOG_LEVEL value (config.Load)
	BrowserURLDisplay string // MINER_BROWSER_URL value (config.Load)
	GitCommit         string // build-time git commit short hash
	Version           string // semver / release tag
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// Static assets shipped via internal/web/static/. Cache aggressively
	// since file names change on rebuild.
	r.Handle("/static/*", http.StripPrefix("/static/", staticHandler()))

	if d.Session == nil {
		// Skeleton mode (used by TestHealthz) — no business routes.
		return r
	}

	setup := setupDeps{q: d.Q, t: d.Templates, sm: d.Session}
	authH := authDeps{q: d.Q, t: d.Templates, sm: d.Session}
	startedAt := d.StartTime
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	dash := dashboardDeps{
		q:               d.Q,
		t:               d.Templates,
		sm:              d.Session,
		sch:             d.Scheduler,
		ring:            d.LogRing,
		start:           startedAt,
		channelCounters: channelCountersFromRegistry(d.Registry),
	}
	accs := accountsDeps{q: d.Q, t: d.Templates, sm: d.Session, sch: d.Scheduler, reload: d.Reload}
	loginTwitch := newLoginTwitchDeps(d, d.RootCtx)
	loginKick := &loginKickDeps{
		q:         d.Q,
		t:         d.Templates,
		sm:        d.Session,
		sessions:  d.Sessions,
		browser:   d.BrowserClient,
		registrar: d.Registrar,
		reload:    d.Reload,
	}
	// loginTwitchPaste retired: Twitch cookie-paste no longer authenticates
	// (web-issued token vs Android client_id). Twitch login is device-code
	// only; the /twitch/paste routes redirect to /twitch/device.

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
	authed.Get("/dashboard/events", dash.events)
	authed.Get("/dashboard/campaign/{id}", dash.campaignDetail)
	authed.Get("/dashboard/account/{id}", dash.accountDetail)
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
			// Twitch is device-code only. Cookie-paste gives a web-issued
			// auth-token, which fails against the Android client_id our
			// direct backend uses (currentUser:null / integrity wall).
			// Device-code mints the Android-issued token DevilXD relies on.
			loginTwitch.get(w, r)
		case "kick":
			loginKick.get(w, r)
		default:
			http.Error(w, "platform does not need login", http.StatusBadRequest)
		}
	})
	// Twitch cookie-paste is retired (doesn't authenticate on the direct
	// backend). Redirect any old paste links to the device-code flow.
	authed.Get("/accounts/{id}/twitch/paste", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/accounts/"+chi.URLParam(r, "id")+"/twitch/device", http.StatusSeeOther)
	})
	authed.Post("/accounts/{id}/twitch/paste", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/accounts/"+chi.URLParam(r, "id")+"/twitch/device", http.StatusSeeOther)
	})
	authed.Get("/accounts/{id}/twitch/device", loginTwitch.get)
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
	authed.Post("/accounts/{id}/games", accs.games)
	authed.Post("/accounts/{id}/games/add", accs.addGame)
	authed.Post("/accounts/{id}/games/use-global", accs.useGlobal)
	authed.Post("/accounts/{id}/delete", accs.delete)
	authed.Post("/accounts/apply", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Count enabled accounts so the flash message is actually
		// informative; a query failure is non-fatal — fall back to
		// the bare success string.
		msg := "watchers reloaded"
		if accs, err := d.Q.ListEnabledAccounts(r.Context()); err == nil {
			msg = fmt.Sprintf("watchers reloaded — %d enabled accounts", len(accs))
		}
		d.Session.Put(r.Context(), "flash", msg)
		// Redirect back to where the user clicked Reload from
		// instead of always bouncing to /accounts. The dashboard
		// also has a Reload button, and bouncing the user off the
		// dashboard each time is jarring.
		http.Redirect(w, r, applyRedirectTarget(r), http.StatusSeeOther)
	})
	authed.Post("/logout", authH.logoutPost)

	settingsH := &settingsDeps{
		s:           d.SettingsStore,
		q:           d.Q,
		sch:         d.Scheduler,
		t:           d.Templates,
		sm:          d.Session,
		onUpdate:    d.OnSettingsUpdate,
		reload:      d.Reload,
		startedAt:   startedAt,
		logLevelEnv: d.LogLevelEnv,
		browserURL:  d.BrowserURLDisplay,
		gitCommit:   d.GitCommit,
		version:     d.Version,
	}
	dropsH := &dropsDeps{q: d.Q, t: d.Templates, reload: d.Reload}
	historyH := &historyDeps{q: d.Q, ring: d.LogRing, t: d.Templates}

	authed.Get("/settings", settingsH.get)
	authed.Post("/settings", settingsH.post)
	authed.Post("/settings/global-games", settingsH.globalGamesPost)
	authed.Post("/settings/global-games/add", settingsH.globalGamesAdd)
	authed.Get("/drops", dropsH.list)
	authed.Get("/drops/campaigns/{id}/items", dropsH.items)
	authed.Post("/drops/whitelist/add", dropsH.addWhitelist)
	authed.Post("/drops/link", dropsH.markLinked)
	authed.Get("/history", historyH.get)

	r.Mount("/", withSession(CSRF(authed)))
	return r
}

// staticHandler serves the embedded static/ assets. CSS/JS use no-cache
// (revalidate every load — cheap 304s) so a redeploy's style/script changes
// show immediately rather than going stale for up to the cache TTL — which
// caused new HTML to render against an old cached app.css. Other assets
// (images/fonts) keep a short TTL.
func staticHandler() http.Handler {
	fs := http.FileServer(http.FS(web.Static()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		fs.ServeHTTP(w, r)
	})
}
