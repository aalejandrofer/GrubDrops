package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/i18n"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// spaRoutes are the paths the SPA renders client-side. When the SPA flag is
// on, Go serves the SPA shell (spaIndex) for each so deep-links/refreshes
// work; the client router takes over. Mirrors web/src/lib/router.svelte.ts's
// spaPaths. Grows as pages are ported.
var spaRoutes = []string{"/", "/drops", "/priority", "/settings", "/settings/notifications", "/settings/security", "/settings/health", "/settings/experimental", "/settings/proxy", "/accounts", "/settings/accounts"}

// applyRedirectTarget picks the post-/accounts/apply landing page from
// the Referer header. The dashboard also has a Reload button, so we
// avoid the old behavior of always bouncing the user to /accounts:
//   - if the referer mentions /accounts, return that path (sticks the
//     user on whichever /accounts page they were on);
//   - otherwise, return the referer path so they stay put (e.g.
//     dashboard "/");
//   - fall back to "/" when the header is missing or unparseable.
//
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
	// Reject protocol-relative paths ("//host") — browsers treat them as
	// absolute URLs to another origin, so honoring one would be an open
	// redirect even though the host was stripped above.
	if strings.HasPrefix(target, "//") {
		return "/"
	}
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return target
}

// Notifier fires a Discord notification. Matches notify.Notifier; kept as a
// local interface so the api package stays decoupled from internal/notify.
// Used by the /settings "send test" button.
type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

type Deps struct {
	DB               *sql.DB
	Q                *gen.Queries
	Templates        Renderer
	Session          *scs.SessionManager
	Scheduler        *scheduler.Scheduler
	Reload           func(context.Context) error
	Sessions         *store.SessionStore
	Registry         *platform.Registry
	RootCtx          context.Context
	BrowserClient    KickBrowserClient
	Registrar        KickChannelRegistrar
	SettingsStore    *store.Settings
	OnSettingsUpdate func()
	// Location is the timezone for displayed times (from TZ env var).
	Location *time.Location
	// OIDC is the configured single-sign-on provider. Nil or disabled means
	// the SSO button is hidden and the /auth/oidc/* routes redirect to /login.
	OIDC *oidc.Provider
	// SecureCookies mirrors config.SecureCookies for the transient OIDC
	// state cookie (the scs session cookie is configured in cmd/miner).
	SecureCookies bool
	// Notifier is the live Discord notifier, used by the /settings "send
	// test" button. Nil disables the button.
	Notifier Notifier
	// AuthCheck runs the auth-health sweep across all accounts (manual
	// "check auth now" button on /accounts). Nil disables the button.
	AuthCheck func(context.Context)
	// RunCanary triggers an immediate canary RunOnce. Nil means the
	// Run-now button on the Health tab is still present but does nothing.
	RunCanary func(context.Context) error
	// ReloadAccount restarts a single account's watcher (targeted account
	// edit) without reloading the whole roster.
	ReloadAccount func(context.Context, string)
	// TwitchBrowser indicates the Twitch backend is the browser-routed
	// variant; the login handler redirects to the cookie-paste page
	// instead of the device-code flow when true.
	TwitchBrowser bool
	LogRing       *mlog.Ring
	StartTime     time.Time

	// Diagnostics surfaced on /settings (read-only).
	LogLevelEnv       string // GRUB_LOG_LEVEL value (config.Load)
	BrowserURLDisplay string // GRUB_BROWSER_URL value (config.Load)
	GitCommit         string // build-time git commit short hash
	Version           string // semver / release tag
	// KickSidecars lists the per-account Kick sidecar addresses for the
	// read-only Status panel. Nil when no Kick backend is configured.
	KickSidecars func() []string
	// KickActivePath reports an account's live Kick watch path ("ws"|"chrome")
	// so the dashboard can tag each row. Nil when no Kick backend is configured.
	KickActivePath func(accountID string) string
	// SPADashboard, when true, serves the Svelte SPA at "/" instead of
	// the html/template dashboard. Gated by GRUB_SPA_DASHBOARD (default
	// off) so prod keeps the live HTMX dashboard until the SPA live-state
	// pass lands. Local-dev only for now.
	SPADashboard bool
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

	// SPA build output (content-hashed JS/CSS). Served at /assets/* to
	// match Vite's default assetsDir.
	r.Handle("/assets/*", spaFileServer())

	if d.Session == nil {
		// Skeleton mode (used by TestHealthz) — no business routes.
		return r
	}

	setup := setupDeps{q: d.Q, t: d.Templates, sm: d.Session}
	oidcEnabled := d.OIDC != nil && d.OIDC.Enabled()
	oidcName := ""
	if d.OIDC != nil {
		oidcName = d.OIDC.Name()
	}
	authH := authDeps{q: d.Q, t: d.Templates, sm: d.Session, oidcEnabled: oidcEnabled, oidcProviderName: oidcName}

	oidcH := oidcDeps{
		p:      d.OIDC,
		hs:     oidc.NewHandshakeStore(d.DB),
		sm:     d.Session,
		secure: d.SecureCookies,
	}
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
		s:               d.SettingsStore,
		start:           startedAt,
		loc:             d.Location,
		channelCounters: channelCountersFromRegistry(d.Registry),
		kickPath:        d.KickActivePath,
	}
	accs := accountsDeps{q: d.Q, db: d.DB, t: d.Templates, sm: d.Session, sch: d.Scheduler, reload: d.Reload, authCheck: d.AuthCheck, reloadAccount: d.ReloadAccount, rootCtx: d.RootCtx, loc: d.Location}
	loginTwitch := newLoginTwitchDeps(d, d.RootCtx)
	loginTwitchCookie := newLoginTwitchCookieDeps(d, d.RootCtx)
	loginTwitchChoose := &loginTwitchChooseDeps{q: d.Q, t: d.Templates}
	loginKick := &loginKickDeps{
		q:         d.Q,
		t:         d.Templates,
		sm:        d.Session,
		sessions:  d.Sessions,
		browser:   d.BrowserClient,
		registrar: d.Registrar,
		reload:    d.Reload,
		rootCtx:   d.RootCtx,
		loc:       d.Location,
	}
	// loginTwitchPaste retired: Twitch cookie-paste no longer authenticates
	// (web-issued token vs Android client_id). Twitch login is device-code
	// only; the /twitch/paste routes redirect to /twitch/device.

	withSession := func(h http.Handler) http.Handler { return d.Session.LoadAndSave(h) }
	csrf := CSRF(d.SecureCookies)
	spaSecureCookies = d.SecureCookies

	// Public (no auth required, but still session + CSRF)
	r.Method(http.MethodGet, "/setup", withSession(csrf(http.HandlerFunc(setup.get))))
	r.Method(http.MethodPost, "/setup", withSession(csrf(http.HandlerFunc(setup.post))))
	if d.SPADashboard {
		r.Method(http.MethodGet, "/login", withSession(csrf(http.HandlerFunc(spaIndex))))
	} else {
		r.Method(http.MethodGet, "/login", withSession(csrf(http.HandlerFunc(authH.loginGet))))
	}
	r.Method(http.MethodPost, "/login", withSession(csrf(http.HandlerFunc(authH.loginPost))))
	r.Method(http.MethodGet, "/auth/oidc/login", withSession(http.HandlerFunc(oidcH.loginRedirect)))
	r.Method(http.MethodGet, "/auth/oidc/callback", withSession(http.HandlerFunc(oidcH.callback)))

	// Authed area
	authed := chi.NewRouter()
	authed.Use(RequireAdmin(d.Session))
	if d.SPADashboard {
		for _, p := range spaRoutes {
			authed.Get(p, spaIndex)
		}
	} else {
		authed.Get("/", dash.page)
	}
	authed.Get("/dashboard/cards", dash.cards)
	authed.Get("/dashboard/telemetry", dash.telemetry)
	authed.Get("/dashboard/events", dash.events)
	authed.Get("/dashboard/campaign/{id}", dash.campaignDetail)
	authed.Get("/dashboard/account/{id}", dash.accountDetail)
	if !d.SPADashboard {
		authed.Get("/accounts", accs.list)
		authed.Get("/settings/accounts", accs.list) // canonical path; /accounts kept as alias
	}
	authed.Post("/accounts/check-auth", accs.checkAuth)
	if !d.SPADashboard {
		authed.Get("/accounts/new", accs.newGet)
	} else {
		authed.Get("/accounts/new", spaIndex)
	}
	authed.Post("/accounts/new", accs.newPost)
	if !d.SPADashboard {
		authed.Get("/accounts/{id}", accs.detail)
	} else {
		authed.Get("/accounts/{id}", spaIndex)
	}
	authed.Get("/accounts/{id}/login", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		acc, err := d.Q.GetAccount(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch acc.Platform {
		case "twitch":
			// Show choose page with both device-code and cookie import options
			loginTwitchChoose.get(w, r)
		case "kick":
			loginKick.get(w, r)
		default:
			http.Error(w, i18n.T(i18n.DetectLang(r), "error.platform_no_login"), http.StatusBadRequest)
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
	// New: Twitch cookie file import
	authed.Get("/accounts/{id}/twitch/cookie", loginTwitchCookie.get)
	authed.Post("/accounts/{id}/twitch/cookie", loginTwitchCookie.post)
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
			http.Error(w, i18n.T(i18n.DetectLang(r), "error.platform_no_login_post"), http.StatusBadRequest)
		}
	})
	authed.Post("/accounts/{id}/update", accs.update)
	authed.Post("/accounts/{id}/games", accs.games)
	authed.Post("/accounts/{id}/games/add", accs.addGame)
	authed.Post("/accounts/{id}/games/use-global", accs.useGlobal)
	authed.Post("/accounts/{id}/channels/add", accs.addChannel)
	authed.Post("/accounts/{id}/channels/remove", accs.removeChannel)
	authed.Post("/accounts/{id}/force-channels", accs.forceChannelsReorder)
	authed.Post("/accounts/{id}/force-channels/add", accs.addForceChannel)
	authed.Post("/accounts/{id}/force-channels/remove", accs.removeForceChannel)
	authed.Post("/accounts/{id}/force-watch", accs.forceWatchToggle)
	authed.Post("/accounts/{id}/reload", accs.reloadOne)
	authed.Post("/accounts/{id}/toggle", accs.toggleEnabled)
	authed.Post("/accounts/{id}/delete", accs.delete)
	authed.Post("/accounts/apply", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Count enabled accounts so the flash message is actually
		// informative; a query failure is non-fatal — fall back to
		// the bare success string.
		msg := "flash.watchers_reloaded"
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
		notifier:    d.Notifier,
		runCanary:   d.RunCanary,
		startedAt:   startedAt,
		loc:         d.Location,
		logLevelEnv: d.LogLevelEnv,
		browserURL:  d.BrowserURLDisplay,
		sidecars:    d.KickSidecars,
		gitCommit:   d.GitCommit,
		version:     d.Version,
		oidc:        d.OIDC,
	}
	dropsH := &dropsDeps{q: d.Q, t: d.Templates, reload: d.Reload, sessions: d.Sessions, registry: d.Registry, loc: d.Location, sm: d.Session}
	historyH := &historyDeps{q: d.Q, ring: d.LogRing, t: d.Templates, loc: d.Location}

	if !d.SPADashboard {
		authed.Get("/settings", settingsH.get)
	}
	if !d.SPADashboard {
		authed.Get("/priority", settingsH.getPriority) // top-level nav item; suppressed when SPA serves it
	}
	authed.Get("/settings/priority", settingsH.getPriority) // legacy path, kept so old links/redirects still resolve
	if !d.SPADashboard {
		authed.Get("/settings/notifications", settingsH.getNotifications)
	}
	if !d.SPADashboard {
		authed.Get("/settings/security", settingsH.getSecurity)
	}
	if !d.SPADashboard {
		authed.Get("/settings/experimental", settingsH.getExperimental)
	}
	if !d.SPADashboard {
		authed.Get("/settings/health", settingsH.getHealth)
	}
	authed.Post("/settings", settingsH.postGeneral)
	authed.Post("/settings/priority-mode", settingsH.postPriorityMode)
	authed.Post("/settings/experimental", settingsH.postExperimental)
	authed.Post("/settings/notifications", settingsH.postNotifications)
	authed.Post("/settings/global-games", settingsH.globalGamesPost)
	authed.Post("/settings/global-games/add", settingsH.globalGamesAdd)
	authed.Post("/settings/password", settingsH.changePassword)
	authed.Post("/settings/notify-test", settingsH.notifyTest)
	authed.Post("/settings/canary", settingsH.canarySave)
	authed.Post("/settings/canary/run", settingsH.canaryRun)
	authed.Get("/settings/health/canary-panel", settingsH.canaryPanel)
	if !d.SPADashboard {
		authed.Get("/settings/proxy", settingsH.getProxy)
	}
	authed.Post("/settings/proxy", settingsH.postProxy)
	authed.Post("/settings/proxy/test", settingsH.proxyTest)
	if !d.SPADashboard {
		authed.Get("/drops", dropsH.list)
	}
	authed.Get("/drops/campaigns/{id}/items", dropsH.items)
	authed.Post("/drops/whitelist/add", dropsH.addWhitelist)
	authed.Post("/drops/whitelist/channel", dropsH.addChannelWhitelist)
	authed.Post("/drops/whitelist/channel/remove", dropsH.removeChannelWhitelist)
	authed.Post("/drops/link", dropsH.markLinked)
	imgH := &imageProxyDeps{registry: d.Registry}
	authed.Get("/img/kick", imgH.kick)
	authed.Get("/history", historyH.get)

	// JSON API mount. Admin-gated routes use RequireAdminAPI so an
	// unauthenticated fetch() gets 401 JSON instead of a 302 to /login.
	// POST /api/lang stays public (language switch on the login page).
	apiAuthed := chi.NewRouter()
	apiAuthed.Post("/lang", func(w http.ResponseWriter, r *http.Request) {
		lang := strings.TrimSpace(r.FormValue("lang"))
		if lang == "" {
			lang = "en"
		}
		if lang != "en" && lang != "zh-CN" && lang != "es" {
			lang = "en"
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "lang",
			Value:    lang,
			Path:     "/",
			MaxAge:   365 * 24 * 60 * 60,
			HttpOnly: true,
			Secure:   d.SecureCookies,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, applyRedirectTarget(r), http.StatusSeeOther)
	})
	// Public API routes: reachable pre-auth (outside the RequireAdminAPI group).
	// Mirror the same placement as POST /lang above.
	apiAuthed.Post("/login", http.HandlerFunc(authH.apiLogin))
	apiAuthed.Get("/auth/info", apiAuthInfo(authInfoDeps{oidcEnabled: oidcEnabled, oidcProvider: oidcName, secureCookies: d.SecureCookies}))
	apiAuthed.Group(func(gr chi.Router) {
		gr.Use(RequireAdminAPI(d.Session))
		gr.Get("/dashboard", dash.apiPage)
		gr.Get("/dashboard/account/{id}", dash.apiAccountDetail)
		gr.Get("/dashboard/campaign/{id}", dash.apiCampaignDetail)
		gr.Get("/accounts", accs.apiAccounts)
		gr.Post("/accounts/new", accs.apiNewAccount)
		gr.Post("/accounts/check-auth", accs.apiCheckAuth)
		gr.Get("/accounts/{id}", accs.apiAccountDetailPage)
		gr.Post("/accounts/{id}/update", accs.apiUpdateAccount)
		gr.Post("/accounts/{id}/delete", accs.apiDeleteAccount)
		gr.Post("/accounts/{id}/toggle", accs.apiToggle)
		gr.Post("/accounts/{id}/reload", accs.apiReload)
		gr.Post("/accounts/{id}/force-watch", accs.apiForceWatch)
		gr.Post("/accounts/{id}/games", accs.apiAccountGames)
		gr.Post("/accounts/{id}/games/add", accs.apiAccountGameAdd)
		gr.Post("/accounts/{id}/games/use-global", accs.apiAccountGamesUseGlobal)
		gr.Post("/accounts/{id}/channels/add", accs.apiAccountChannelAdd)
		gr.Post("/accounts/{id}/channels/remove", accs.apiAccountChannelRemove)
		gr.Post("/accounts/{id}/force-channels", accs.apiAccountForceChannels)
		gr.Post("/accounts/{id}/force-channels/add", accs.apiAccountForceChannelAdd)
		gr.Post("/accounts/{id}/force-channels/remove", accs.apiAccountForceChannelRemove)
		gr.Post("/accounts/apply", func(w http.ResponseWriter, r *http.Request) {
			if d.Reload == nil {
				writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "reload unavailable")
				return
			}
			if err := d.Reload(r.Context()); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		})
		gr.Get("/drops", dropsH.apiDrops)
		gr.Post("/drops/whitelist/add", dropsH.apiAddWhitelist)
		gr.Post("/drops/whitelist/channel", dropsH.apiAddChannelWhitelist)
		gr.Post("/drops/whitelist/channel/remove", dropsH.apiRemoveChannelWhitelist)
		gr.Post("/drops/link", dropsH.apiMarkLinked)
		gr.Get("/settings", settingsH.apiSettings)
		gr.Post("/settings/general", settingsH.apiGeneral)
		gr.Post("/settings/global-games", settingsH.apiGlobalGamesOrder)
		gr.Post("/settings/global-games/add", settingsH.apiGlobalGamesAdd)
		gr.Post("/settings/priority-mode", settingsH.apiPriorityMode)
		gr.Post("/settings/notifications", settingsH.apiNotifications)
		gr.Post("/settings/experimental", settingsH.apiExperimental)
		gr.Post("/settings/proxy", settingsH.apiProxy)
		gr.Post("/settings/notify-test", settingsH.apiNotifyTest)
		gr.Post("/settings/proxy/test", settingsH.apiProxyTest)
		gr.Post("/settings/password", settingsH.apiChangePassword)
		gr.Post("/settings/canary", settingsH.apiCanary)
		gr.Post("/settings/canary/run", settingsH.apiCanaryRun)
	})
	r.Mount("/api", withSession(csrf(apiAuthed)))

	r.Mount("/", withSession(csrf(authed)))
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
