// Package runner executes the per-account drop plan: pick, watch, claim.
package runner

import (
	"context"
	"errors"
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

// Safeguard values carried over from 1.x. recheckInterval and stepErrBackoff
// are vars so tests can shrink them.
var (
	recheckInterval = 5 * time.Minute
	stepErrBackoff  = 5 * time.Second
)

const (
	maxBackoff = 5 * time.Minute
)

type Runner struct {
	cfg Config

	mu         sync.Mutex
	state      State
	current    *plan.Target
	handle     *platform.WatchHandle
	stream     *platform.Stream
	channel    string
	viewers    int
	minutes    int
	startedAt  time.Time
	lastPollAt time.Time
	lastScanAt time.Time
	queue      []plan.Target
	blocked    []plan.Blocked
	skipped    map[string]bool
	noStream   map[string]bool // campaign IDs with no live channel this cycle

	// claimFailures counts consecutive Claim failures per benefit (Task 5).
	claimFailures map[string]int
	// claimFailSkipped holds benefits skipped for repeated claim failures,
	// value = required minutes (Task 5).
	claimFailSkipped map[string]int

	// Per-watch safeguards (watch.go).
	stalled          map[string]time.Time // channel -> cooldown end
	tickN            int
	noProgress       int // consecutive polls without a row for the benefit
	noAdvance        int // consecutive polls with flat minutes
	lastMin          int
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
	if cfg.Session.Games == nil {
		names := make([]string, 0, len(games))
		for _, g := range games {
			names = append(names, g.Name)
		}
		cfg.Session.Games = names
	}
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

// Stub until Task 5.
func (r *Runner) claim(ctx context.Context) error { return errors.New("not implemented") }

// reapRewards claims one-click reward campaigns. Task 5 fills it in.
func (r *Runner) reapRewards(ctx context.Context) {}

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
	camps, err := r.cfg.Backend.ListActiveCampaigns(ctx, r.cfg.Session)
	if err != nil {
		if errors.Is(err, platform.ErrIntegrityBlocked) {
			slog.Warn("runner integrity blocked; login needed",
				"kind", "auth", "account", r.cfg.AccountID, "err", err)
			r.setState(ctx, StateNeedsLogin)
			return nil
		}
		return err
	}
	var progress []platform.Progress
	inventoryOK := false
	if len(camps) > 0 {
		p, err := r.cfg.Backend.InventoryProgress(ctx, r.cfg.Session)
		if err != nil {
			slog.Warn("runner inventory failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
		} else {
			progress = p
			inventoryOK = true
		}
	}

	if inventoryOK {
		r.selfHeal(progress)
	}
	if r.cfg.OnInventory != nil {
		r.cfg.OnInventory(ctx, r.cfg.AccountID, camps, progress)
	}
	r.reapRewards(ctx)

	var claimed map[string]bool
	if r.cfg.Claims != nil {
		m, err := r.cfg.Claims.ClaimedBenefitIDs(ctx, r.cfg.AccountID)
		if err != nil {
			slog.Warn("runner claim log read failed", "kind", "error", "account", r.cfg.AccountID, "err", err)
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

	r.mu.Lock()
	r.queue = res.Queue
	r.blocked = res.Blocked
	r.lastScanAt = time.Now()
	var next *plan.Target
	for i := range res.Queue {
		if !r.noStream[res.Queue[i].Campaign.ID] {
			t := res.Queue[i]
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
		slog.Info("runner picked drop", "kind", "discovery", "account", r.cfg.AccountID,
			"game", next.Campaign.Game, "campaign", next.Campaign.ID, "drop", next.Benefit.ID)
		r.setState(ctx, StateStarting)
		return nil
	}
	r.current = nil
	r.noStream = map[string]bool{}
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
func (r *Runner) Run(ctx context.Context) error {
	t := time.NewTicker(r.cfg.TickInterval)
	defer t.Stop()
	backoff := time.Duration(0)

	exit := func() error {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r.stopWatch(sctx)
		return ctx.Err()
	}

	for {
		err := r.step(ctx)
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
		}

		var wait <-chan time.Time
		var btimer *time.Timer
		if backoff == 0 {
			wait = t.C
		} else {
			btimer = time.NewTimer(backoff)
			wait = btimer.C
		}
		select {
		case <-ctx.Done():
			if btimer != nil {
				btimer.Stop()
			}
			return exit()
		case <-wait:
		}
		if btimer != nil {
			btimer.Stop()
		}
	}
}
