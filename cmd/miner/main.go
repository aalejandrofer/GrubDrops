package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/rust-drops-miner/internal/api"
	"github.com/aalejandrofer/rust-drops-miner/internal/auth/browser"
	"github.com/aalejandrofer/rust-drops-miner/internal/config"
	mlog "github.com/aalejandrofer/rust-drops-miner/internal/log"
	"github.com/aalejandrofer/rust-drops-miner/internal/notify"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/fake"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/kick"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform/twitch"
	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
	"github.com/aalejandrofer/rust-drops-miner/internal/watcher"
	"github.com/aalejandrofer/rust-drops-miner/internal/web"
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

	ring := mlog.NewRing(1000)
	logger := mlog.NewWithRing(os.Stdout, "info", ring)

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
	registry.Register(fake.New(fake.WithFastTime()))
	registry.Register(twitch.New())

	var browserClient *browser.Client
	var kickBackend *kick.Backend
	if cfg.BrowserURL != "" {
		bc, err := browser.Dial(cfg.BrowserURL)
		if err != nil {
			return fmt.Errorf("dial browser sidecar: %w", err)
		}
		defer bc.Close()
		browserClient = bc
		kickBackend = kick.New(bc)
		registry.Register(kickBackend)
		logger.Info("kick backend enabled via sidecar", "url", cfg.BrowserURL)
	} else {
		logger.Info("MINER_BROWSER_URL empty, Kick backend disabled")
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

	build := func(a gen.Account) (scheduler.Entry, error) {
		b, ok := registry.Get(a.Platform)
		if !ok {
			return scheduler.Entry{}, fmt.Errorf("no backend for platform %q", a.Platform)
		}

		var sess platform.Session
		switch a.Platform {
		case "fake":
			s, err := b.PollDeviceLogin(ctx, platform.DeviceChallenge{})
			if err != nil {
				return scheduler.Entry{}, fmt.Errorf("device login: %w", err)
			}
			sess = s
		default:
			// Real backends load persisted session blob. If missing/expired,
			// install a nopRunner so the account is visible but paused;
			// the GUI device-code flow (Task 11) refreshes it.
			s, ok, err := sessions.Get(ctx, a.ID)
			if err != nil {
				return scheduler.Entry{}, fmt.Errorf("load session: %w", err)
			}
			if !ok || s.ExpiresAt.Before(time.Now()) {
				logger.Warn("account has no valid session, will idle until re-auth",
					"account", a.ID, "platform", a.Platform)
				return scheduler.NewEntry(a.ID, nopRunner{}), nil
			}
			sess = s
		}

		w := watcher.New(watcher.Config{
			AccountID: a.ID, Backend: b, Session: sess,
			Notifier: notifier, TickInterval: 500 * time.Millisecond,
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

	// Avoid typed-nil-interface trap: only assign if the concrete pointer is non-nil.
	var bc api.KickBrowserClient
	var reg api.KickChannelRegistrar
	if browserClient != nil {
		bc = browserClient  // *browser.Client satisfies KickBrowserClient
		reg = kickBackend   // *kick.Backend satisfies KickChannelRegistrar
	}

	deps := api.Deps{
		DB: db, Q: q, Templates: tmplSet, Session: sm,
		Scheduler: sched, Reload: loadAndStart,
		Sessions: sessions, Registry: registry,
		RootCtx:          ctx,
		BrowserClient:    bc,
		Registrar:        reg,
		SettingsStore:    settingsStore,
		OnSettingsUpdate: onSettingsUpdate,
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
