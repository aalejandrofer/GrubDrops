package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/chano-fernandez/rust-drops-miner/internal/api"
	"github.com/chano-fernandez/rust-drops-miner/internal/config"
	mlog "github.com/chano-fernandez/rust-drops-miner/internal/log"
	"github.com/chano-fernandez/rust-drops-miner/internal/notify"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/fake"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/twitch"
	"github.com/chano-fernandez/rust-drops-miner/internal/scheduler"
	"github.com/chano-fernandez/rust-drops-miner/internal/store"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
	"github.com/chano-fernandez/rust-drops-miner/internal/watcher"
	"github.com/chano-fernandez/rust-drops-miner/internal/web"
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

	notifier := makeNotifier(cfg, logger)
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

	deps := api.Deps{
		DB: db, Q: q, Templates: tmplSet, Session: sm,
		Scheduler: sched, Reload: loadAndStart,
		Sessions: sessions, Registry: registry,
		RootCtx: ctx,
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

func makeNotifier(cfg config.Config, logger *slog.Logger) notify.Notifier {
	if cfg.DiscordWebhookURL != "" {
		return notify.NewDiscordWebhook(cfg.DiscordWebhookURL, &notify.VerbosityFilter{Allow: map[string]bool{
			notify.EventClaim: true, notify.EventError: true,
			notify.EventProgress: true, notify.EventAuth: true,
		}})
	}
	return &notify.NoopNotifier{Logger: logger}
}

type nopRunner struct{}

func (nopRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
