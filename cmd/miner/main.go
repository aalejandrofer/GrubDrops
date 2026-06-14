package main

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/api"
	"github.com/aalejandrofer/grubdrops/internal/auth/browser"
	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/config"
	"github.com/aalejandrofer/grubdrops/internal/discovery"
	"github.com/aalejandrofer/grubdrops/internal/dockerctl"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/kick"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// version is the release tag, injected at build time via
// -ldflags "-X main.version=<tag>" (see deploy/Dockerfile.miner + release.yml).
// It auto-tracks the git tag of each released image; the GRUB_VERSION env var
// is only a fallback for source/dev builds where no ldflag is set.
var version string

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ring := mlog.NewRingFromEnv(1000)
	logger := mlog.NewWithRing(os.Stdout, cfg.LogLevel, ring)
	slog.SetDefault(logger)
	startTime := time.Now()
	logger.Info("miner starting", "log_level", cfg.LogLevel, "http_addr", cfg.HTTPAddr, "db_path", cfg.DBPath, "browser_url", cfg.BrowserURL, "secure_cookies", cfg.SecureCookies)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	cryptor, err := store.NewCryptor(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("master key invalid: %w", err)
	}
	_ = cryptor

	q := gen.New(db)
	settingsStore := store.NewSettings(q)
	sessions := store.NewSessionStore(db, q, cryptor)

	tmplSet, err := web.Templates()
	if err != nil {
		return fmt.Errorf("load templates: %w", err)
	}

	sm := scs.New()
	sm.Store = api.NewKVSessionStore(db)
	sm.Lifetime = 12 * time.Hour
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteStrictMode
	sm.Cookie.Secure = cfg.SecureCookies

	oidcProvider, oidcErr := oidc.New(ctx, oidc.Config{
		Issuer:        cfg.OIDCIssuer,
		ClientID:      cfg.OIDCClientID,
		ClientSecret:  cfg.OIDCClientSecret,
		RedirectURL:   cfg.OIDCRedirectURL,
		ProviderName:  cfg.OIDCProviderName,
		AllowedEmails: cfg.OIDCAllowedEmails,
		AllowedGroups: cfg.OIDCAllowedGroups,
	})
	if oidcErr != nil {
		// IdP unreachable at boot: log and continue with OIDC disabled so the
		// miner still starts and password login keeps working.
		logger.Warn("oidc disabled: discovery failed", "err", oidcErr)
		oidcProvider, _ = oidc.New(ctx, oidc.Config{}) // disabled provider
	}
	if oidcProvider.Enabled() {
		logger.Info("oidc enabled", "issuer", cfg.OIDCIssuer, "provider", oidcProvider.Name())
		// Open-by-default footgun: with no allowlist, ANY user the IdP
		// authenticates becomes admin. Warn so operators notice.
		if len(cfg.OIDCAllowedEmails) == 0 && len(cfg.OIDCAllowedGroups) == 0 {
			logger.Warn("oidc has no email/group allowlist: any user authenticated by the IdP will gain admin access")
		}
	}

	registry := platform.NewRegistry()

	var browserClient *browser.Client
	var kickBackend *kick.Backend
	twitchBrowserEnabled := os.Getenv("GRUB_TWITCH_BROWSER") == "1"
	if len(cfg.BrowserURLs) > 0 {
		bc, err := browser.Dial(cfg.BrowserURLs[0])
		if err != nil {
			return fmt.Errorf("dial browser sidecar %q: %w", cfg.BrowserURLs[0], err)
		}
		defer bc.Close()
		browserClient = bc // login / Twitch / display client
	}
	logger.Info("browser login client dialed", "configured", browserClient != nil, "urls", cfg.BrowserURLs)
	// On-demand Kick sidecars: control per-account chromedp containers over the
	// host docker socket so each account's Chrome only runs when actively
	// watching. Degrade to always-on if the socket is unreachable.
	var dockerCtl dockerctl.Controller
	if dc, err := dockerctl.New(); err != nil {
		logger.Warn("docker control unavailable; kick sidecars stay always-on", "err", err)
	} else {
		dockerCtl = dc
		logger.Info("docker control enabled for on-demand kick sidecars")
	}
	// Kick runs over the pure-HTTP utls transport (Chrome TLS fingerprint) and
	// no longer needs the chromedp sidecar for data — Kick's API 403s any
	// CDP browser but accepts utls. The browser client (may be nil) is kept
	// only for the legacy login path; watch sidecars are derived per-account.
	// When the docker socket is reachable, auto-create per-account browser
	// sidecars (pull + create + start, labelled, on the miner's network) so the
	// default compose can be JUST the miner + docker.sock — no hand-defined
	// browser services. Degrades to start-only of existing containers when the
	// controller is nil (socket unreachable).
	var kickOpts []kick.Option
	if dockerCtl != nil {
		kickOpts = append(kickOpts, kick.WithSidecarAutoCreate(cfg.KickSidecarImage, cfg.KickSidecarNetwork))
		logger.Info("kick sidecar auto-create enabled", "image", cfg.KickSidecarImage, "network", cfg.KickSidecarNetwork)
	}
	kickBackend = kick.New(browserClient, dockerCtl, cfg.KickSidecarTemplate, cfg.KickSidecarPort, 10*time.Minute, kickOpts...)
	// Watch path is operator-selectable (Settings → Experimental). "browser"
	// (default) drives a real IVS <video> in the sidecar. "ws" is the
	// experimental pure-WebSocket path (no browser): leaving browser-watch OFF
	// routes StartWatch to the WebSocket watch (live-verified to accrue — see
	// internal/platform/kick/wswatch.go). The two are mutually exclusive (one
	// active watch per account). EnableBrowserWatch no-ops + warns if no sidecar
	// client is configured.
	kickWatchMode, _ := settingsStore.KickWatchMode(ctx)
	switch kickWatchMode {
	case store.KickWatchModeWS:
		// pure WebSocket, no browser — nothing to enable.
	case store.KickWatchModeAuto:
		kickBackend.EnableAutoWatch() // WS first, Chrome fallback on WS death
	default:
		kickBackend.EnableBrowserWatch()
	}
	registry.Register(kickBackend)
	logger.Info("kick backend enabled (utls HTTP transport)",
		"sidecar", browserClient != nil,
		"watch_mode", kickWatchMode,
		"browser_watch", kickWatchMode == store.KickWatchModeBrowser && browserClient != nil)

	if twitchBrowserEnabled && browserClient != nil {
		registry.Register(twitch.NewBrowserBackend(browserClient))
		logger.Info("twitch backend: BROWSER (via sidecar)")
	} else {
		registry.Register(twitch.New())
		logger.Info("twitch backend: direct HTTP (Android device-code, no integrity header)")
	}

	resolver := func(accountID string) string {
		acc, err := q.GetAccount(ctx, accountID)
		if err != nil || !acc.WebhookUrl.Valid {
			return ""
		}
		return acc.WebhookUrl.String
	}

	buildNotifier := func() notify.Notifier {
		// Build the per-kind filter from the saved toggles every time so
		// toggling CLAIMS/PROGRESS/AUTH/ERRORS on /settings + Save actually
		// takes effect (previously the filter was hardcoded all-true, so
		// PROGRESS fired regardless of the checkbox). State events are
		// never sent to Discord (too noisy).
		claim, progress, auth, errs := settingsStore.NotifyKinds(ctx)
		filter := &notify.VerbosityFilter{Allow: map[string]bool{
			notify.EventClaim:    claim,
			notify.EventProgress: progress,
			notify.EventAuth:     auth,
			notify.EventError:    errs,
			notify.EventState:    false,
			// Manual "send test" always delivers, regardless of toggles.
			notify.EventTest: true,
		}}
		globalURL, _ := settingsStore.GlobalDiscordWebhook(ctx)
		if globalURL == "" {
			globalURL = cfg.DiscordWebhookURL
		}
		avatarURL, _ := settingsStore.NotifyAvatarURL(ctx)
		const botUsername = "GrubDrops"
		var fallback notify.Notifier
		if globalURL != "" {
			wh := notify.NewDiscordWebhook(globalURL, filter)
			wh.Username = botUsername
			wh.AvatarURL = avatarURL
			fallback = wh
		} else {
			fallback = &notify.NoopNotifier{Logger: logger}
		}
		routed := notify.NewAccountRouted(fallback, resolver, filter)
		routed.Username = botUsername
		routed.AvatarURL = avatarURL
		return routed
	}

	var notifierMu sync.Mutex
	currentNotifier := buildNotifier()

	onSettingsUpdate := func() {
		n := buildNotifier()
		notifierMu.Lock()
		currentNotifier = n
		notifierMu.Unlock()
	}

	notifier := &indirectNotifier{mu: &notifierMu, ptr: &currentNotifier}

	sched := scheduler.New(scheduler.Options{Notifier: notifier})

	// Persister writes every whitelisted campaign the watcher discovers
	// into the campaigns table so the /drops page can render past +
	// current + upcoming tabs. Shared by every watcher.
	campaignPersister := store.NewCampaignPersister(q)
	// ClaimRecorder persists a claims row after each successful
	// Backend.Claim. Without it InsertClaim has no production caller
	// and the /drops Past + /history views stay empty.
	claimRecorder := store.NewClaimRecorder(q)

	// Per-account direct-Twitch backends. The direct twitch.Backend holds
	// per-account state (auth, userID/userLogin caches, its own PubSub
	// socket), so sharing ONE instance across accounts races their tokens
	// and caches together (audit P0). Give each Twitch account its own
	// instance, cached across Reloads so we don't leak a PubSub goroutine
	// per reload. Kick + the browser BrowserBackend are already
	// multi-account-aware (per-account maps), so they keep using the
	// shared registry instance. The registry's twitch instance stays in
	// use by the discovery scraper + dashboard channel counters only.
	browserActive := twitchBrowserEnabled && browserClient != nil
	twitchBackends := map[string]*twitch.Backend{}
	var twitchBackendsMu sync.Mutex
	backendFor := func(a gen.Account) (platform.Backend, bool) {
		if a.Platform == "twitch" && !browserActive {
			twitchBackendsMu.Lock()
			defer twitchBackendsMu.Unlock()
			if bk, ok := twitchBackends[a.ID]; ok {
				return bk, true
			}
			bk := twitch.New()
			twitchBackends[a.ID] = bk
			return bk, true
		}
		return registry.Get(a.Platform)
	}

	build := func(a gen.Account) (scheduler.Entry, error) {
		b, ok := backendFor(a)
		if !ok {
			return scheduler.Entry{}, fmt.Errorf("no backend for platform %q", a.Platform)
		}

		var sess platform.Session
		{
			s, ok, err := sessions.Get(ctx, a.ID)
			if err != nil {
				return scheduler.Entry{}, fmt.Errorf("load session: %w", err)
			}
			if !ok {
				logger.Warn("account has no session, will idle until re-auth",
					"account", a.ID, "platform", a.Platform)
				return scheduler.NewEntry(a.ID, nopRunner{}), nil
			}
			if s.ExpiresAt.Before(time.Now()) {
				if s.RefreshToken == "" {
					logger.Warn("session expired and no refresh token, will idle",
						"account", a.ID, "platform", a.Platform)
					return scheduler.NewEntry(a.ID, nopRunner{}), nil
				}
				refreshed, err := b.RefreshSession(ctx, s)
				if err != nil {
					logger.Warn("session refresh failed, will idle",
						"account", a.ID, "platform", a.Platform, "err", err)
					return scheduler.NewEntry(a.ID, nopRunner{}), nil
				}
				if err := sessions.Put(ctx, a.ID, refreshed); err != nil {
					logger.Warn("persist refreshed session failed",
						"account", a.ID, "err", err)
					return scheduler.NewEntry(a.ID, nopRunner{}), nil
				}
				logger.Info("session refreshed", "account", a.ID, "platform", a.Platform)
				s = refreshed
			}
			sess = s
		}

		// Re-register Kick channels from the persisted session blob so
		// the in-memory channelsByAcc map survives daemon restarts. The
		// browser login handler also calls this, but boot-time reload
		// is required because the map is purely in-memory.
		if a.Platform == "kick" && kickBackend != nil {
			// Map the account to its username-derived sidecar so StartWatch
			// can start the right container on demand. Runs every Reload, so
			// a freshly-added account is registered without a daemon restart.
			kickBackend.RegisterSidecar(a.ID, a.DisplayName)
			if chs := decodeKickChannels(sess); len(chs) > 0 {
				kickBackend.RegisterChannels(a.ID, chs)
				logger.Info("kick channels restored from session", "account", a.ID, "channels", chs)
			}
		}

		allow, rank, err := loadAccountWhitelist(ctx, q, a.ID)
		if err != nil {
			logger.Warn("load account whitelist failed; mining nothing until fixed",
				"account", a.ID, "err", err)
			return scheduler.NewEntry(a.ID, nopRunner{}), nil
		}
		if !hasAnyGame(allow) {
			logger.Info("account has empty game whitelist, idle until games are picked",
				"account", a.ID)
			return scheduler.NewEntry(a.ID, nopRunner{}), nil
		}

		// PriorityMode is a global setting — read once per build (i.e.
		// per Reload). Watcher snapshots the value for the lifetime of
		// the entry; the next Reload picks up changes.
		priorityMode, _ := settingsStore.PriorityMode(ctx)

		// Runtime cadence + progress-notify granularity — read per build (per
		// Reload) so saving on /settings + reloading takes effect.
		tickSec, _ := settingsStore.TickIntervalSec(ctx)
		progressStep, _ := settingsStore.ProgressNotifyStepPct(ctx)

		// Manual "I've linked it" overrides — campaign ids the user asserted
		// are account-linked. Loaded per build (per Reload) so toggling +
		// reloading takes effect. See ForceLinked in watcher.Config.
		forceLinked := loadLinkOverrides(ctx, q)

		acctLabel := a.DisplayName
		w := watcher.New(watcher.Config{
			AccountID: a.ID, AccountLabel: acctLabel, Platform: a.Platform,
			Backend: b, Session: sess,
			Notifier: notifier, TickInterval: time.Duration(tickSec) * time.Second,
			// LOCKED to 60s, not user-tunable: the watch-ping beacon cadence is
			// derived from HeartbeatInterval, and Twitch credits exactly 1 minute
			// per beacon — any value >60s under-credits Twitch watch-time (120s =>
			// 0.5 min/real-min, measured 2026-06-12). See watcher.heartbeatEveryTicks.
			HeartbeatInterval:     60 * time.Second,
			ProgressNotifyStepPct: progressStep,
			AllowGame:             allow, GameRank: rank,
			PriorityMode:  priorityMode,
			Persister:     campaignPersister,
			ClaimRecorder: claimRecorder,
			ForceLinked:   forceLinked,
		})
		return scheduler.NewEntry(a.ID, w), nil
	}

	loadAndStart := func(parent context.Context) error {
		accs, err := q.ListEnabledAccounts(parent)
		if err != nil {
			return err
		}
		builders := make([]scheduler.EntryBuilder, 0, len(accs))
		for _, a := range accs {
			a := a
			builders = append(builders, func() scheduler.Entry {
				e, err := build(a)
				if err != nil {
					logger.Error("account skipped", "account", a.ID, "err", err)
					return scheduler.NewEntry(a.ID, nopRunner{})
				}
				return e
			})
		}
		return sched.Reload(parent, builders)
	}

	// reloadAccount restarts a SINGLE account's watcher (targeted account
	// edit) without touching the rest of the roster.
	reloadAccount := func(parent context.Context, accountID string) {
		acc, err := q.GetAccount(parent, accountID)
		if err != nil {
			logger.Warn("reloadAccount: account not found", "account", accountID, "err", err)
			return
		}
		sched.ReloadAccount(parent, accountID, func() scheduler.Entry {
			e, err := build(acc)
			if err != nil {
				logger.Error("account skipped on targeted reload", "account", accountID, "err", err)
				return scheduler.NewEntry(accountID, nopRunner{})
			}
			return e
		})
	}

	if err := loadAndStart(ctx); err != nil {
		return fmt.Errorf("initial scheduler boot: %w", err)
	}

	// Now that the initial Reload has registered every enabled Kick account's
	// sidecar, sweep away any auto-created (grubdrops.managed=true) browser
	// container whose account is gone. Unlabeled hand-defined sidecars are
	// never listed by the sweep, so they survive untouched. The periodic
	// reaper repeats this every minute (covers account deletions at runtime).
	if kickBackend != nil {
		kickBackend.SweepSidecars(ctx)
	}

	// Anonymous active-drops scraper: keeps the /drops page populated
	// with every active whitelisted campaign even when no watcher has
	// ticked recently (e.g. all accounts disabled, sessions expired).
	// Providers borrow ONE shared session per platform — the first
	// enabled account's — so we don't multiply the gql cost across the
	// account roster. Whitelist is the union of every enabled account's
	// game opt-ins; non-whitelisted games are never scraped.
	// Cadence comes from the /settings DB value (minutes); the env var,
	// when set, overrides it (ops escape hatch).
	discMin, _ := settingsStore.DiscoveryIntervalMin(ctx)
	discoveryInterval := parseDuration(os.Getenv("GRUB_DISCOVERY_INTERVAL"), time.Duration(discMin)*time.Minute)
	startDiscovery(ctx, logger, q, sessions, registry, campaignPersister, discoveryInterval)

	// Auth-health sweep: probe each account's auth (Twitch token / Kick
	// cookies) on a long cadence so the operator sees a "needs re-auth"
	// flag before an account silently stops mining. CheckAll is also wired
	// to a manual button on /accounts.
	authChecker := authcheck.New(q, sessions, registry)
	authInterval := parseDuration(os.Getenv("GRUB_AUTHCHECK_INTERVAL"), time.Hour)
	go authChecker.Run(ctx, authInterval)

	// Avoid typed-nil-interface trap: only assign if the concrete pointer is non-nil.
	var bc api.KickBrowserClient
	if browserClient != nil {
		bc = browserClient // *browser.Client satisfies KickBrowserClient
	}
	// The Kick registrar/verifier is the utls backend — always wire it
	// (independent of the browser sidecar) so Kick login can register
	// channels + verify cookies over utls with no sidecar.
	var reg api.KickChannelRegistrar
	if kickBackend != nil {
		reg = kickBackend // *kick.Backend: RegisterChannels + VerifyAuth
	}

	deps := api.Deps{
		DB: db, Q: q, Templates: tmplSet, Session: sm,
		Scheduler: sched, Reload: loadAndStart,
		Sessions: sessions, Registry: registry,
		RootCtx:           ctx,
		BrowserClient:     bc,
		Registrar:         reg,
		SettingsStore:     settingsStore,
		OnSettingsUpdate:  onSettingsUpdate,
		Notifier:          notifier,
		AuthCheck:         authChecker.CheckAll,
		ReloadAccount:     reloadAccount,
		TwitchBrowser:     twitchBrowserEnabled && browserClient != nil,
		LogRing:           ring,
		StartTime:         startTime,
		LogLevelEnv:       cfg.LogLevel,
		BrowserURLDisplay: cfg.BrowserURL,
		KickSidecars:      kickSidecarLister(kickBackend),
		KickActivePath:    kickActivePathFn(kickBackend),
		GitCommit:         os.Getenv("GIT_COMMIT"),
		Version:           cmp.Or(version, os.Getenv("GRUB_VERSION")),
		OIDC:              oidcProvider,
		SecureCookies:     cfg.SecureCookies,
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	sched.Stop(shutdownCtx)
	return nil
}

type nopRunner struct{}

func (nopRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

// loadLinkOverrides reads the manual "I've linked it" campaign overrides
// from kv (keys prefixed store.LinkOverridePrefix) and returns a membership
// predicate. Errors degrade to "no overrides" — the gate just stays on.
func loadLinkOverrides(ctx context.Context, q *gen.Queries) func(campaignID string) bool {
	set := map[string]bool{}
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.LinkOverridePrefix, Valid: true})
	if err == nil {
		for _, kv := range rows {
			if string(kv.Value) != "1" {
				continue
			}
			set[strings.TrimPrefix(kv.Key, store.LinkOverridePrefix)] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return func(campaignID string) bool { return set[campaignID] }
}

type indirectNotifier struct {
	mu  *sync.Mutex
	ptr *notify.Notifier
}

func (i *indirectNotifier) Notify(ctx context.Context, event string, fields map[string]any) error {
	i.mu.Lock()
	n := *i.ptr
	i.mu.Unlock()
	return n.Notify(ctx, event, fields)
}

// decodeKickChannels pulls the channel list out of a stored Kick
// session blob. Prefers the new "channels" array but falls back to the
// legacy "channel" string for back-compat with sessions written before
// multi-channel support. Also handles the transitional case where
// pre-upgrade clients posted "channel=a,b" against an old server — the
// stored Channel string may contain commas/spaces. Returns nil for
// non-Kick sessions or when no channels were stored.
func decodeKickChannels(s platform.Session) []string {
	blob := s.Cookies["kick"]
	if blob == "" {
		return nil
	}
	var stored struct {
		Channel  string   `json:"channel"`
		Channels []string `json:"channels"`
	}
	if err := json.Unmarshal([]byte(blob), &stored); err != nil {
		return nil
	}
	if len(stored.Channels) > 0 {
		return stored.Channels
	}
	if stored.Channel == "" {
		return nil
	}
	// Legacy "channel" field may be a single name or a comma/space
	// list pushed by a newer client to an older server.
	splitter := func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r', ';':
			return true
		}
		return false
	}
	parts := strings.FieldsFunc(stored.Channel, splitter)
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loadAccountWhitelist materialises the per-account game allow-list
// into match + rank closures the watcher consumes. When the account
// has NO rows of its own, falls back to the global priority list
// (settings → Global priority). Returns nil closures only when BOTH
// the account-specific and global lists are empty.
func loadAccountWhitelist(ctx context.Context, q *gen.Queries, accountID string) (func(string) bool, func(string) int, error) {
	rows, err := q.ListAccountGames(ctx, accountID)
	if err != nil {
		return nil, nil, fmt.Errorf("list account games: %w", err)
	}
	if len(rows) == 0 {
		// Fall back to global priority list when account has no
		// override. The data shape matches ListAccountGames so we
		// reuse the rest of the function.
		gRows, gErr := q.ListGlobalGames(ctx)
		if gErr != nil {
			return nil, nil, fmt.Errorf("list global games: %w", gErr)
		}
		if len(gRows) == 0 {
			return nil, nil, nil
		}
		rows = make([]gen.ListAccountGamesRow, len(gRows))
		for i, r := range gRows {
			rows[i] = gen.ListAccountGamesRow{ID: r.ID, Name: r.Name, Slug: r.Slug, Rank: r.Rank}
		}
	}
	// game.name (lowercased) -> rank
	rankByName := make(map[string]int, len(rows))
	// game.slug -> rank, in case backends report by slug
	rankBySlug := make(map[string]int, len(rows))
	for _, r := range rows {
		rankByName[strings.ToLower(r.Name)] = int(r.Rank)
		rankBySlug[r.Slug] = int(r.Rank)
	}
	allow := func(game string) bool {
		g := strings.ToLower(game)
		if _, ok := rankByName[g]; ok {
			return true
		}
		_, ok := rankBySlug[g]
		return ok
	}
	rank := func(game string) int {
		g := strings.ToLower(game)
		if r, ok := rankByName[g]; ok {
			return r
		}
		if r, ok := rankBySlug[g]; ok {
			return r
		}
		return 1 << 30
	}
	return allow, rank, nil
}

func hasAnyGame(allow func(string) bool) bool {
	return allow != nil
}

// parseDuration parses a Go duration string (e.g. "5m", "30s") with a
// fallback when the env var is empty or malformed. Matches the pattern
// used elsewhere in the codebase for config-by-env.
func parseDuration(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// startDiscovery wires the anonymous active-drops scraper. Providers
// are added only when the prerequisite backend is registered; a missing
// twitch/kick backend means that Provider is silently omitted (the
// remaining ones still run).
//
// Falls back to a graceful no-op for each platform when:
//   - the backend isn't registered (sidecar absent for Kick, etc.), OR
//   - no enabled account on that platform has a usable session.
//
// In either case a single warning is logged and Run returns without
// touching the persister. The persister is the same one the watchers
// use, so duplicate writes are harmless (UPSERT).
func startDiscovery(
	ctx context.Context,
	logger *slog.Logger,
	q *gen.Queries,
	sessions *store.SessionStore,
	registry *platform.Registry,
	persister *store.CampaignPersister,
	interval time.Duration,
) {
	providers := make([]discovery.Provider, 0, 2)
	if b, ok := registry.Get("twitch"); ok {
		providers = append(providers, discovery.NewTwitchScraperFromStore(q, sessions, b))
	} else {
		logger.Warn("discovery: no twitch backend registered; skipping twitch scraper")
	}
	if b, ok := registry.Get("kick"); ok {
		providers = append(providers, discovery.NewKickScraperFromStore(q, sessions, b))
	} else {
		logger.Warn("discovery: no kick backend registered (GRUB_BROWSER_URL empty?); skipping kick scraper")
	}
	if len(providers) == 0 {
		logger.Warn("discovery: no providers configured; /drops page will only show watcher-discovered campaigns")
		return
	}
	scraper := discovery.New(persister, discovery.NewQueriesWhitelist(q), providers...)
	logger.Info("discovery scraper started", "interval", interval, "providers", len(providers))
	go scraper.Run(ctx, interval)
}

// kickSidecarLister returns a closure the Status panel calls to list the
// per-account Kick sidecar addresses, or nil when no Kick backend is wired.
func kickSidecarLister(b *kick.Backend) func() []string {
	if b == nil {
		return nil
	}
	return b.SidecarAddrs
}

// kickActivePathFn returns a closure the dashboard calls to tag each Kick row
// with its live watch path ("ws"|"chrome"), or nil when no Kick backend exists.
func kickActivePathFn(b *kick.Backend) func(string) string {
	if b == nil {
		return nil
	}
	return b.ActiveWatchPath
}
