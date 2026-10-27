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
// recheckInterval the force tick rescans the plan and a non-empty queue ends
// the force watch.

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
	r.setState(ctx, StateForceWatching)
	return nil
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
// every recheckInterval rechecks the plan and the force list. A Rescan or
// Mine now ends the force watch and re-picks.
func (r *Runner) forceTick(ctx context.Context) error {
	if r.forceScan.Swap(false) {
		select {
		case <-r.rescan:
		default:
		}
		slog.Debug("runner force watch ended by rescan", "kind", "state", "account", r.cfg.AccountID)
		r.stopWatch(ctx)
		r.setState(ctx, StatePicking)
		return nil
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
// the best live force channel, or sleeps when none is live.
func (r *Runner) forceRecheck(ctx context.Context, login string) error {
	r.mu.Lock()
	r.forceCheckAt = time.Now()
	r.mu.Unlock()
	if len(r.cfg.Games) > 0 {
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
