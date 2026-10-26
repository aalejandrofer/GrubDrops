// Package runner executes the per-account drop plan: pick, watch, claim.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

var (
	errIdle = errors.New("idle; recheck later")
	errStop = errors.New("stopped; needs user action") // Run returns nil on it
)

// Safeguard values carried over from 1.x. recheckInterval, stepErrBackoff
// and rescanEvery are vars so tests can shrink them.
var (
	recheckInterval = 5 * time.Minute
	stepErrBackoff  = 5 * time.Second
	// rescanEvery is how often a watching runner rebuilds the plan.
	rescanEvery = 15 * time.Minute
)

const (
	maxBackoff = 5 * time.Minute
	// rewardCodeWindow is how long after a claim a reward code push is
	// still attributed to the claimed target.
	rewardCodeWindow = 10 * time.Minute
)

type Runner struct {
	cfg Config

	mu          sync.Mutex
	state       State
	current     *plan.Target
	handle      *platform.WatchHandle
	stream      *platform.Stream
	channel     string
	viewers     int
	minutes     int
	startedAt   time.Time
	lastPollAt  time.Time
	lastScanAt  time.Time
	lastScanTry time.Time     // last scan attempt, failed or not; paces rescans
	lastBeatAt  time.Time     // when the current watch last beaconed; zero = not yet
	wake        chan struct{} // signalled on every state change, cap 1
	tramp       *trampoline   // PubSub forwarder for this account; nil = none
	queue       []plan.Target
	blocked     []plan.Blocked
	skipped     map[string]bool
	noStream    map[string]bool // campaign IDs with no live channel this cycle

	// claimFailures counts consecutive Claim failures per benefit (Task 5).
	claimFailures map[string]int
	// claimFailSkipped holds benefits skipped for repeated claim failures,
	// value = required minutes (Task 5).
	claimFailSkipped map[string]int
	// notifiedSwept dedupes sweep notifications by reward title, per run.
	notifiedSwept map[string]struct{}
	// lastClaimed is the target the runner last claimed, at lastClaimedAt.
	// A reward code push is attributed to it, never to current.
	lastClaimed   *plan.Target
	lastClaimedAt time.Time

	// Per-watch safeguards (watch.go).
	stalled          map[string]time.Time // channel -> cooldown end
	tickN            int
	noProgress       int // consecutive polls without a row for the benefit
	noAdvance        int // consecutive polls with flat minutes
	lastMin          int // latest minutes, polled or pushed; display and vanish checks
	lastPollMin      int // minutes at the last inventory poll; freeze detection only
	lastMilestone    int // last progress milestone notified; -1 = none
	milestoneBenefit string
}

// New builds a Runner and plumbs the game whitelist into the session.
func New(cfg Config) *Runner {
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = DefaultTick
	}
	games := cfg.Games
	cfg.Session.AccountID = cfg.AccountID
	// Always from cfg.Games: a stale Session.Games must not widen or narrow the scan.
	names := make([]string, 0, len(games))
	for _, g := range games {
		names = append(names, g.Name)
	}
	cfg.Session.Games = names
	if cfg.Session.GameFilter == nil {
		cfg.Session.GameFilter = func(game string) bool {
			_, ok := plan.Match(games, game)
			return ok
		}
	}
	r := &Runner{
		cfg:      cfg,
		state:    StateIdle,
		skipped:  map[string]bool{},
		noStream: map[string]bool{},
		stalled:  map[string]time.Time{},
		wake:     make(chan struct{}, 1),

		lastMilestone: -1,
	}
	if cfg.Skips != nil {
		m, err := cfg.Skips.Skips(cfg.AccountID)
		if err != nil {
			slog.Warn("runner load skips failed", "kind", "error", "account", cfg.AccountID, "err", err)
		}
		for id, v := range m {
			if v {
				r.skipped[id] = true
			}
		}
	}
	r.registerPubSub()
	return r
}

// State returns the current state.
func (r *Runner) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Snapshot returns a copy of the observable state.
func (r *Runner) Snapshot() Snapshot {
	r.mu.Lock()
	s := Snapshot{
		AccountID:      r.cfg.AccountID,
		Platform:       r.cfg.Platform,
		State:          r.state,
		Channel:        r.channel,
		ViewerCount:    r.viewers,
		MinutesWatched: r.minutes,
		StartedAt:      r.startedAt,
		LastPollAt:     r.lastPollAt,
		LastScanAt:     r.lastScanAt,
		Blocked:        append([]plan.Blocked(nil), r.blocked...),
	}
	if !r.lastScanTry.IsZero() {
		s.NextScanAt = r.lastScanTry.Add(rescanEvery)
	}
	var rest []plan.Target
	if r.current != nil {
		c := *r.current
		s.Current = &c
	}
	for _, t := range r.queue {
		if r.current != nil && t.Benefit.ID == r.current.Benefit.ID {
			continue
		}
		rest = append(rest, t)
	}
	r.mu.Unlock()
	if r.cfg.Order != nil {
		rest = plan.ApplyOrder(rest, r.cfg.Order())
	}
	s.UpNext = rest
	return s
}

func (r *Runner) changed() {
	if r.cfg.OnChange != nil {
		r.cfg.OnChange(r.Snapshot())
	}
}

func (r *Runner) setState(ctx context.Context, s State) {
	r.mu.Lock()
	prev := r.state
	r.state = s
	r.mu.Unlock()
	if s != prev {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
	slog.Info("runner state change",
		"kind", "state", "account", r.cfg.AccountID, "state", string(s), "prev", string(prev))
	if r.cfg.Notifier != nil {
		_ = r.cfg.Notifier.Notify(ctx, "state", map[string]any{
			"account": r.cfg.AccountID, "state": string(s),
		})
	}
	r.changed()
}

func (r *Runner) step(ctx context.Context) error {
	switch r.State() {
	case StateIdle, StatePicking:
		return r.pick(ctx)
	case StateStarting:
		return r.start(ctx)
	case StateWatching:
		return r.tick(ctx)
	case StateClaiming:
		return r.claim(ctx)
	case StateSleeping, StateAwaitingConnect:
		return errIdle
	case StateNeedsLogin, StateNoGames:
		return errStop
	}
	return errIdle
}

func (r *Runner) clearSkip(id string) {
	r.mu.Lock()
	delete(r.skipped, id)
	r.mu.Unlock()
	if r.cfg.Skips != nil {
		if err := r.cfg.Skips.ClearSkip(r.cfg.AccountID, id); err != nil {
			slog.Warn("runner clear skip failed", "kind", "error", "account", r.cfg.AccountID, "benefit", id, "err", err)
		}
	}
}

func (r *Runner) pick(ctx context.Context) error {
	if len(r.cfg.Games) == 0 {
		r.setState(ctx, StateNoGames)
		return nil
	}
	res, err := r.scan(ctx, nil, false)
	if err != nil {
		if errors.Is(err, platform.ErrIntegrityBlocked) {
			slog.Warn("runner integrity blocked; login needed",
				"kind", "auth", "account", r.cfg.AccountID, "err", err)
			r.setState(ctx, StateNeedsLogin)
			return nil
		}
		return err
	}

	r.mu.Lock()
	// A fresh pick retries every campaign: start keeps its own no-stream set
	// while it walks the queue, so a campaign offline earlier is not starved.
	r.noStream = map[string]bool{}
	var next *plan.Target
	if len(res.Queue) > 0 {
		t := res.Queue[0]
		next = &t
	}
	if next != nil {
		r.current = next
		r.minutes = next.Minutes
		r.channel = ""
		r.viewers = 0
		r.mu.Unlock()
		slog.Info("runner picked drop", "kind", "discovery", "account", r.cfg.AccountID,
			"game", next.Campaign.Game, "campaign", next.Campaign.ID, "drop", next.Benefit.ID)
		r.setState(ctx, StateStarting)
		return nil
	}
	r.current = nil
	r.clearWatchViewLocked()
	awaiting := false
	if len(res.Queue) == 0 {
		for _, b := range res.Blocked {
			if b.Reason == plan.ReasonNotLinked {
				awaiting = true
				break
			}
		}
	}
	r.mu.Unlock()
	if awaiting {
		r.setState(ctx, StateAwaitingConnect)
	} else {
		r.setState(ctx, StateSleeping)
	}
	return nil
}

// scan lists campaigns, reads the inventory (unless the caller already has
// it), builds the plan and stores the queue. It calls OnScan with the result,
// or with Err set when the campaign list or the claim log read fails. It never touches current, the
// watch handle or the state.
func (r *Runner) scan(ctx context.Context, progress []platform.Progress, haveProgress bool) (plan.Result, error) {
	// Stamped before the call so a failed scan also waits rescanEvery.
	r.mu.Lock()
	r.lastScanTry = time.Now()
	r.mu.Unlock()
	camps, err := r.cfg.Backend.ListActiveCampaigns(ctx, r.cfg.Session)
	if err != nil {
		if r.cfg.OnScan != nil {
			r.cfg.OnScan(ctx, Scan{AccountID: r.cfg.AccountID, Platform: r.cfg.Platform, At: time.Now(), Err: err})
		}
		return plan.Result{}, err
	}
	inventoryOK := haveProgress
	inventoryErr := ""
	if !haveProgress {
		progress = nil
		if len(camps) > 0 {
			p, err := r.cfg.Backend.InventoryProgress(ctx, r.cfg.Session)
			if err != nil {
				slog.Warn("runner inventory failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
				inventoryErr = err.Error()
			} else {
				progress = p
				inventoryOK = true
			}
		}
	}

	if inventoryOK {
		r.selfHeal(progress)
	}
	r.reapRewards(ctx, camps)

	var claimed map[string]bool
	if r.cfg.Claims != nil {
		m, err := r.cfg.Claims.ClaimedBenefitIDs(ctx, r.cfg.AccountID)
		if err != nil {
			// Planning without the log could re-queue a just-claimed drop:
			// fail the scan and keep the previous queue.
			slog.Warn("runner claim log read failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
			err = fmt.Errorf("claim log: %w", err)
			if r.cfg.OnScan != nil {
				r.cfg.OnScan(ctx, Scan{AccountID: r.cfg.AccountID, Platform: r.cfg.Platform, At: time.Now(), Err: err})
			}
			return plan.Result{}, err
		}
		claimed = m
	}

	var order []string
	if r.cfg.Order != nil {
		order = r.cfg.Order()
	}
	r.mu.Lock()
	skipped := make(map[string]bool, len(r.skipped))
	for id := range r.skipped {
		skipped[id] = true
	}
	r.mu.Unlock()
	res := plan.Build(plan.Input{
		Games:       r.cfg.Games,
		Campaigns:   camps,
		Progress:    progress,
		Claimed:     claimed,
		Skipped:     skipped,
		ForceLinked: r.cfg.ForceLinked,
		Order:       order,
	})

	now := time.Now()
	r.mu.Lock()
	r.queue = res.Queue
	r.blocked = res.Blocked
	r.lastScanAt = now
	r.mu.Unlock()
	if r.cfg.OnScan != nil {
		r.cfg.OnScan(ctx, Scan{
			AccountID: r.cfg.AccountID, Platform: r.cfg.Platform, At: now,
			Campaigns: camps, Progress: progress, InventoryOK: inventoryOK,
			InventoryErr: inventoryErr, Logged: claimed, Result: res,
		})
	}
	return res, nil
}

// selfHeal clears skips for benefits that are tracked and unclaimed in a
// trustworthy inventory read. A claim-failure skip stays while its minutes
// are still at or above the requirement.
func (r *Runner) selfHeal(progress []platform.Progress) {
	minutes := make(map[string]int, len(progress))
	claimed := map[string]bool{}
	for _, p := range progress {
		minutes[p.BenefitID] = p.MinutesWatched
		if p.Claimed {
			claimed[p.BenefitID] = true
		}
	}
	var clear []string
	r.mu.Lock()
	for _, p := range progress {
		id := p.BenefitID
		if claimed[id] {
			continue
		}
		if req, failed := r.claimFailSkipped[id]; failed {
			if minutes[id] >= req {
				continue
			}
			delete(r.claimFailSkipped, id)
		}
		if r.skipped[id] {
			clear = append(clear, id)
		}
	}
	r.mu.Unlock()
	for _, id := range clear {
		slog.Info("runner un-skipped benefit back in inventory",
			"kind", "state", "account", r.cfg.AccountID, "benefit", id)
		r.clearSkip(id)
	}
}

// nextBackoff returns stepErrBackoff for 0, else doubles cur up to maxBackoff.
func nextBackoff(cur time.Duration) time.Duration {
	if cur == 0 {
		return stepErrBackoff
	}
	return min(cur*2, maxBackoff)
}

// Run drives step until ctx ends or a step needs user action.
//
// Transitions run back to back: a step that changed state (pick to start,
// start to watching, watching to claiming, claim to pick, any rotation) is
// followed at once by the next step, so no drop waits a beacon interval
// between pick, start, first beacon and claim. Only watching waits, and only
// for the next beacon: the first beacon of a watch goes out right after
// StartWatch, later ones every TickInterval. A step that left a non-watching
// state unchanged waits TickInterval so it cannot spin.
func (r *Runner) Run(ctx context.Context) error {
	defer func() {
		if p := recover(); p != nil {
			r.cleanupAfterPanic()
			panic(p)
		}
	}()
	backoff := time.Duration(0)

	exit := func() error {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r.stopWatch(sctx)
		return ctx.Err()
	}

	for {
		if ctx.Err() != nil {
			return exit()
		}
		before := r.State()
		err := r.step(ctx)
		after := r.State()
		switch {
		case err == nil:
			backoff = 0
		case errors.Is(err, errStop):
			return nil
		case errors.Is(err, errIdle):
			backoff = 0
			timer := time.NewTimer(recheckInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return exit()
			case <-timer.C:
			}
			r.setState(ctx, StatePicking)
			continue
		default:
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return exit()
			}
			backoff = nextBackoff(backoff)
			slog.Warn("runner step error; retrying after backoff",
				"kind", "error", "account", r.cfg.AccountID, "state", string(r.State()),
				"backoff", backoff, "err", err)
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			r.stopWatch(sctx)
			cancel()
			r.setState(ctx, StatePicking)
			if !r.wait(ctx, backoff, StatePicking) {
				return exit()
			}
			continue
		}

		var d time.Duration
		switch {
		case after == StateWatching:
			d = r.untilNextBeat()
		case after != before:
			continue
		default:
			d = r.cfg.TickInterval
		}
		if !r.wait(ctx, d, after) {
			return exit()
		}
	}
}

// cleanupAfterPanic stops the live watch and leaves a non-watching state so
// a panicking step cannot leak the handle or sidecar. If r.mu stays held (the
// panic unwound a locked section) it gives up rather than deadlock.
func (r *Runner) cleanupAfterPanic() {
	deadline := time.Now().Add(time.Second)
	for !r.mu.TryLock() {
		if time.Now().After(deadline) {
			slog.Error("runner panic cleanup skipped; state lock held",
				"kind", "error", "account", r.cfg.AccountID)
			return
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Unlock()
	r.stopWatch(context.Background()) // detached internally
	r.mu.Lock()
	prev := r.state
	r.state = StateIdle
	r.mu.Unlock()
	slog.Error("runner panicked; watch stopped", "kind", "error",
		"account", r.cfg.AccountID, "prev", string(prev))
	r.changed()
}

// untilNextBeat is the time left before the current watch may beacon again.
// Zero before the first beacon of a watch.
func (r *Runner) untilNextBeat() time.Duration {
	r.mu.Lock()
	last := r.lastBeatAt
	r.mu.Unlock()
	if last.IsZero() {
		return 0
	}
	return time.Until(last.Add(r.cfg.TickInterval))
}

// wait sleeps d, returning early when the state moves off st (a PubSub claim
// or stream-down push). It returns false when ctx ends.
func (r *Runner) wait(ctx context.Context, d time.Duration, st State) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		case <-r.wake:
			if r.State() != st {
				return true
			}
		}
	}
}
