package main

import (
	"context"
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

	"github.com/aalejandrofer/dropsminer/internal/api"
	"github.com/aalejandrofer/dropsminer/internal/auth/browser"
	"github.com/aalejandrofer/dropsminer/internal/config"
	"github.com/aalejandrofer/dropsminer/internal/discovery"
	mlog "github.com/aalejandrofer/dropsminer/internal/log"
	"github.com/aalejandrofer/dropsminer/internal/notify"
	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/platform/kick"
	"github.com/aalejandrofer/dropsminer/internal/platform/twitch"
	"github.com/aalejandrofer/dropsminer/internal/scheduler"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
	"github.com/aalejandrofer/dropsminer/internal/watcher"
	"github.com/aalejandrofer/dropsminer/internal/web"
)

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

	registry := platform.NewRegistry()

	var browserClient *browser.Client
	var kickBackend *kick.Backend
	twitchBrowserEnabled := os.Getenv("MINER_TWITCH_BROWSER") == "1"
	if cfg.BrowserURL != "" {
		bc, err := browser.Dial(cfg.BrowserURL)
		if err != nil {
			return fmt.Errorf("dial browser sidecar: %w", err)
		}
		defer bc.Close()
		browserClient = bc
	}
	// Kick runs over the pure-HTTP utls transport (Chrome TLS fingerprint) and
	// no longer needs the chromedp sidecar for data — Kick's API 403s any
	// CDP browser but accepts utls. Register it regardless of MINER_BROWSER_URL;
	// the browser client (may be nil) is kept only for the legacy login path.
	kickBackend = kick.New(browserClient)
	registry.Register(kickBackend)
	logger.Info("kick backend enabled (utls HTTP transport)", "sidecar", browserClient != nil)

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

	filter := &notify.VerbosityFilter{Allow: map[string]bool{
		notify.EventClaim:    true,
		notify.EventError:    true,
		notify.EventProgress: true,
		notify.EventAuth:     true,
	}}

	buildNotifier := func() notify.Notifier {
		globalURL, _ := settingsStore.GlobalDiscordWebhook(ctx)
		if globalURL == "" {
			globalURL = cfg.DiscordWebhookURL
		}
		var fallback notify.Notifier
		if globalURL != "" {
			fallback = notify.NewDiscordWebhook(globalURL, filter)
		} else {
			fallback = &notify.NoopNotifier{Logger: logger}
		}
		return notify.NewAccountRouted(fallback, resolver, filter)
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

		w := watcher.New(watcher.Config{
			AccountID: a.ID, Backend: b, Session: sess,
			Notifier: notifier, TickInterval: 500 * time.Millisecond,
			AllowGame: allow, GameRank: rank,
			PriorityMode:  priorityMode,
			Persister:     campaignPersister,
			ClaimRecorder: claimRecorder,
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

	if err := loadAndStart(ctx); err != nil {
		return fmt.Errorf("initial scheduler boot: %w", err)
	}

	// Anonymous active-drops scraper: keeps the /drops page populated
	// with every active whitelisted campaign even when no watcher has
	// ticked recently (e.g. all accounts disabled, sessions expired).
	// Providers borrow ONE shared session per platform — the first
	// enabled account's — so we don't multiply the gql cost across the
	// account roster. Whitelist is the union of every enabled account's
	// game opt-ins; non-whitelisted games are never scraped.
	discoveryInterval := parseDuration(os.Getenv("MINER_DISCOVERY_INTERVAL"), 5*time.Minute)
	startDiscovery(ctx, logger, q, sessions, registry, campaignPersister, discoveryInterval)

	// Avoid typed-nil-interface trap: only assign if the concrete pointer is non-nil.
	var bc api.KickBrowserClient
	var tbc api.TwitchBrowserClient
	var reg api.KickChannelRegistrar
	if browserClient != nil {
		bc = browserClient  // *browser.Client satisfies KickBrowserClient
		tbc = browserClient // *browser.Client satisfies TwitchBrowserClient
		reg = kickBackend   // *kick.Backend satisfies KickChannelRegistrar
	}

	deps := api.Deps{
		DB: db, Q: q, Templates: tmplSet, Session: sm,
		Scheduler: sched, Reload: loadAndStart,
		Sessions: sessions, Registry: registry,
		RootCtx:             ctx,
		BrowserClient:       bc,
		TwitchBrowserClient: tbc,
		Registrar:           reg,
		SettingsStore:       settingsStore,
		OnSettingsUpdate:    onSettingsUpdate,
		TwitchBrowser:       twitchBrowserEnabled && browserClient != nil,
		LogRing:             ring,
		StartTime:           startTime,
		LogLevelEnv:         cfg.LogLevel,
		BrowserURLDisplay:   cfg.BrowserURL,
		GitCommit:           os.Getenv("GIT_COMMIT"),
		Version:             os.Getenv("MINER_VERSION"),
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
		logger.Warn("discovery: no kick backend registered (MINER_BROWSER_URL empty?); skipping kick scraper")
	}
	if len(providers) == 0 {
		logger.Warn("discovery: no providers configured; /drops page will only show watcher-discovered campaigns")
		return
	}
	scraper := discovery.New(persister, discovery.NewQueriesWhitelist(q), providers...)
	logger.Info("discovery scraper started", "interval", interval, "providers", len(providers))
	go scraper.Run(ctx, interval)
}
