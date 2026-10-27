package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Mining safeguards carried over from 1.x.
const (
	// vanishThreshold: consecutive polls without the benefit, after it had
	// progress, before it counts as claimed elsewhere or expired.
	vanishThreshold = 3
	// neverEnrolledThreshold: polls to wait for a benefit that never appeared
	// (claimed already, or a ghost). 1.x synthSkipThreshold.
	neverEnrolledThreshold = 6
	// freezeThreshold: consecutive flat polls before rotating channel.
	freezeThreshold = 5
	// stalledChannelCooldown keeps a frozen channel out of the pick.
	stalledChannelCooldown = 30 * time.Minute
	// preemptCooldown keeps a campaign a preempt could not watch from
	// triggering another switch at every rescan.
	preemptCooldown = 3 * time.Hour
	// liveCheckEvery is the stream-liveness re-probe cadence.
	liveCheckEvery = 5 * time.Minute
	// maxProbe caps AvailableDropIDs calls per pick.
	maxProbe = 5
)

// start picks a live channel for the current target and begins watching.
func (r *Runner) start(ctx context.Context) error {
	r.mu.Lock()
	if r.current == nil {
		r.mu.Unlock()
		return errors.New("no current target")
	}
	target := *r.current
	stale := r.handle != nil
	r.mu.Unlock()
	if stale {
		r.stopWatch(ctx)
	}
	camp := target.Campaign

	streams, err := r.cfg.Backend.ListEligibleChannels(ctx, r.cfg.Session, camp)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}

	r.mu.Lock()
	now := time.Now()
	streams = r.unstalledLocked(streams, now)
	if len(streams) == 0 {
		r.noStream[camp.ID] = true
		if r.preemptTo == camp.ID {
			r.preemptTo = ""
			if r.preemptBlocked == nil {
				r.preemptBlocked = map[string]time.Time{}
			}
			r.preemptBlocked[camp.ID] = now.Add(preemptCooldown)
		}
		// A Mine now target with no stream is unmineable: back to the plan.
		if r.pinned != nil && r.pinned.Campaign.ID == camp.ID {
			r.pinned = nil
		}
		var next *plan.Target
		for i := range r.queue {
			if !r.noStream[r.queue[i].Campaign.ID] {
				t := r.queue[i]
				next = &t
				break
			}
		}
		if next != nil {
			r.current = next
			r.minutes = next.Minutes
			r.channel = ""
			r.viewers = 0
			r.mu.Unlock()
			slog.Info("runner no eligible streams live, trying next queued campaign",
				"kind", "discovery", "account", r.cfg.AccountID, "campaign", camp.Name,
				"next_campaign", next.Campaign.ID, "drop", next.Benefit.ID)
			r.changed()
			// Same state, so Run would wait a tick: start the next target now.
			return r.start(ctx)
		}
		r.current = nil
		r.clearWatchViewLocked()
		r.noStream = map[string]bool{}
		r.sleepNoStream = true
		r.mu.Unlock()
		// Nothing queued can be watched: a live force channel fills the gap.
		if login, s, ok := r.liveForce(ctx, r.forceChannels()); ok {
			slog.Info("runner no eligible streams live in any queued campaign; force watching",
				"kind", "discovery", "account", r.cfg.AccountID, "campaign", camp.Name)
			return r.beginForce(ctx, login, s)
		}
		slog.Info("runner no eligible streams live in any queued campaign; sleeping",
			"kind", "discovery", "account", r.cfg.AccountID, "campaign", camp.Name)
		r.setState(ctx, StateSleeping)
		return nil
	}
	r.mu.Unlock()

	sort.SliceStable(streams, func(i, j int) bool {
		return streams[i].ViewerCount > streams[j].ViewerCount
	})

	s := streams[0]
	if checker, ok := r.cfg.Backend.(platform.AvailableDropsChecker); ok {
		probed := 0
		for _, cand := range streams {
			if cand.ChannelID == "" {
				continue
			}
			if probed >= maxProbe {
				break
			}
			probed++
			drops, err := checker.AvailableDropIDs(ctx, r.cfg.Session, cand.ChannelID)
			if err != nil {
				slog.Debug("runner AvailableDropIDs failed; falling back",
					"kind", "error", "account", r.cfg.AccountID, "channel", cand.Channel, "err", err)
				break
			}
			if len(drops) == 0 {
				s = cand
				break
			}
			if _, hit := drops[target.Benefit.ID]; hit {
				s = cand
				break
			}
		}
	}

	if r.logStart.Changed(s.Channel + "/" + camp.Name) {
		slog.Info("runner starting watch", "kind", "state", "account", r.cfg.AccountID,
			"channel", s.Channel, "campaign", camp.Name, "eligible_count", len(streams))
	}
	h, err := r.cfg.Backend.StartWatch(ctx, r.cfg.Session, s)
	if err != nil {
		return fmt.Errorf("start watch: %w", err)
	}

	r.mu.Lock()
	if r.preemptTo == camp.ID {
		r.preemptTo = ""
	}
	r.stream = &s
	r.handle = &h
	r.channel = s.Channel
	r.viewers = s.ViewerCount
	r.startedAt = time.Now()
	r.tickN = 0
	r.lastBeatAt = time.Time{}
	r.noProgress = 0
	r.noAdvance = 0
	r.lastMin = 0
	r.lastPollMin = 0
	if target.Benefit.ID != r.milestoneBenefit {
		r.lastMilestone = -1
		r.milestoneBenefit = target.Benefit.ID
	}
	r.mu.Unlock()

	if cs, ok := r.cfg.Backend.(platform.ChannelSubscriber); ok && s.ChannelID != "" {
		cs.SubscribeChannel(r.cfg.AccountID, s.ChannelID)
	}
	r.setState(ctx, StateWatching)
	return nil
}

// tick sends the beacon, checks the channel is live, and polls inventory.
func (r *Runner) tick(ctx context.Context) error {
	r.mu.Lock()
	if r.handle == nil || r.current == nil {
		r.mu.Unlock()
		return errors.New("runner watch state incomplete")
	}
	handle := *r.handle
	target := *r.current
	r.tickN++
	tickN := r.tickN
	// Stamped before the call so beacons start TickInterval apart, as the
	// 1.x ticker did.
	r.lastBeatAt = time.Now()
	r.mu.Unlock()
	benefit := target.Benefit

	if err := r.beat(ctx, handle); err != nil {
		return err
	}

	every := max(1, int(liveCheckEvery/r.cfg.TickInterval))
	if tickN%every == 0 {
		streams, err := r.cfg.Backend.ListEligibleChannels(ctx, r.cfg.Session, target.Campaign)
		if err == nil {
			live := false
			for _, s := range streams {
				if s.Channel == handle.Channel {
					live = true
					break
				}
			}
			if !live {
				slog.Info("runner channel went offline; swapping", "kind", "state",
					"account", r.cfg.AccountID, "channel", handle.Channel, "alternatives", len(streams))
				r.stopWatch(ctx)
				r.setState(ctx, StateStarting)
				return nil
			}
		}
	}

	progress, err := r.cfg.Backend.InventoryProgress(ctx, r.cfg.Session)
	if err != nil {
		slog.Error("runner inventory failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		return fmt.Errorf("inventory: %w", err)
	}
	r.mu.Lock()
	r.lastPollAt = time.Now()
	lastScan := r.lastScanTry
	r.mu.Unlock()
	if r.cfg.OnProgress != nil {
		r.cfg.OnProgress(ctx, r.cfg.AccountID, progress)
	}
	// Rebuild the plan from this poll. It never touches current, the handle or
	// the state, so a failure leaves the watch running.
	if r.forceScan.Swap(false) || time.Since(lastScan) >= rescanPace(r.scanEvery()) {
		select {
		case <-r.rescan:
		default:
		}
		if res, err := r.scan(ctx, progress, true); err != nil {
			slog.Warn("runner rescan failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		} else {
			r.changed()
			r.maybePreempt(ctx, res, target)
		}
	}

	r.sweepCompleted(ctx)

	matched := false
	for _, p := range progress {
		if p.BenefitID != benefit.ID {
			continue
		}
		matched = true
		if r.logProgress.Changed(fmt.Sprintf("%s/%d/%t", benefit.ID, p.MinutesWatched, p.Claimed)) {
			slog.Info("runner progress", "kind", "progress", "account", r.cfg.AccountID,
				"benefit", benefit.ID, "min_watched", p.MinutesWatched,
				"required", benefit.RequiredMinutes, "claimed", p.Claimed)
		}
		if n := r.logStepErr.Reset(); n > 0 {
			slog.Info("runner step recovered", "kind", "state", "account", r.cfg.AccountID,
				"suppressed_repeats", n)
		}
		if p.Claimed {
			// Claimed outside this watch (another client, or a lagged earlier
			// claim): record it, never call Claim again.
			slog.Info("runner benefit already claimed in inventory", "kind", "claim",
				"account", r.cfg.AccountID, "benefit", benefit.ID, "benefit_name", benefit.Name,
				"channel", handle.Channel)
			r.finishClaim(ctx, benefit, handle.Channel)
			return nil
		}
		r.mu.Lock()
		if p.MinutesWatched > r.lastPollMin {
			r.noAdvance = 0
		} else {
			r.noAdvance++
		}
		noAdvance := r.noAdvance
		r.lastMin = p.MinutesWatched
		r.lastPollMin = p.MinutesWatched
		r.minutes = p.MinutesWatched
		r.noProgress = 0
		if r.current != nil && r.current.Benefit.ID == benefit.ID {
			// Home and Rewards read the current drop's minutes.
			r.current.Minutes = p.MinutesWatched
		}
		if p.InstanceID != "" && r.current != nil {
			r.current.Benefit.InstanceID = p.InstanceID
		}
		r.mu.Unlock()
		r.changed() // polls are the only progress source without PubSub (Kick)

		if p.MinutesWatched >= benefit.RequiredMinutes {
			r.maybeNotifyProgress(ctx, p.MinutesWatched, benefit.RequiredMinutes)
			slog.Info("runner benefit complete, claiming", "kind", "claim", "account", r.cfg.AccountID,
				"benefit", benefit.ID, "benefit_name", benefit.Name, "instance", p.InstanceID, "channel", handle.Channel)
			r.setState(ctx, StateClaiming)
			return nil
		}
		if noAdvance >= freezeThreshold {
			slog.Info("runner benefit frozen (minutes not advancing); rotating channel", "kind", "state",
				"account", r.cfg.AccountID, "benefit", benefit.ID, "benefit_name", benefit.Name,
				"channel", handle.Channel, "stalled_min", p.MinutesWatched, "flat_polls", noAdvance)
			r.stopWatch(ctx)
			r.mu.Lock()
			r.stalled[handle.Channel] = time.Now().Add(stalledChannelCooldown)
			r.noAdvance = 0
			r.mu.Unlock()
			r.setState(ctx, StateStarting)
			return nil
		}
	}

	if !matched {
		if len(progress) > 0 {
			ids := make([]string, 0, len(progress))
			for _, p := range progress {
				ids = append(ids, fmt.Sprintf("%s(%dm)", p.BenefitID, p.MinutesWatched))
			}
			slog.Debug("runner inventory did not contain watched benefit", "kind", "progress",
				"account", r.cfg.AccountID, "benefit", benefit.ID, "inventory_returned", ids)
		}
		r.mu.Lock()
		r.noProgress++
		n := r.noProgress
		prev := r.lastMin
		r.mu.Unlock()
		r.changed() // LastPollAt moved
		switch {
		case n >= vanishThreshold && prev > 0:
			slog.Info("runner benefit vanished from inventory; treating as externally claimed", "kind", "state",
				"account", r.cfg.AccountID, "benefit", benefit.ID, "benefit_name", benefit.Name,
				"channel", handle.Channel, "last_progress_min", prev)
			r.skipCurrent(ctx, benefit.ID)
			return nil
		case n >= neverEnrolledThreshold && prev == 0:
			slog.Warn("runner benefit never appeared in inventory (claimed or ghost); skipping", "kind", "state",
				"account", r.cfg.AccountID, "benefit", benefit.ID, "benefit_name", benefit.Name,
				"channel", handle.Channel, "ticks_without_progress", n)
			r.skipCurrent(ctx, benefit.ID)
			return nil
		}
	}

	r.mu.Lock()
	cur := r.lastMin
	r.mu.Unlock()
	r.maybeNotifyProgress(ctx, cur, benefit.RequiredMinutes)
	return nil
}

// beat sends one heartbeat on handle and fires OnBeat.
func (r *Runner) beat(ctx context.Context, handle platform.WatchHandle) error {
	if err := r.cfg.Backend.Heartbeat(ctx, handle); err != nil {
		slog.Error("runner heartbeat failed", "kind", "error", "account", r.cfg.AccountID,
			"channel", handle.Channel, "err", err)
		return fmt.Errorf("heartbeat: %w", err)
	}
	slog.Debug("runner heartbeat sent", "kind", "heartbeat", "account", r.cfg.AccountID, "channel", handle.Channel)
	if r.cfg.OnBeat != nil {
		r.cfg.OnBeat(ctx, r.cfg.AccountID)
	}
	return nil
}

// sweepCompleted runs the multi-reward sweep (Kick): one watch advances many
// rewards at once, so it claims every reward that hit 100% and notifies each
// once.
func (r *Runner) sweepCompleted(ctx context.Context) {
	sweeper, ok := r.cfg.Backend.(platform.CompletedSweeper)
	if !ok {
		return
	}
	swept, err := sweeper.SweepCompletedClaims(ctx, r.cfg.Session)
	if err != nil {
		slog.Debug("runner completed-claim sweep failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		return
	}
	for _, cr := range swept {
		slog.Info("runner swept completed reward", "kind", "claim", "account", r.cfg.AccountID, "title", cr.Title)
		r.notifySwept(ctx, cr)
	}
}

// maybePreempt switches drops when Config.Preempt is on and the fresh plan
// ranks another campaign above the current one. It reuses the Mine now
// switch, so pick takes the new top of the queue. A Mine now pin holds, and
// a candidate with no live stream is not worth dropping the watch for. A
// candidate start could not watch after a preempt waits preemptCooldown.
func (r *Runner) maybePreempt(ctx context.Context, res plan.Result, cur plan.Target) {
	if r.cfg.Preempt == nil || !r.cfg.Preempt() {
		return
	}
	r.mu.Lock()
	pinned := r.pinned != nil
	r.mu.Unlock()
	if pinned {
		return
	}
	// The current campaign must still be queued, below the top. A target the
	// plan dropped is handled by the claim and skip paths.
	i := slices.IndexFunc(res.Queue, func(t plan.Target) bool { return t.Campaign.ID == cur.Campaign.ID })
	if i <= 0 {
		return
	}
	top := res.Queue[0]
	r.mu.Lock()
	blocked := time.Now().Before(r.preemptBlocked[top.Campaign.ID])
	r.mu.Unlock()
	if blocked {
		return
	}
	streams, err := r.cfg.Backend.ListEligibleChannels(ctx, r.cfg.Session, top.Campaign)
	if err != nil {
		return
	}
	// Start skips stalled channels, so a candidate with only those is not live.
	r.mu.Lock()
	streams = r.unstalledLocked(streams, time.Now())
	if len(streams) > 0 {
		r.preemptTo = top.Campaign.ID
	}
	r.mu.Unlock()
	if len(streams) == 0 {
		return
	}
	slog.Info("runner preempting drop for a higher-ranked campaign", "kind", "state",
		"account", r.cfg.AccountID, "campaign", cur.Campaign.ID, "drop", cur.Benefit.ID,
		"next_campaign", top.Campaign.ID, "next_drop", top.Benefit.ID)
	r.switchNow.Store(true)
	select {
	case r.switchCh <- struct{}{}:
	default:
	}
}

// unstalledLocked drops expired stalled marks and returns streams without the
// channels still cooling down. Caller holds r.mu.
func (r *Runner) unstalledLocked(streams []platform.Stream, now time.Time) []platform.Stream {
	for ch, until := range r.stalled {
		if now.After(until) {
			delete(r.stalled, ch)
		}
	}
	if len(r.stalled) == 0 {
		return streams
	}
	out := make([]platform.Stream, 0, len(streams))
	for _, s := range streams {
		if until, ok := r.stalled[s.Channel]; ok && now.Before(until) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// skipCurrent gives up on the benefit: remembers and persists the skip, stops
// the watch and returns to picking.
func (r *Runner) skipCurrent(ctx context.Context, benefitID string) {
	r.stopWatch(ctx)
	r.mu.Lock()
	r.skipped[benefitID] = true
	r.current = nil
	r.unpinLocked(benefitID)
	r.mu.Unlock()
	if r.cfg.Skips != nil {
		if err := r.cfg.Skips.RecordSkip(r.cfg.AccountID, benefitID); err != nil {
			slog.Warn("runner persist skip failed; in-memory skip still applies this run",
				"kind", "error", "account", r.cfg.AccountID, "benefit", benefitID, "err", err)
		}
	}
	r.setState(ctx, StatePicking)
}

// clearWatchViewLocked blanks the per-watch snapshot fields. Caller holds r.mu.
func (r *Runner) clearWatchViewLocked() {
	r.channel = ""
	r.viewers = 0
	r.minutes = 0
}

// stopWatch ends the live watch, if any, and drops the channel subscription.
// The current target stays so a restart can resume the same drop; the
// snapshot's channel, viewers and minutes clear until the next watch polls.
func (r *Runner) stopWatch(ctx context.Context) {
	r.mu.Lock()
	h := r.handle
	s := r.stream
	r.handle = nil
	r.stream = nil
	r.clearWatchViewLocked()
	r.mu.Unlock()
	if h == nil {
		return
	}
	// Detached: a cancelled run ctx must not leak the handle or sidecar.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.cfg.Backend.StopWatch(ctx, *h); err != nil {
		slog.Warn("runner stop watch failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
	}
	if cs, ok := r.cfg.Backend.(platform.ChannelSubscriber); ok && s != nil && s.ChannelID != "" {
		cs.UnsubscribeChannel(r.cfg.AccountID, s.ChannelID)
	}
}

// notifyFields builds the notification payload for the current target.
func (r *Runner) notifyFields(extra map[string]any) map[string]any {
	f := map[string]any{"account": r.cfg.AccountID}
	if r.cfg.AccountLabel != "" {
		f["account_label"] = r.cfg.AccountLabel
	}
	if r.cfg.Platform != "" {
		f["platform"] = r.cfg.Platform
	}
	r.mu.Lock()
	if r.current != nil {
		if g := r.current.Campaign.Game; g != "" {
			f["game"] = g
		}
		if n := r.current.Campaign.Name; n != "" {
			f["campaign"] = n
		}
		if n := r.current.Benefit.Name; n != "" {
			f["drop"] = n
		}
		if u := r.current.Benefit.ImageURL; u != "" {
			f["image"] = u
		}
	}
	if r.channel != "" {
		f["channel"] = r.channel
	}
	r.mu.Unlock()
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func (r *Runner) maybeNotifyProgress(ctx context.Context, curMin, reqMin int) {
	if r.cfg.Notifier != nil && r.shouldNotifyProgress(curMin, reqMin) {
		_ = r.cfg.Notifier.Notify(ctx, "progress", r.notifyFields(map[string]any{
			"cur_min": curMin,
			"req_min": reqMin,
		}))
	}
}

// shouldNotifyProgress reports whether a progress notification should fire
// for curMin, recording the milestone when so. Notifications land at 0%, each
// ProgressNotifyStepPct%, and 100%, each at most once per benefit. Step 0
// disables them.
func (r *Runner) shouldNotifyProgress(curMin, reqMin int) bool {
	step := r.cfg.ProgressNotifyStepPct
	if step <= 0 {
		return false
	}
	if step > 100 {
		step = 100
	}
	pct := 0
	if reqMin > 0 {
		pct = curMin * 100 / reqMin
	}
	pct = max(0, min(pct, 100))
	milestone := (pct / step) * step
	if pct >= 100 {
		milestone = 100
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if milestone > r.lastMilestone {
		r.lastMilestone = milestone
		return true
	}
	return false
}
