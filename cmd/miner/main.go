package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"filippo.io/age"
	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/api"
	"github.com/aalejandrofer/grubdrops/internal/auth/browser"
	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/config"
	"github.com/aalejandrofer/grubdrops/internal/dockerctl"
	"github.com/aalejandrofer/grubdrops/internal/i18n"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/netutil"
	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/kick"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/summary"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
	"github.com/aalejandrofer/grubdrops/internal/update"
	"github.com/aalejandrofer/grubdrops/internal/web"
)

// version is the release tag, injected at build time via
// -ldflags "-X main.version=<tag>" (see deploy/Dockerfile.miner + release.yml).
// It auto-tracks the git tag of each released image; the GRUB_VERSION env var
// is only a fallback for source/dev builds where no ldflag is set.
var version string

func main() {
	// `grubdrops keygen` prints a fresh, valid GRUB_MASTER_KEY and exits, so a
	// Docker-only user can generate one without Go or the age tool:
	//   docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen
	// The store requires an age X25519 identity (AGE-SECRET-KEY-1...); a random
	// base64 blob fails age.ParseX25519Identity and crashes at startup.
	if len(os.Args) > 1 && os.Args[1] == "keygen" {
		key, err := generateMasterKey()
		if err != nil {
			fmt.Fprintln(os.Stderr, "keygen:", err)
			os.Exit(1)
		}
		fmt.Println(key)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// generateMasterKey returns a fresh age X25519 identity string suitable for
// GRUB_MASTER_KEY (the exact format store.NewCryptor parses).
func generateMasterKey() (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// setProcessZone sets time.Local. A test that calls run swaps it out, because
// the write races the test runtime's own timers.
var setProcessZone = func(l *time.Location) { time.Local = l }

// run boots the miner and blocks until ctx ends or the HTTP server fails,
// then shuts down. Runners, background loops and open SSE streams run on a
// child of ctx that every exit path cancels.
func run(ctx context.Context) error {
	ctx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ring := mlog.NewRingFromEnv(1000)
	logger := mlog.NewWithRing(os.Stdout, cfg.LogLevel, ring)
	slog.SetDefault(logger)
	startTime := time.Now()
	logger.Info("miner starting", "log_level", cfg.LogLevel, "http_addr", cfg.HTTPAddr, "db_path", cfg.DBPath, "browser_url", cfg.BrowserURL, "secure_cookies", cfg.SecureCookies)

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	cryptor, err := store.NewCryptor(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("master key invalid: %w", err)
	}

	q := gen.New(db)
	settingsStore := store.NewSettings(q)
	sessions := store.NewSessionStore(db, q, cryptor)

	if err := i18n.Load(); err != nil {
		return fmt.Errorf("load i18n: %w", err)
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
	if oidcProvider != nil && oidcProvider.Enabled() {
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

	// Proxy support: read from settings and create shared transport
	proxyURLVal, proxyURLErr := settingsStore.ProxyURL(ctx)
	proxyURL := settingOr(logger, proxyURLVal, proxyURLErr, "", "proxy_url")
	proxyEnabledVal, proxyEnabledErr := settingsStore.ProxyEnabled(ctx)
	proxyEnabled := settingOr(logger, proxyEnabledVal, proxyEnabledErr, false, "proxy_enabled")
	var proxyTransport *http.Transport
	if proxyEnabled && proxyURL != "" {
		proxyTransport = netutil.NewTransport(proxyURL)
		// Log proxy URL with credentials masked
		safeURL := maskProxyURL(proxyURL)
		logger.Info("proxy enabled", "url", safeURL)
	}
	effectiveProxy := ""
	if proxyEnabled && proxyURL != "" {
		effectiveProxy = proxyURL
	}
	if effectiveProxy != "" {
		if _, err := netutil.ProxyDialer(effectiveProxy); err != nil {
			return fmt.Errorf("proxy enabled but misconfigured (%s): %w", maskProxyURL(effectiveProxy), err)
		}
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
	kickOpts = append(kickOpts, kick.WithProxy(effectiveProxy))
	kickBackend = kick.New(browserClient, dockerCtl, cfg.KickSidecarTemplate, cfg.KickSidecarPort, 10*time.Minute, kickOpts...)
	// Watch path is operator-selectable (Settings → Experimental). "browser"
	// (default) drives a real IVS <video> in the sidecar. "ws" is the
	// experimental pure-WebSocket path (no browser): leaving browser-watch OFF
	// routes StartWatch to the WebSocket watch (live-verified to accrue — see
	// internal/platform/kick/wswatch.go). The two are mutually exclusive (one
	// active watch per account). EnableBrowserWatch no-ops + warns if no sidecar
	// client is configured.
	kickWatchModeVal, kickWatchModeErr := settingsStore.KickWatchMode(ctx)
	kickWatchMode := settingOr(logger, kickWatchModeVal, kickWatchModeErr, store.KickWatchModeBrowser, "kick_watch_mode")
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

	// twitchBackend is the direct-HTTP backend used for the accrual canary
	// probe (ProbeBeacon). It is always created even in browser-watch mode
	// because BrowserBackend does not implement ProbeBeacon and the canary
	// only needs the beacon transport, not the full watch stack.
	var twitchBackend *twitch.Backend
	if proxyTransport != nil {
		bk, err := twitch.NewWithProxy(proxyTransport, effectiveProxy)
		if err != nil {
			logger.Warn("twitch: proxy-aware backend failed, falling back to non-proxied PubSub", "err", err)
			twitchBackend = twitch.NewWithTransport(proxyTransport)
		} else {
			twitchBackend = bk
		}
	} else {
		twitchBackend = twitch.New()
	}
	defer twitchBackend.Close()
	if twitchBrowserEnabled && browserClient != nil {
		// proxyTransport is nil when no proxy is configured; the browser
		// backend then dials the Spade beacon direct, same as before.
		registry.Register(twitch.NewBrowserBackendWithTransport(browserClient, proxyTransport))
		logger.Info("twitch backend: BROWSER (via sidecar)")
	} else {
		registry.Register(twitchBackend)
		logger.Info("twitch backend: direct HTTP (Android device-code, no integrity header)")
	}

	buildNotifier := func() notify.Notifier {
		// Build the per-kind filter from the saved toggles every time so a
		// settings save takes effect. State events are never sent to Discord
		// (too noisy).
		claim, progress, auth, errs, canaryOn := settingsStore.NotifyKinds(ctx)
		dailySummary, _ := settingsStore.NotifyDailySummary(ctx)
		filter := &notify.VerbosityFilter{Allow: map[string]bool{
			notify.EventClaim:    claim,
			notify.EventProgress: progress,
			notify.EventAuth:     auth,
			notify.EventError:    errs,
			notify.EventCanary:   canaryOn,
			notify.EventSummary:  dailySummary,
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
			var wh *notify.DiscordWebhook
			if proxyTransport != nil {
				wh = notify.NewDiscordWebhookWithTransport(globalURL, filter, proxyTransport)
			} else {
				wh = notify.NewDiscordWebhook(globalURL, filter)
			}
			wh.Username = botUsername
			wh.AvatarURL = avatarURL
			fallback = wh
		} else {
			fallback = &notify.NoopNotifier{Logger: logger}
		}
		return fallback
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

	// Effective display timezone: the in-app setting wins over the TZ env var,
	// falling back to UTC. Held in a live-swappable Zone so a Settings change
	// applies without a restart.
	tzSetting, _ := settingsStore.Timezone(ctx)
	displayLoc := timeutil.Resolve(tzSetting, os.Getenv("TZ"))
	displayZone := timeutil.NewZone(displayLoc)
	cfg.Location = displayLoc
	// Set process-wide timezone so any residual time.Local-based formatting
	// matches the display zone at boot.
	setProcessZone(displayLoc)

	// Per-account direct-Twitch backends. The direct twitch.Backend holds
	// per-account state (auth, userID/userLogin caches, its own PubSub
	// socket), so sharing ONE instance across accounts races their tokens
	// and caches together (audit P0). Give each Twitch account its own
	// instance, cached across rebuilds so we don't leak a PubSub goroutine
	// per rebuild. Kick + the browser BrowserBackend are already
	// multi-account-aware (per-account maps), so they keep using the
	// shared registry instance.
	browserActive := twitchBrowserEnabled && browserClient != nil
	twitchBackends := map[string]*twitch.Backend{}
	var twitchBackendsMu sync.Mutex
	backendFor := func(a gen.Account) (platform.Backend, error) {
		if a.Platform == "twitch" && !browserActive {
			twitchBackendsMu.Lock()
			defer twitchBackendsMu.Unlock()
			if bk, ok := twitchBackends[a.ID]; ok {
				return bk, nil
			}
			var bk *twitch.Backend
			if proxyTransport != nil {
				pbk, err := twitch.NewWithProxy(proxyTransport, effectiveProxy)
				if err != nil {
					logger.Warn("twitch: proxy-aware backend failed for account, falling back to non-proxied", "account", a.ID, "err", err)
					bk = twitch.New()
				} else {
					bk = pbk
				}
			} else {
				bk = twitch.New()
			}
			twitchBackends[a.ID] = bk
			return bk, nil
		}
		b, ok := registry.Get(a.Platform)
		if !ok {
			return nil, fmt.Errorf("no backend for platform %q", a.Platform)
		}
		return b, nil
	}
	closeTwitchBackend := func(id string) {
		twitchBackendsMu.Lock()
		defer twitchBackendsMu.Unlock()
		if bk, ok := twitchBackends[id]; ok {
			bk.Close()
			delete(twitchBackends, id)
			logger.Info("closed twitch backend", "account", id)
		}
	}

	pool := runner.NewPool(notifier)

	// Background loops join bg. On every exit path the deferred cleanup
	// cancels them, stops the runners and waits for both before the Twitch
	// backends and the DB close (their defers run after this one).
	var bg sync.WaitGroup
	defer func() {
		cancelRun()
		pool.Stop()
		bg.Wait()
		twitchBackendsMu.Lock()
		for id, bk := range twitchBackends {
			bk.Close()
			logger.Info("closed twitch backend", "account", id)
		}
		twitchBackends = map[string]*twitch.Backend{}
		twitchBackendsMu.Unlock()
	}()

	collector := collect.New(db, q)
	collector.Loc = displayZone.Location
	hub := api.NewHub()
	bg.Go(func() { hub.Run(ctx) })

	ro := newRoster(roster{
		ctx:        ctx,
		q:          q,
		sessions:   sessions,
		chosen:     store.NewChosenGames(db, q),
		skips:      store.NewSkips(q),
		collector:  collector,
		hub:        hub,
		pool:       pool,
		logger:     logger,
		backendFor: backendFor,
		notifier:   notifier,
	}, kickBackend)

	// Every ring line goes out as a `log` SSE event. The hook runs on the
	// logging goroutine, so it must not block: the account name comes from
	// the roster's in-memory cache, never the DB.
	bg.Go(ro.runGamesReloader)

	ring.SetHook(logHook(hub, ro.accountName))
	defer ring.SetHook(nil)

	builders, err := ro.builders(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	if err := pool.Reload(ctx, builders); err != nil {
		return fmt.Errorf("initial runner boot: %w", err)
	}

	// Now that the initial Reload has registered every enabled Kick account's
	// sidecar, sweep away any auto-created (grubdrops.managed=true) browser
	// container whose account is gone. Unlabeled hand-defined sidecars are
	// never listed by the sweep, so they survive untouched. The periodic
	// reaper repeats this every minute (covers account deletions at runtime).
	kickBackend.SweepSidecars(ctx)

	// Auth-health sweep: probe each account's auth (Twitch token / Kick
	// cookies) on a long cadence so the operator sees a "needs re-auth"
	// flag before an account silently stops mining. The notifier raises an
	// alert when an account's auth dies.
	authChecker := authcheck.New(q, sessions, registry).WithNotifier(notifier)
	authInterval := parseDuration(os.Getenv("GRUB_AUTHCHECK_INTERVAL"), time.Hour)
	bg.Go(func() { authChecker.Run(ctx, authInterval) })

	// Accrual canary: periodically probes the Twitch beacon and Kick WS
	// transports so the operator sees a canary result without waiting for an
	// actual campaign to be live. Borrows ONE shared session per platform
	// (first enabled account). Channel + interval come from settingsStore; the
	// GRUB_CANARY_INTERVAL env var overrides the DB value for ops use.
	canarySource := canary.SessionSource(func(runCtx context.Context, plat string) (platform.Session, bool, error) {
		accs, err := q.ListEnabledAccounts(runCtx)
		if err != nil {
			return platform.Session{}, false, err
		}
		for _, a := range accs {
			if a.Platform != plat {
				continue
			}
			s, ok, err := sessions.Get(runCtx, a.ID)
			if err != nil {
				return platform.Session{}, false, err
			}
			if !ok {
				continue
			}
			s.AccountID = a.ID
			return s, true, nil
		}
		return platform.Session{}, false, nil
	})
	canaryRunner := canary.NewRunnerWithSettingsReader(
		q,
		canarySource,
		canary.NewTwitchProbe(twitchBackend, 5*time.Second),
		canary.NewKickProbe(kickBackend, 45*time.Second),
		func(runCtx context.Context) canary.RunnerSettings {
			twChVal, twChErr := settingsStore.CanaryTwitchChannel(runCtx)
			twCh := settingOr(logger, twChVal, twChErr, "", "canary_twitch_channel")
			kkChVal, kkChErr := settingsStore.CanaryKickChannel(runCtx)
			kkCh := settingOr(logger, kkChVal, kkChErr, "", "canary_kick_channel")
			return canary.RunnerSettings{TwitchChannel: twCh, KickChannel: kkCh}
		},
		notifier,
	)
	canaryIntervalSecVal, canaryIntervalSecErr := settingsStore.CanaryIntervalSec(ctx)
	canaryIntervalSec := settingOr(logger, canaryIntervalSecVal, canaryIntervalSecErr, 3600, "canary_interval_sec")
	canaryInterval := parseDuration(os.Getenv("GRUB_CANARY_INTERVAL"), time.Duration(canaryIntervalSec)*time.Second)
	bg.Go(func() { canaryRunner.Run(ctx, canaryInterval) })

	// Update checker: background GitHub-release poll (boot + every interval),
	// cached so the request path never calls GitHub. Disabled by GRUB_UPDATE_CHECK=0.
	var updateChecker *update.Checker
	var updateStatus func(current string) (bool, string)
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("GRUB_UPDATE_CHECK"))); v != "0" && v != "false" {
		updateClient := &http.Client{Timeout: 10 * time.Second}
		if proxyTransport != nil {
			updateClient.Transport = proxyTransport
		}
		updateChecker = update.NewChecker(updateClient, "aalejandrofer/GrubDrops", settingsStore)
		updateInterval := parseDuration(os.Getenv("GRUB_UPDATE_INTERVAL"), 6*time.Hour)
		bg.Go(func() { updateChecker.Run(ctx, updateInterval) })
		updateStatus = updateChecker.Status
	}

	// Daily summary at each local midnight, when turned on in Settings.
	bg.Go(func() {
		summary.Run(ctx, summary.Config{
			Settings: settingsStore,
			Minutes:  collector.Minutes,
			Claims:   collector.Claims,
			Notifier: notifier,
			Zone:     displayZone.Location,
		})
	})

	deps := api.Deps{
		DB: db, Q: q, Session: sm,
		OIDC:          oidcProvider,
		SecureCookies: cfg.SecureCookies,
		Version:       cmp.Or(version, os.Getenv("GRUB_VERSION")),
		SPA:           web.Dist(),
		Hub:           hub,

		Pool:         pool,
		Minutes:      collector.Minutes,
		Claims:       collector.Claims,
		Scans:        collector.Scans,
		Zone:         displayZone.Location,
		UpdateStatus: updateStatus,

		Chosen:       ro.chosen,
		Board:        collector.Board,
		GamesChanged: ro.requestGamesReload,

		Sessions:  sessions,
		Registry:  registry,
		RootCtx:   ctx,
		Registrar: kickBackend,
		AccountChanged: func(id string) {
			ro.reloadAccount(id)
			hub.Publish(api.Event{Name: api.EventAccounts, Data: struct{}{}})
		},
		AccountRemoved: func(id string) {
			ro.accountRemoved(id)
			closeTwitchBackend(id)
		},

		Settings:         settingsStore,
		Notifier:         notifier,
		WebhookFallback:  cfg.DiscordWebhookURL,
		OnSettingsUpdate: onSettingsUpdate,
		SetZone: func(name string) {
			displayZone.Set(timeutil.Resolve(name, os.Getenv("TZ")))
		},

		Ring:         ring,
		StartTime:    startTime,
		Commit:       os.Getenv("GIT_COMMIT"),
		KickSidecars: kickSidecarLister(kickBackend),
		RunCanary:    canaryRunner.RunOnce,
		Update:       updateChecker,
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
		// Requests inherit the root ctx, so open SSE streams end on shutdown
		// instead of holding srv.Shutdown until its deadline.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	srvErr := make(chan error, 1)
	bg.Go(func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- err
		}
	})

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-srvErr:
		logger.Error("http server", "err", runErr)
	}
	logger.Info("shutting down")
	ring.SetHook(nil)
	cancelRun()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return runErr
}

// logHook publishes each ring line as a `log` SSE event. name must not block.
func logHook(hub *api.Hub, name func(id string) string) func(mlog.LogLine) {
	return func(l mlog.LogLine) {
		var n string
		if id := api.LogAccountID(l); id != "" {
			n = name(id)
		}
		hub.Publish(api.Event{Name: api.EventLog, Data: api.LogEventJSON(l, n)})
	}
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

// kickSidecarLister returns a closure the Status panel calls to list the
// RUNNING per-account Kick sidecar addresses, or nil when no Kick backend is
// wired. Only running sidecars are shown — on-demand containers spin up when a
// Kick watcher needs them, so an empty list when all watchers are idle is
// correct, not a fault.
func kickSidecarLister(b *kick.Backend) func() []string {
	if b == nil {
		return nil
	}
	return func() []string {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return b.RunningSidecarAddrs(ctx)
	}
}

// maskProxyURL masks credentials in proxy URLs for safe logging.
// e.g. "http://user:pass@host:port" -> "http://user:***@host:port"
func maskProxyURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}
	masked := *u
	masked.User = url.UserPassword(u.User.Username(), "***")
	return masked.String()
}

// settingOr logs a warning and returns the fallback when a settings read fails.
// Used to make ignored errors visible without cluttering every call site.
func settingOr[T any](logger *slog.Logger, v T, err error, fallback T, key string) T {
	if err != nil {
		logger.Warn("settings read failed, using default", "key", key, "err", err)
		return fallback
	}
	return v
}
