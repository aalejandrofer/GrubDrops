package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/api"
	"github.com/chano-fernandez/rust-drops-miner/internal/config"
	mlog "github.com/chano-fernandez/rust-drops-miner/internal/log"
	"github.com/chano-fernandez/rust-drops-miner/internal/notify"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform/fake"
	"github.com/chano-fernandez/rust-drops-miner/internal/scheduler"
	"github.com/chano-fernandez/rust-drops-miner/internal/store"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
	"github.com/chano-fernandez/rust-drops-miner/internal/watcher"
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
	// Held for Plan 2/3 wiring (session blob encrypt/decrypt). Bind the
	// reference here so the daemon refuses to start without a valid key.
	_ = cryptor

	q := gen.New(db)
	accounts, err := q.ListEnabledAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}

	registry := platform.NewRegistry()
	// Fast time keeps the demo loop short (RequiredMinutes=2 vs default 5)
	// so a claim happens within a few seconds during smoke runs.
	registry.Register(fake.New(fake.WithFastTime()))

	notifier := &notify.NoopNotifier{Logger: logger}
	sched := scheduler.New(scheduler.Options{Notifier: notifier})

	for _, a := range accounts {
		backend, ok := registry.Get(a.Platform)
		if !ok {
			logger.Warn("no backend registered for account", "platform", a.Platform, "account", a.ID)
			continue
		}
		sess, err := backend.PollDeviceLogin(ctx, platform.DeviceChallenge{})
		if err != nil {
			logger.Error("device login failed", "account", a.ID, "err", err)
			continue
		}
		w := watcher.New(watcher.Config{
			AccountID:    a.ID,
			Backend:      backend,
			Session:      sess,
			Notifier:     notifier,
			TickInterval: 500 * time.Millisecond,
		})
		sched.Add(a.ID, w)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(api.Deps{}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", "err", err)
		}
	}()

	if err := sched.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	sched.Wait()
	return nil
}
