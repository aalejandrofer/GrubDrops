package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Force watch: with the plan queue empty, the runner watches the first live
// channel of the account's ordered force list. A drop always wins: every
// recheckInterval, and at once on Rescan, the force tick rescans the plan
// and a queued drop with a live stream ends the force watch.
//
// Points only: the account never scans or picks a drop campaign. pick goes
// straight to the force list (pickPoints) and the recheck only moves between
// force channels. The force tick still claims drops the inventory reports
// ready (claimReady, sweepCompleted): that is no campaign scan, and a drop a
// points channel happened to earn is not left unclaimed.

// forceChannels reads the account's force list, or nil when the feature is
// off.
func (r *Runner) forceChannels() []string {
	if r.cfg.ForceLookup == nil || r.cfg.ForceChannels == nil {
		return nil
	}
	return r.cfg.ForceChannels()
}

// liveForce returns the first live channel of logins, in order. A lookup
// error counts as not live. A channel whose watch failed to start within
// recheckInterval is passed over without a lookup.
func (r *Runner) liveForce(ctx context.Context, logins []string) (string, platform.Stream, bool) {
	now := time.Now()
	for _, login := range logins {
		if login == "" {
			continue
		}
		r.mu.Lock()
		failed := now.Before(r.forceFailed[login])
		r.mu.Unlock()
		if failed {
			continue
		}
		s, live, err := r.cfg.ForceLookup(ctx, login)
		if err != nil {
			slog.Debug("runner force channel lookup failed; treating as offline", "kind", "discovery",
				"account", r.cfg.AccountID, "channel", login, "err", err)
			continue
		}
		if live {
			return login, s, true
		}
	}
	return "", platform.Stream{}, false
}

// startForce watches the first live channel of logins whose watch starts.
// started is false with a nil error when none is live, and with the last
// start error when every live one failed to start.
func (r *Runner) startForce(ctx context.Context, logins []string) (bool, error) {
	login, s, ok := r.liveForce(ctx, logins)
	if !ok {
		return false, nil
	}
	if err := r.forceFrom(ctx, logins, login, s); err != nil {
		return false, err
	}
	return true, nil
}

// forceFrom starts watching login, live on s. A failed start marks login
// failed for recheckInterval and moves on to the next live channel ranked
// below it. It returns the last start error when none starts. Any watch must
// already be stopped.
func (r *Runner) forceFrom(ctx context.Context, logins []string, login string, s platform.Stream) error {
	for {
		err := r.beginForce(ctx, login, s)
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return err
		}
		slog.Warn("runner force channel failed to start; trying the next one", "kind", "error",
			"account", r.cfg.AccountID, "channel", login, "retry_in", recheckInterval, "err", err)
		r.mu.Lock()
		if r.forceFailed == nil {
			r.forceFailed = map[string]time.Time{}
		}
		r.forceFailed[login] = time.Now().Add(recheckInterval)
		r.mu.Unlock()
		var rest []string
		for i, l := range logins {
			if l == login {
				rest = logins[i+1:]
				break
			}
		}
		var ok bool
		if login, s, ok = r.liveForce(ctx, rest); !ok {
			return err
		}
	}
}

// beginForce starts watching the force channel login on stream s. Any watch
// must already be stopped.
func (r *Runner) beginForce(ctx context.Context, login string, s platform.Stream) error {
	if s.Channel == "" {
		s.Channel = login
	}
	h, err := r.cfg.Backend.StartWatch(ctx, r.cfg.Session, s)
	if err != nil {
		return fmt.Errorf("start force watch: %w", err)
	}
	slog.Info("runner force watching", "kind", "state", "account", r.cfg.AccountID, "channel", login)
	now := time.Now()
	r.mu.Lock()
	delete(r.forceFailed, login)
	r.current = nil
	r.stream = &s
	r.handle = &h
	r.channel = login
	r.viewers = s.ViewerCount
	r.minutes = 0
	r.startedAt = now
	r.tickN = 0
	r.lastBeatAt = time.Time{}
	r.forceCheckAt = now
	r.mu.Unlock()
	if cs, ok := r.cfg.Backend.(platform.ChannelSubscriber); ok && s.ChannelID != "" {
		cs.SubscribeChannel(r.cfg.AccountID, s.ChannelID)
	}
	r.startPoints(ctx, s.Channel)
	r.startRaids(ctx, s.ChannelID)
	r.setState(ctx, StateForceWatching)
	return nil
}

// pickPoints is pick for a points-only account: no campaign scan, no plan.
// It drops any plan left from drops mode and watches the first live force
// channel, failing over in order. None live sleeps at the 5-minute recheck;
// an empty force list idles as StateNoChannels.
func (r *Runner) pickPoints(ctx context.Context) error {
	r.mu.Lock()
	// Paces the idle wait and NextScanAt as a scan would.
	r.lastScanTry = time.Now()
	r.noStream = map[string]bool{}
	r.sleepNoStream = false
	r.current = nil
	r.clearWatchViewLocked()
	r.mu.Unlock()
	r.dropPlan()
	force := r.forceChannels()
	if len(force) == 0 {
		r.setState(ctx, StateNoChannels)
		return nil
	}
	started, err := r.startForce(ctx, force)
	if started || err != nil {
		return err
	}
	slog.Debug("runner points only: force channels offline; sleeping", "kind", "discovery",
		"account", r.cfg.AccountID)
	r.mu.Lock()
	r.sleepNoStream = true // recheck at the 5-minute pace
	r.mu.Unlock()
	r.setState(ctx, StateSleeping)
	return nil
}

// dropPlan clears the queue, blocked list and Mine now pin, so a points-only
// account shows no Up next from its drops-mode scans. A pending Mine now goes
// too: taken, it would restart the force watch.
func (r *Runner) dropPlan() {
	r.switchNow.Store(false)
	r.mu.Lock()
	had := len(r.queue) > 0 || len(r.blocked) > 0 || r.pinned != nil
	r.queue, r.blocked, r.pinned = nil, nil, nil
	r.mu.Unlock()
	if had {
		r.changed()
	}
}

// ForceListChanged tells the runner its force list was edited. A force watch
// rechecks the list at its next beat and keeps its channel when that is
// still the best live one. An idle runner re-picks now. A runner starting,
// watching or claiming a drop is left alone: it reads the list once idle.
func (r *Runner) ForceListChanged() {
	r.mu.Lock()
	st := r.state
	if st == StateForceWatching {
		r.forceCheckAt = time.Time{}
	}
	r.mu.Unlock()
	switch st {
	case StateForceWatching, StateStarting, StateWatching, StateClaiming:
		return
	}
	r.Rescan()
}

// forceTick beacons the force channel, sweeps and claims finished drops, and
// every recheckInterval rechecks the plan and the force list. A Rescan runs
// that recheck now, without a beacon: the watch ends only for a drop or a
// better force channel, as at the 5-minute recheck. Mine now ends it.
func (r *Runner) forceTick(ctx context.Context) error {
	if r.forceScan.Swap(false) {
		select {
		case <-r.rescan:
		default:
		}
		r.mu.Lock()
		login := r.channel
		r.mu.Unlock()
		slog.Debug("runner force watch rechecking on rescan", "kind", "state",
			"account", r.cfg.AccountID, "channel", login)
		return r.forceRecheck(ctx, login)
	}
	// An early wake (a stale Rescan token) must not beacon before TickInterval.
	if r.untilNextBeat() > 0 {
		return nil
	}
	r.mu.Lock()
	if r.handle == nil {
		r.mu.Unlock()
		return errors.New("runner force watch state incomplete")
	}
	handle := *r.handle
	login := r.channel
	// Stamped before the call so beacons start TickInterval apart.
	r.lastBeatAt = time.Now()
	due := time.Since(r.forceCheckAt) >= recheckInterval
	r.mu.Unlock()

	if err := r.beat(ctx, handle); err != nil {
		return err
	}
	r.sweepCompleted(ctx)
	r.claimReady(ctx, login)
	if !due {
		return nil
	}
	return r.forceRecheck(ctx, login)
}

// forceRecheck rescans the plan, then the force list. A queued drop with a
// live eligible stream ends the force watch. Otherwise the runner moves to
// the best live force channel, or sleeps when none is live. A points-only
// account skips the plan: no drop ever ends its force watch.
func (r *Runner) forceRecheck(ctx context.Context, login string) error {
	r.mu.Lock()
	r.forceCheckAt = time.Now()
	r.mu.Unlock()
	if r.pointsOnly() {
		r.dropPlan()
	} else if len(r.cfg.Games) > 0 {
		res, err := r.scan(ctx, nil, false)
		if err != nil {
			slog.Warn("runner rescan failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		} else {
			r.changed()
			if t, ok := r.watchableQueued(ctx, res.Queue); ok {
				slog.Info("runner drop available; ending force watch", "kind", "state",
					"account", r.cfg.AccountID, "channel", login,
					"campaign", t.Campaign.ID, "drop", t.Benefit.ID)
				r.stopWatch(ctx)
				r.setState(ctx, StatePicking)
				return nil
			}
		}
	}
	logins := r.forceChannels()
	if r.pointsOnly() && len(logins) == 0 {
		// The list was emptied: pick idles as no_channels now, not after the
		// 5-minute offline sleep.
		slog.Info("runner force list now empty; ending force watch", "kind", "state",
			"account", r.cfg.AccountID, "channel", login)
		r.stopWatch(ctx)
		r.setState(ctx, StatePicking)
		return nil
	}
	next, s, ok := r.liveForce(ctx, logins)
	switch {
	case !ok:
		slog.Debug("runner force channels offline; sleeping", "kind", "discovery",
			"account", r.cfg.AccountID, "channel", login)
		r.stopWatch(ctx)
		r.mu.Lock()
		r.sleepNoStream = true // recheck at the 5-minute pace
		r.mu.Unlock()
		r.setState(ctx, StateSleeping)
	case next != login:
		r.stopWatch(ctx)
		return r.forceFrom(ctx, logins, next, s)
	default:
		slog.Debug("runner force channel still live", "kind", "discovery",
			"account", r.cfg.AccountID, "channel", login, "viewers", s.ViewerCount)
		r.mu.Lock()
		r.viewers = s.ViewerCount
		r.mu.Unlock()
		r.changed()
	}
	return nil
}

// watchableQueued returns the first queued target whose campaign has a live
// eligible stream that start would take (stalled channels excluded). A
// campaign whose channel list fails counts as not live.
func (r *Runner) watchableQueued(ctx context.Context, queue []plan.Target) (plan.Target, bool) {
	seen := map[string]bool{}
	for _, t := range queue {
		if seen[t.Campaign.ID] {
			continue
		}
		seen[t.Campaign.ID] = true
		streams, err := r.cfg.Backend.ListEligibleChannels(ctx, r.cfg.Session, t.Campaign)
		if err != nil {
			slog.Debug("runner force recheck channel list failed", "kind", "discovery",
				"account", r.cfg.AccountID, "campaign", t.Campaign.ID, "err", err)
			continue
		}
		r.mu.Lock()
		streams = r.unstalledLocked(streams, time.Now())
		r.mu.Unlock()
		if len(streams) > 0 {
			return t, true
		}
	}
	return plan.Target{}, false
}
