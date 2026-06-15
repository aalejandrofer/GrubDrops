// Package canary provides a scheduled accrual-canary Runner that periodically
// probes the Twitch and Kick watch-time transports and persists results via
// SaveResult / LoadResult. The Runner borrows ONE shared session per platform
// (the first enabled account's) — the same pattern used by internal/discovery.
package canary

import (
	"context"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// probe is the minimal interface satisfied by both TwitchProbe and KickProbe.
// Keeping it here (rather than in each probe file) lets tests inject fakes
// without importing the concrete probe types.
type probe interface {
	Run(ctx context.Context, sess platform.Session, channel string) Result
}

// SessionSource is the function signature used to borrow a session for a
// platform. Return ok=false to indicate no enabled session is available;
// the Runner will skip that platform for the current tick without error.
//
// Implementations typically call q.ListEnabledAccounts and sessions.Get,
// filtering by platform name — exactly the pattern in internal/discovery.
type SessionSource func(ctx context.Context, platform string) (platform.Session, bool, error)

// RunnerSettings carries the per-run configuration read from settingsStore.
// The Runner reads these once per RunOnce call so changes take effect on the
// next tick without a restart.
type RunnerSettings struct {
	// TwitchChannel is the Twitch channel slug to probe. Empty = skip Twitch.
	TwitchChannel string
	// KickChannel is the Kick channel slug to probe. Empty = skip Kick.
	KickChannel string
}

// Runner is the scheduled canary. It holds:
//   - a Queries handle for persisting results,
//   - a SessionSource for borrowing one session per platform,
//   - the configured TwitchProbe / KickProbe (or test doubles),
//   - a RunnerSettings snapshot (re-read each tick via the settingsReader).
type Runner struct {
	q              *gen.Queries
	source         SessionSource
	twitchProbe    probe
	kickProbe      probe
	settingsReader func(ctx context.Context) RunnerSettings
	log            *slog.Logger
}

// NewRunner creates a Runner that uses the provided probes and a static
// RunnerSettings snapshot. This constructor is used in tests and in production
// wiring where the caller owns settings retrieval.
//
// Use NewRunnerWithSettingsReader when the interval / channels should be
// re-read from a store on every tick.
func NewRunner(q *gen.Queries, source SessionSource, twitch, kick probe, settings RunnerSettings) *Runner {
	return &Runner{
		q:      q,
		source: source,
		twitchProbe: twitch,
		kickProbe:   kick,
		settingsReader: func(_ context.Context) RunnerSettings { return settings },
		log:    slog.Default().With("component", "canary"),
	}
}

// NewRunnerWithSettingsReader creates a Runner that calls settingsReader on
// every RunOnce to pick up the latest channel configuration. This is what the
// production cmd/miner wiring uses.
func NewRunnerWithSettingsReader(
	q *gen.Queries,
	source SessionSource,
	twitch, kick probe,
	reader func(ctx context.Context) RunnerSettings,
) *Runner {
	return &Runner{
		q:              q,
		source:         source,
		twitchProbe:    twitch,
		kickProbe:      kick,
		settingsReader: reader,
		log:            slog.Default().With("component", "canary"),
	}
}

// RunOnce reads current settings, then for each platform:
//   - skips if the configured channel is empty, OR
//   - skips if no enabled session is available for that platform,
//   - otherwise runs the probe and persists the result via SaveResult.
func (r *Runner) RunOnce(ctx context.Context) error {
	settings := r.settingsReader(ctx)

	r.runProbe(ctx, "twitch", settings.TwitchChannel, r.twitchProbe)
	r.runProbe(ctx, "kick", settings.KickChannel, r.kickProbe)

	return nil
}

func (r *Runner) runProbe(ctx context.Context, plat, channel string, p probe) {
	if channel == "" {
		r.log.Debug("canary: skipping platform (no channel configured)", "platform", plat)
		return
	}

	sess, ok, err := r.source(ctx, plat)
	if err != nil {
		r.log.Warn("canary: session source error", "platform", plat, "err", err)
		return
	}
	if !ok {
		r.log.Debug("canary: skipping platform (no enabled session)", "platform", plat)
		return
	}

	result := p.Run(ctx, sess, channel)
	if err := SaveResult(ctx, r.q, plat, result); err != nil {
		r.log.Warn("canary: persist result failed", "platform", plat, "err", err)
		return
	}
	r.log.Info("canary: probe complete", "platform", plat, "ok", result.OK, "detail", result.Detail)
}

// Run probes once immediately (to surface issues without waiting for the first
// tick), then every interval until ctx is cancelled. If interval <= 0, Run
// returns immediately without running any probe — callers should treat this as
// "canary disabled".
func (r *Runner) Run(ctx context.Context, interval time.Duration) {
	if r == nil {
		return
	}
	if interval <= 0 {
		return
	}

	_ = r.RunOnce(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = r.RunOnce(ctx)
		}
	}
}
