package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
)

type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

// CampaignPersister is the seam that lets the watcher write every campaign
// it discovers — past, current, and upcoming — to the local DB so the
// /drops page can show them. The watcher only calls this for campaigns
// the per-account whitelist already accepted, so non-whitelisted rows
// NEVER touch the campaigns table.
type CampaignPersister interface {
	PersistCampaigns(ctx context.Context, camps []platform.Campaign) error
}

type Config struct {
	AccountID    string
	Backend      platform.Backend
	Session      platform.Session
	Notifier     Notifier
	TickInterval time.Duration

	// AllowGame returns true if a campaign whose Game string matches
	// the account's whitelist should be considered for mining. When
	// nil the watcher mines anything (legacy behaviour); production
	// passes a function backed by the account_games table.
	//
	// Match by either game.id or game.name — Twitch's GraphQL returns
	// the human-readable Game name on Campaign.Game, while our games
	// table stores both. The check should be lenient.
	AllowGame func(game string) bool

	// GameRank returns the priority of `game` within the whitelist
	// (lower = higher priority). Used to sort matching campaigns.
	// Defaults to math.MaxInt when AllowGame is nil.
	GameRank func(game string) int

	// Persister, when set, receives every campaign the backend discovered
	// after the watcher's whitelist filter has been applied. Used so the
	// /drops page can render past + current + upcoming rows even before
	// anything has been claimed. Non-whitelisted campaigns are NEVER
	// passed to the persister.
	Persister CampaignPersister
}

type Watcher struct {
	cfg Config

	mu    sync.Mutex
	state State

	currentCampaign *platform.Campaign
	currentBenefit  *platform.DropBenefit
	currentStream   *platform.Stream
	handle          *platform.WatchHandle
	watchStartedAt  time.Time
	lastProgressMin int

	// lastDiscovery is the most recent successful
	// Backend.ListActiveCampaigns result. Cached so the dashboard's
	// Active Campaigns panel can union per-account discoveries without
	// duplicating the backend call.
	lastDiscovery   []platform.Campaign
	lastDiscoveryAt time.Time
}

func New(cfg Config) *Watcher {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = time.Minute
	}
	cfg.Session.AccountID = cfg.AccountID
	// Plumb the whitelist into the Session so backends can short-circuit
	// non-whitelisted games before doing per-campaign detail fetches.
	// Same closure backs both layers — the whitelist is canonical.
	if cfg.Session.GameFilter == nil {
		cfg.Session.GameFilter = cfg.AllowGame
	}
	return &Watcher{cfg: cfg, state: StateIdle}
}

func (w *Watcher) State() State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *Watcher) AccountID() string { return w.cfg.AccountID }

// AllowGame exposes the per-account whitelist predicate so external
// consumers (e.g. the dashboard's discovery union) can apply the same
// filter as the watcher. May be nil for legacy "mine anything" config.
func (w *Watcher) AllowGame() func(game string) bool { return w.cfg.AllowGame }

// LastDiscovery returns a copy of the most recent successful
// Backend.ListActiveCampaigns result, plus the time it was captured.
// Returns (nil, zero-time) before the watcher has completed a
// successful pickCampaign tick.
func (w *Watcher) LastDiscovery() ([]platform.Campaign, time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.lastDiscovery) == 0 {
		return nil, w.lastDiscoveryAt
	}
	out := make([]platform.Campaign, len(w.lastDiscovery))
	copy(out, w.lastDiscovery)
	return out, w.lastDiscoveryAt
}

// Snapshot is the dashboard-friendly view of a watcher's in-flight
// state. Safe to call from any goroutine; returns a copy.
type Snapshot struct {
	AccountID       string
	State           string
	CampaignID      string
	CampaignName    string
	CampaignGame    string
	BenefitID       string
	BenefitName     string
	RequiredMinutes int
	MinutesWatched  int
	Channel         string
	ViewerCount     int
	StartedAt       time.Time
}

func (w *Watcher) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := Snapshot{
		AccountID: w.cfg.AccountID,
		State:     w.state.String(),
	}
	if w.currentCampaign != nil {
		s.CampaignID = w.currentCampaign.ID
		s.CampaignName = w.currentCampaign.Name
		s.CampaignGame = w.currentCampaign.Game
	}
	if w.currentBenefit != nil {
		s.BenefitID = w.currentBenefit.ID
		s.BenefitName = w.currentBenefit.Name
		s.RequiredMinutes = w.currentBenefit.RequiredMinutes
	}
	if w.currentStream != nil {
		s.Channel = w.currentStream.Channel
		s.ViewerCount = w.currentStream.ViewerCount
	}
	s.MinutesWatched = w.lastProgressMin
	s.StartedAt = w.watchStartedAt
	return s
}

func (w *Watcher) setState(ctx context.Context, s State) {
	w.mu.Lock()
	prev := w.state
	w.state = s
	w.mu.Unlock()
	slog.Info("watcher state change",
		"kind", "state",
		"account", w.cfg.AccountID,
		"state", s.String(),
		"prev", prev.String())
	_ = w.cfg.Notifier.Notify(ctx, "state", map[string]any{
		"account": w.cfg.AccountID, "state": s.String(),
	})
}

func (w *Watcher) Run(ctx context.Context) error {
	t := time.NewTicker(w.cfg.TickInterval)
	defer t.Stop()

	// Exponential backoff on repeated step errors. Resets to zero
	// after a successful step.
	backoff := time.Duration(0)
	const maxBackoff = 5 * time.Minute

	for {
		err := w.step(ctx)
		if err == nil {
			backoff = 0
		} else {
			if errors.Is(err, errComplete) {
				return nil
			}
			// Transient errors (gql 5xx, sidecar fetch poisoned by
			// PerimeterX, etc) shouldn't kill the watcher. Reset state
			// to PickCampaign for the next tick.
			if backoff == 0 {
				backoff = 5 * time.Second
			} else if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			slog.Warn("watcher step error; will retry after backoff",
				"account", w.cfg.AccountID, "state", w.State().String(),
				"backoff", backoff, "err", err)
			w.setState(ctx, StatePickCampaign)
		}

		// Pick the wait interval: ticker for the fast path, the backoff
		// timer when we're recovering from an error.
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
			return ctx.Err()
		case <-wait:
		}
		if btimer != nil {
			btimer.Stop()
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
	slog.Debug("watcher pickCampaign", "account", w.cfg.AccountID)
	campaigns, err := w.cfg.Backend.ListActiveCampaigns(ctx, w.cfg.Session)
	if err != nil {
		slog.Error("watcher list campaigns failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		return fmt.Errorf("list campaigns: %w", err)
	}
	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		slog.Error("watcher inventory failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		return fmt.Errorf("inventory: %w", err)
	}
	claimed := map[string]bool{}
	for _, p := range progress {
		if p.Claimed {
			claimed[p.BenefitID] = true
		}
	}

	// Apply the per-account whitelist to EVERYTHING the backend returned —
	// active, expired, upcoming. Non-whitelisted campaigns are dropped
	// here so they never reach the persister or the mining loop.
	var whitelisted []platform.Campaign
	if w.cfg.AllowGame != nil {
		whitelisted = make([]platform.Campaign, 0, len(campaigns))
		for _, c := range campaigns {
			if w.cfg.AllowGame(c.Game) {
				whitelisted = append(whitelisted, c)
			}
		}
	} else {
		whitelisted = campaigns
	}

	// Persist every whitelisted campaign (regardless of status) so the
	// /drops page can render past + upcoming tabs. Persistence failures
	// are non-fatal — we still want to mine even if the DB hiccups.
	if w.cfg.Persister != nil && len(whitelisted) > 0 {
		if err := w.cfg.Persister.PersistCampaigns(ctx, whitelisted); err != nil {
			slog.Warn("watcher persist campaigns failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		}
	}

	// Cache the whitelisted discovery for outside readers (dashboard +
	// /drops fallback). Copy first so callers can't mutate our slice.
	cached := make([]platform.Campaign, len(whitelisted))
	copy(cached, whitelisted)
	w.mu.Lock()
	w.lastDiscovery = cached
	w.lastDiscoveryAt = time.Now()
	w.mu.Unlock()

	// For mining, keep only ACTIVE campaigns and sort by whitelist rank
	// (lower = higher priority). Empty Status is treated as "active" for
	// backwards compatibility with the platformtest MockBackend.
	matched := make([]platform.Campaign, 0, len(whitelisted))
	for _, c := range whitelisted {
		if c.Status != "" && c.Status != "active" {
			continue
		}
		matched = append(matched, c)
	}
	if w.cfg.GameRank != nil {
		sort.SliceStable(matched, func(i, j int) bool {
			return w.cfg.GameRank(matched[i].Game) < w.cfg.GameRank(matched[j].Game)
		})
	}
	slog.Info("watcher discovery", "kind", "discovery", "account", w.cfg.AccountID, "campaigns_total", len(campaigns), "campaigns_eligible", len(matched), "claimed_count", len(claimed))

	for _, c := range matched {
		for _, b := range c.Benefits {
			if claimed[b.ID] {
				continue
			}
			campaignCopy, benefitCopy := c, b
			w.mu.Lock()
			w.currentCampaign = &campaignCopy
			w.currentBenefit = &benefitCopy
			w.mu.Unlock()
			slog.Info("watcher picked benefit", "kind", "discovery", "account", w.cfg.AccountID, "campaign", c.Name, "game", c.Game, "benefit", b.ID, "required_min", b.RequiredMinutes)
			w.setState(ctx, StatePickStream)
			return nil
		}
	}
	if w.cfg.AllowGame != nil && len(matched) == 0 && len(campaigns) > 0 {
		slog.Info("watcher: no whitelisted games match active campaigns, sleeping", "account", w.cfg.AccountID, "active_campaigns", len(campaigns))
	} else {
		slog.Info("watcher has no eligible benefit, sleeping", "account", w.cfg.AccountID, "scanned_campaigns", len(matched))
	}
	w.setState(ctx, StateSleeping)
	return nil
}

func (w *Watcher) pickStream(ctx context.Context) error {
	w.mu.Lock()
	camp := *w.currentCampaign
	w.mu.Unlock()
	slog.Debug("watcher pickStream", "account", w.cfg.AccountID, "campaign", camp.Name)

	streams, err := w.cfg.Backend.ListEligibleChannels(ctx, w.cfg.Session, camp)
	if err != nil {
		slog.Error("watcher list channels failed", "kind", "error", "account", w.cfg.AccountID, "campaign", camp.Name, "err", err)
		return fmt.Errorf("list channels: %w", err)
	}
	if len(streams) == 0 {
		slog.Info("watcher no eligible streams live, sleeping", "kind", "discovery", "account", w.cfg.AccountID, "campaign", camp.Name)
		w.setState(ctx, StateSleeping)
		return nil
	}
	s := streams[0]
	slog.Info("watcher starting watch", "kind", "state", "account", w.cfg.AccountID, "channel", s.Channel, "campaign", camp.Name, "eligible_count", len(streams))
	h, err := w.cfg.Backend.StartWatch(ctx, w.cfg.Session, s)
	if err != nil {
		slog.Error("watcher StartWatch failed", "kind", "error", "account", w.cfg.AccountID, "channel", s.Channel, "err", err)
		return fmt.Errorf("start watch: %w", err)
	}
	w.mu.Lock()
	w.currentStream = &s
	w.handle = &h
	w.watchStartedAt = time.Now()
	w.lastProgressMin = 0
	w.mu.Unlock()
	w.setState(ctx, StateWatching)
	return nil
}

func (w *Watcher) tickWatch(ctx context.Context) error {
	w.mu.Lock()
	handle := *w.handle
	benefit := *w.currentBenefit
	w.mu.Unlock()

	if err := w.cfg.Backend.Heartbeat(ctx, handle); err != nil {
		slog.Error("watcher heartbeat failed", "kind", "error", "account", w.cfg.AccountID, "channel", handle.Channel, "err", err)
		return fmt.Errorf("heartbeat: %w", err)
	}

	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		slog.Error("watcher inventory failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		return fmt.Errorf("inventory: %w", err)
	}
	for _, p := range progress {
		if p.BenefitID == benefit.ID {
			slog.Info("watcher progress", "kind", "progress", "account", w.cfg.AccountID, "benefit", benefit.ID, "min_watched", p.MinutesWatched, "required", benefit.RequiredMinutes, "claimed", p.Claimed)
			w.mu.Lock()
			w.lastProgressMin = p.MinutesWatched
			w.mu.Unlock()
			if p.MinutesWatched >= benefit.RequiredMinutes {
				slog.Info("watcher benefit complete, claiming", "kind", "claim", "account", w.cfg.AccountID, "benefit", benefit.ID, "benefit_name", benefit.Name, "channel", handle.Channel)
				w.setState(ctx, StateClaiming)
				return nil
			}
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
		slog.Error("watcher claim failed", "kind", "error", "account", w.cfg.AccountID, "benefit", benefit.ID, "err", err)
		return fmt.Errorf("claim: %w", err)
	}
	_ = w.cfg.Backend.StopWatch(ctx, handle)

	slog.Info("watcher claim recorded",
		"kind", "claim",
		"account", w.cfg.AccountID,
		"benefit", benefit.ID,
		"benefit_name", benefit.Name,
		"channel", handle.Channel)

	_ = w.cfg.Notifier.Notify(ctx, "claim", map[string]any{
		"account": w.cfg.AccountID, "benefit": benefit.ID,
	})

	w.mu.Lock()
	w.currentBenefit = nil
	w.currentStream = nil
	w.handle = nil
	w.mu.Unlock()

	w.setState(ctx, StateIdle)
	return nil
}
