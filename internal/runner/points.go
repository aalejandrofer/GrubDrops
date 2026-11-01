package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Channel points: while any watch runs (a drop or a force channel), an
// account whose points mode is on claims the channel-points bonus on the
// watched channel. The claim is a side task on a watch-scoped context, apart
// from the beacon: it never delays a heartbeat, and stopWatch cancels it.

// pointsEvery and pointsTimeout are vars so tests can shrink them. Each watch
// reads them once, at its start.
var (
	// pointsEvery is the claim cadence after the first try at watch start.
	pointsEvery = 5 * time.Minute
	// pointsTimeout bounds one claim attempt.
	pointsTimeout = 10 * time.Second
)

// pointsTask is the running claim side task of one watch.
type pointsTask struct {
	cancel context.CancelFunc
	done   chan struct{} // closed when the goroutine has returned
}

// stop cancels the task and waits for its goroutine to return. nil-safe.
func (t *pointsTask) stop() {
	if t == nil {
		return
	}
	t.cancel()
	<-t.done
}

// pointsOn reads the account's points mode now.
func (r *Runner) pointsOn() bool {
	return r.cfg.PointsMode != nil && r.cfg.PointsMode().On()
}

// pointsOnly reports whether the account is in points-only mode now: no
// drop campaigns, only its force channels.
func (r *Runner) pointsOnly() bool {
	return r.cfg.PointsMode != nil && r.cfg.PointsMode() == platform.PointsOnly
}

// pointsClaimer is the backend's claimer when this account's points mode is
// on, else ok is false.
func (r *Runner) pointsClaimer() (platform.PointsClaimer, bool) {
	if !r.pointsOn() {
		return nil, false
	}
	pc, ok := r.cfg.Backend.(platform.PointsClaimer)
	return pc, ok
}

// startPoints starts the claim side task for a watch on channel, a child of
// ctx. It replaces any task still running. A no-op when points are off, the
// backend cannot claim, or no watch is live (a PubSub stream-down push may
// have stopped it).
func (r *Runner) startPoints(ctx context.Context, channel string) {
	pc, ok := r.pointsClaimer()
	if !ok || channel == "" {
		return
	}
	pctx, cancel := context.WithCancel(ctx)
	t := &pointsTask{cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	if r.handle == nil {
		r.mu.Unlock()
		cancel()
		return
	}
	prev := r.points
	r.points = t
	r.mu.Unlock()
	prev.stop()
	go r.pointsLoop(pctx, t.done, pc, channel, pointsEvery, pointsTimeout)
}

// syncPoints starts or stops the live watch's claim side task to match the
// points mode now, keeping the watch. A backend that cannot claim (Kick)
// never starts one. Run goroutine only.
func (r *Runner) syncPoints(ctx context.Context) {
	_, on := r.pointsClaimer()
	r.mu.Lock()
	running := r.points != nil
	channel := ""
	if r.handle != nil && r.stream != nil {
		channel = r.stream.Channel
	}
	r.mu.Unlock()
	switch {
	case on && !running && channel != "":
		slog.Info("runner channel points on; claiming on the current watch", "kind", "state",
			"account", r.cfg.AccountID, "channel", channel)
		r.startPoints(ctx, channel)
	case !on && running:
		slog.Info("runner channel points off; claims stopped", "kind", "state",
			"account", r.cfg.AccountID, "channel", channel)
		r.stopPoints()
	}
}

// stopPoints cancels the running claim side task and waits for it to end.
func (r *Runner) stopPoints() {
	r.mu.Lock()
	t := r.points
	r.points = nil
	r.mu.Unlock()
	t.stop()
}

// pointsLoop tries a claim at once, then every interval, until ctx ends.
func (r *Runner) pointsLoop(ctx context.Context, done chan struct{}, pc platform.PointsClaimer, channel string, every, timeout time.Duration) {
	defer close(done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		r.claimPoints(ctx, pc, channel, timeout)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// claimPoints makes one claim attempt under its own timeout. A claim logs
// once at INFO; no bonus available logs nothing; a failure logs once per
// distinct error.
func (r *Runner) claimPoints(ctx context.Context, pc platform.PointsClaimer, channel string, timeout time.Duration) {
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := pc.ClaimChannelPoints(actx, r.cfg.Session, channel)
	if ctx.Err() != nil {
		return // the watch stopped mid-call
	}
	if err != nil {
		if r.logPoints.Changed(err.Error()) {
			slog.Warn("runner channel points claim failed", "kind", "error",
				"account", r.cfg.AccountID, "channel", channel, "err", err)
		}
		return
	}
	r.logPoints.Reset()
	if res.Balance >= 0 && r.cfg.OnPointsBalance != nil {
		r.cfg.OnPointsBalance(ctx, channel, res.Balance)
	}
	if !res.Claimed {
		return
	}
	attrs := []any{"kind", "info", "account", r.cfg.AccountID, "channel", channel}
	if res.Gained > 0 {
		attrs = append(attrs, "points", res.Gained)
	}
	if res.Balance >= 0 {
		attrs = append(attrs, "balance", res.Balance)
	}
	slog.Info("runner claimed channel points bonus", attrs...)
}
