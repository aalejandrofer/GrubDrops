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
// forceScanInterval the force tick rescans the plan and a non-empty queue ends
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
	r.mu.Lock()
	r.forceChannelsSnapshot = append([]string{}, logins...)
	r.mu.Unlock()
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
	r.forceGeneration++
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
	r.startExtraForce(ctx, login)
	return nil
}
func (r *Runner) startExtraForce(ctx context.Context, primary string) {
	if r.cfg.Platform != "twitch" {
		return
	}
	r.mu.Lock()
	if r.state != StateForceWatching || r.handle == nil || r.channel != primary {
		r.mu.Unlock()
		return
	}
	channels := append([]string(nil), r.forceChannelsSnapshot...)
	uninitialized := r.forceChannelsSnapshot == nil
	generation := r.forceGeneration
	r.mu.Unlock()
	if uninitialized {
		channels = r.forceChannels()
		r.mu.Lock()
		if r.state != StateForceWatching || r.handle == nil || r.channel != primary || r.forceGeneration != generation {
			r.mu.Unlock()
			return
		}
		r.forceChannelsSnapshot = append([]string{}, channels...)
		r.mu.Unlock()
	}
	slots := 1
	if r.cfg.PointsStreams != nil {
		slots = max(1, r.cfg.PointsStreams())
	}
	r.mu.Lock()
	now := time.Now()
	retryDue := !r.extraRetryAt.IsZero() && !now.Before(r.extraRetryAt)
	probe := retryDue || (r.extraRetryAt.IsZero() && 1+len(r.extraWatches) < slots)
	r.mu.Unlock()
	r.reconcileExtraForceMode(ctx, primary, channels, probe)
}

func (r *Runner) reconcileExtraForce(ctx context.Context, primary string, channels []string) {
	r.reconcileExtraForceMode(ctx, primary, channels, true)
}

func (r *Runner) reconcileExtraForceMode(ctx context.Context, primary string, channels []string, probe bool) {
	if r.cfg.Platform != "twitch" {
		return
	}
	slots := 1
	if r.cfg.PointsStreams != nil {
		slots = max(1, r.cfg.PointsStreams())
	}
	capacity := min(slots-1, len(channels))
	wanted := make([]string, 0, capacity)
	positions := make(map[string]int, capacity)
	afterPrimary := false
	for _, login := range channels {
		if login == primary {
			afterPrimary = true
			continue
		}
		if afterPrimary && login != "" {
			if _, exists := positions[login]; !exists {
				positions[login] = len(wanted)
				wanted = append(wanted, login)
			}
		}
	}
	r.mu.Lock()
	if r.state != StateForceWatching || r.handle == nil || r.channel != primary {
		r.mu.Unlock()
		return
	}
	generation := r.forceGeneration
	existing := make(map[string]extraWatch, len(r.extraWatches))
	for i, login := range r.extraChannels {
		existing[login] = r.extraWatches[i]
	}
	keptNames := make([]string, 0, capacity)
	kept := make([]extraWatch, 0, capacity)
	for _, login := range wanted {
		if extra, ok := existing[login]; ok && len(kept) < slots-1 {
			keptNames = append(keptNames, login)
			kept = append(kept, extra)
			delete(existing, login)
		}
	}
	stale := make([]extraWatch, 0, len(existing))
	for _, extra := range existing {
		stale = append(stale, extra)
	}
	r.extraWatches, r.extraChannels = kept, keptNames
	if len(stale) > 0 {
		r.extraRetryAt = time.Time{}
	}
	retry := !r.extraRetryAt.IsZero() && time.Now().Before(r.extraRetryAt)
	r.mu.Unlock()
	r.stopExtraWatches(ctx, stale)
	if len(stale) > 0 {
		r.changed()
	}
	if !probe || retry {
		return
	}

	seen := make(map[string]bool, len(keptNames)+1)
	seen[primary] = true
	for _, login := range keptNames {
		seen[login] = true
	}
	failedCandidate := false
	for _, login := range wanted {
		if seen[login] {
			continue
		}
		s, live, err := r.cfg.ForceLookup(ctx, login)
		if err != nil || !live {
			failedCandidate = true
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if s.Channel == "" {
			s.Channel = login
		}

		// Make room only by evicting a lower-ranked extra; if its replacement
		// fails, the later candidate pass can restore that stream.
		r.mu.Lock()
		if r.state != StateForceWatching || r.handle == nil || r.channel != primary || r.forceGeneration != generation {
			r.mu.Unlock()
			return
		}
		evicted := extraWatch{}
		evictedLogin := ""
		worstRank := positions[login]
		for i, current := range r.extraChannels {
			if rank := positions[current]; rank > worstRank {
				worstRank = rank
				evicted, evictedLogin = r.extraWatches[i], current
			}
		}
		if 1+len(r.extraWatches) >= slots {
			if evictedLogin == "" {
				r.mu.Unlock()
				continue
			}
			for i, current := range r.extraChannels {
				if current == evictedLogin {
					r.extraWatches = append(r.extraWatches[:i], r.extraWatches[i+1:]...)
					r.extraChannels = append(r.extraChannels[:i], r.extraChannels[i+1:]...)
					break
				}
			}
		}
		r.mu.Unlock()
		if evictedLogin != "" {
			r.stopExtraWatches(ctx, []extraWatch{evicted})
			delete(seen, evictedLogin)
		}

		h, err := r.cfg.Backend.StartWatch(ctx, r.cfg.Session, s)
		if err != nil {
			failedCandidate = true
			slog.Warn("runner extra force watch failed", "kind", "error", "account", r.cfg.AccountID, "channel", login, "err", err)
			continue
		}
		currentSlots := 1
		if r.cfg.PointsStreams != nil {
			currentSlots = max(1, r.cfg.PointsStreams())
		}
		r.mu.Lock()
		already := false
		for _, current := range r.extraChannels {
			if current == login {
				already = true
				break
			}
		}
		if r.state != StateForceWatching || r.handle == nil || r.channel != primary || r.forceGeneration != generation ||
			1+len(r.extraWatches) >= currentSlots || already ||
			!forceAssignmentAllowsExtra(r.forceChannelsSnapshot, primary, login, currentSlots) {
			r.mu.Unlock()
			r.stopExtraWatches(ctx, []extraWatch{{handle: h, stream: s}})
			continue
		}
		if cs, ok := r.cfg.Backend.(platform.ChannelSubscriber); ok && s.ChannelID != "" {
			cs.SubscribeChannel(r.cfg.AccountID, s.ChannelID)
		}
		r.extraWatches = append(r.extraWatches, extraWatch{handle: h, stream: s})
		r.extraChannels = append(r.extraChannels, login)
		activeCount := 1 + len(r.extraWatches)
		r.mu.Unlock()
		seen[login] = true
		if activeCount >= currentSlots {
			break
		}
	}
	r.mu.Lock()
	if failedCandidate || 1+len(r.extraWatches) < slots {
		r.extraRetryAt = time.Now().Add(recheckInterval)
	} else {
		r.extraRetryAt = time.Time{}
	}
	r.mu.Unlock()
	r.changed()
}

func (r *Runner) stopExtraWatches(ctx context.Context, extras []extraWatch) {
	if len(extras) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, extra := range extras {
		if err := r.cfg.Backend.StopWatch(ctx, extra.handle); err != nil {
			slog.Warn("runner extra force stop failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		}
		if cs, ok := r.cfg.Backend.(platform.ChannelSubscriber); ok && extra.stream.ChannelID != "" {
			cs.UnsubscribeChannel(r.cfg.AccountID, extra.stream.ChannelID)
		}
	}
}
func forceAssignmentAllowsExtra(channels []string, primary, login string, slots int) bool {
	if slots <= 1 {
		return false
	}
	afterPrimary := false
	for _, channel := range channels {
		if channel == primary {
			afterPrimary = true
			continue
		}
		if afterPrimary && channel == login {
			return true
		}
	}
	return false
}

func (r *Runner) stopExtraForce(login string) {
	r.mu.Lock()
	for i, name := range r.extraChannels {
		if name != login {
			continue
		}
		extra := r.extraWatches[i]
		r.extraWatches = append(r.extraWatches[:i], r.extraWatches[i+1:]...)
		r.extraChannels = append(r.extraChannels[:i], r.extraChannels[i+1:]...)
		r.extraRetryAt = time.Now().Add(recheckInterval)
		r.mu.Unlock()
		r.stopExtraWatches(context.Background(), []extraWatch{extra})
		r.changed()
		return
	}
	r.mu.Unlock()
}
func (r *Runner) stopForceWatch(ctx context.Context) {
	r.mu.Lock()
	r.forceGeneration++
	r.forceChannelsSnapshot = nil
	r.mu.Unlock()
	r.stopWatch(ctx)
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
		r.forceChannelsSnapshot = nil
		r.extraRetryAt = time.Time{}
	}
	r.mu.Unlock()
	switch st {
	case StateForceWatching, StateStarting, StateWatching, StateClaiming:
		return
	}
	r.Rescan()
}

// forceTick beacons the force channel, sweeps and claims finished drops, and
// every forceCheckInterval rechecks the plan and the force list. Points-only
// runners recheck channel priority less often and never scan for drops.
func (r *Runner) forceTick(ctx context.Context) error {
	if r.forceScan.Swap(false) {
		select {
		case <-r.rescan:
		default:
		}
		slog.Debug("runner force watch ended by rescan", "kind", "state", "account", r.cfg.AccountID)
		r.stopForceWatch(ctx)
		r.setState(ctx, StatePicking)
		return nil
	}
	// An early wake (a stale Rescan token) must not beacon before TickInterval.
	if r.untilNextBeat() > 0 {
		return nil
	}
	if r.cfg.SleepUntil != nil {
		if until, asleep := r.cfg.SleepUntil(time.Now()); asleep {
			r.stopForceWatch(ctx)
			r.mu.Lock()
			r.sleepUntil = until
			r.sleepNoStream = false
			r.mu.Unlock()
			r.setState(ctx, StateSleeping)
			return nil
		}
	}
	r.mu.Lock()
	if r.handle == nil {
		r.mu.Unlock()
		return errors.New("runner force watch state incomplete")
	}
	handle := *r.handle
	login := r.channel
	extras := append([]extraWatch(nil), r.extraWatches...)
	// Stamped before the call so beacons start TickInterval apart.
	r.lastBeatAt = time.Now()
	due := time.Since(r.forceCheckAt) >= r.forceCheckInterval()
	r.mu.Unlock()

	if err := r.beat(ctx, handle); err != nil {
		return err
	}
	for _, extra := range extras {
		if err := r.cfg.Backend.Heartbeat(ctx, extra.handle); err != nil {
			slog.Warn("runner extra force heartbeat failed", "kind", "error", "account", r.cfg.AccountID,
				"channel", extra.handle.Channel, "retry_in", recheckInterval, "err", err)
			r.stopExtraForce(extra.handle.Channel)
		}
	}
	if !r.cfg.PointsOnly {
		r.sweepCompleted(ctx)
		r.claimReady(ctx, login)
	}
	if !due {
		r.startExtraForce(ctx, login)
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
	if len(r.cfg.Games) > 0 && !r.cfg.PointsOnly {
		res, err := r.scan(ctx, nil, false)
		if err != nil {
			slog.Warn("runner rescan failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		} else {
			r.changed()
			if t, ok := r.watchableQueued(ctx, res.Queue); ok {
				slog.Info("runner drop available; ending force watch", "kind", "state",
					"account", r.cfg.AccountID, "channel", login,
					"campaign", t.Campaign.ID, "drop", t.Benefit.ID)
				r.stopForceWatch(ctx)
				r.setState(ctx, StatePicking)
				return nil
			}
		}
	}
	logins := r.forceChannels()
	r.mu.Lock()
	r.forceChannelsSnapshot = append([]string{}, logins...)
	r.mu.Unlock()
	next, s, ok := r.liveForce(ctx, logins)
	switch {
	case !ok:
		slog.Debug("runner force channels offline; sleeping", "kind", "discovery",
			"account", r.cfg.AccountID, "channel", login)
		r.stopForceWatch(ctx)
		r.mu.Lock()
		r.sleepNoStream = true // recheck at the 5-minute pace
		r.mu.Unlock()
		r.setState(ctx, StateSleeping)
	case next != login:
		r.stopForceWatch(ctx)
		return r.forceFrom(ctx, logins, next, s)
	default:
		r.reconcileExtraForce(ctx, login, logins)
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
