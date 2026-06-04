package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

type Config struct {
	AccountID    string
	Backend      platform.Backend
	Session      platform.Session
	Notifier     Notifier
	TickInterval time.Duration
}

type Watcher struct {
	cfg Config

	mu    sync.Mutex
	state State

	currentCampaign *platform.Campaign
	currentBenefit  *platform.DropBenefit
	currentStream   *platform.Stream
	handle          *platform.WatchHandle
}

func New(cfg Config) *Watcher {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = time.Minute
	}
	return &Watcher{cfg: cfg, state: StateIdle}
}

func (w *Watcher) State() State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *Watcher) setState(s State) {
	w.mu.Lock()
	w.state = s
	w.mu.Unlock()
	_ = w.cfg.Notifier.Notify(context.Background(), "state", map[string]any{
		"account": w.cfg.AccountID, "state": s.String(),
	})
}

func (w *Watcher) Run(ctx context.Context) error {
	t := time.NewTicker(w.cfg.TickInterval)
	defer t.Stop()

	for {
		if err := w.step(ctx); err != nil {
			if errors.Is(err, errComplete) {
				return nil
			}
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

var errComplete = errors.New("nothing left to mine")

func (w *Watcher) step(ctx context.Context) error {
	switch w.State() {
	case StateIdle, StatePickCampaign:
		return w.pickCampaign(ctx)
	case StatePickStream:
		return w.pickStream(ctx)
	case StateWatching:
		return w.tickWatch(ctx)
	case StateClaiming:
		return w.claim(ctx)
	case StateSleeping:
		return errComplete
	case StateAuthRequired, StatePaused:
		return errComplete
	default:
		return fmt.Errorf("unknown state %s", w.State())
	}
}

func (w *Watcher) pickCampaign(ctx context.Context) error {
	campaigns, err := w.cfg.Backend.ListActiveCampaigns(ctx, w.cfg.Session)
	if err != nil {
		return fmt.Errorf("list campaigns: %w", err)
	}
	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	claimed := map[string]bool{}
	for _, p := range progress {
		if p.Claimed {
			claimed[p.BenefitID] = true
		}
	}

	for _, c := range campaigns {
		for _, b := range c.Benefits {
			if claimed[b.ID] {
				continue
			}
			campaignCopy, benefitCopy := c, b
			w.mu.Lock()
			w.currentCampaign = &campaignCopy
			w.currentBenefit = &benefitCopy
			w.mu.Unlock()
			w.setState(StatePickStream)
			return nil
		}
	}
	w.setState(StateSleeping)
	return nil
}

func (w *Watcher) pickStream(ctx context.Context) error {
	w.mu.Lock()
	camp := *w.currentCampaign
	w.mu.Unlock()

	streams, err := w.cfg.Backend.ListEligibleChannels(ctx, w.cfg.Session, camp)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	if len(streams) == 0 {
		w.setState(StateSleeping)
		return nil
	}
	s := streams[0]
	h, err := w.cfg.Backend.StartWatch(ctx, w.cfg.Session, s)
	if err != nil {
		return fmt.Errorf("start watch: %w", err)
	}
	w.mu.Lock()
	w.currentStream = &s
	w.handle = &h
	w.mu.Unlock()
	w.setState(StateWatching)
	return nil
}

func (w *Watcher) tickWatch(ctx context.Context) error {
	w.mu.Lock()
	handle := *w.handle
	benefit := *w.currentBenefit
	w.mu.Unlock()

	if err := w.cfg.Backend.Heartbeat(ctx, handle); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}

	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	for _, p := range progress {
		if p.BenefitID == benefit.ID && p.MinutesWatched >= benefit.RequiredMinutes {
			w.setState(StateClaiming)
			return nil
		}
	}

	_ = w.cfg.Notifier.Notify(ctx, "progress", map[string]any{
		"account": w.cfg.AccountID, "benefit": benefit.ID,
	})
	return nil
}

func (w *Watcher) claim(ctx context.Context) error {
	w.mu.Lock()
	benefit := *w.currentBenefit
	handle := *w.handle
	w.mu.Unlock()

	if err := w.cfg.Backend.Claim(ctx, w.cfg.Session, benefit); err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	_ = w.cfg.Backend.StopWatch(ctx, handle)

	_ = w.cfg.Notifier.Notify(ctx, "claim", map[string]any{
		"account": w.cfg.AccountID, "benefit": benefit.ID,
	})

	w.mu.Lock()
	w.currentBenefit = nil
	w.currentStream = nil
	w.handle = nil
	w.mu.Unlock()

	w.setState(StateIdle)
	return nil
}
