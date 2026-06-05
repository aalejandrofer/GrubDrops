package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
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

	// ExcludeGame, when set and returning true for a game, prevents
	// that game's campaigns from being mined even if AllowGame
	// permits them. Useful for temporarily skipping a game without
	// editing the whitelist (DevilXD parity — P3 exclude-game set).
	// Applied AFTER AllowGame so the whitelist remains canonical.
	ExcludeGame func(game string) bool

	// GameRank returns the priority of `game` within the whitelist
	// (lower = higher priority). Used to sort matching campaigns.
	// Defaults to math.MaxInt when AllowGame is nil.
	GameRank func(game string) int

	// PriorityMode picks the ordering policy when multiple
	// whitelisted campaigns are eligible. "ordered" sorts by
	// GameRank (whitelist top-down); "ending_soonest" sorts by the
	// campaign's EndsAt ascending. Empty defaults to "ordered".
	PriorityMode string

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
	tickCount       int // increments each tickWatch, used to throttle stream-live re-checks
	// noProgressTicks counts consecutive tickWatch calls where
	// InventoryProgress returned NO row matching the current benefit ID.
	// Reset to 0 on any match; on pickStream when starting a fresh watch.
	// When the count exceeds vanishThreshold AND we previously saw
	// progress for this benefit, the watcher treats the benefit as
	// externally completed (code-style Twitch drop claimed manually +
	// dropped from dropCampaignsInProgress, or campaign expired
	// mid-watch) and re-enters PickCampaign instead of mining forever
	// against a ghost benefit (B2.5).
	noProgressTicks int

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
	w := &Watcher{cfg: cfg, state: StateIdle}
	// Register PubSub hooks BEFORE the first ListActiveCampaigns call so
	// the backend's lazy PubSub bootstrap picks them up. Backends that
	// don't implement PubSubAware (Kick, mock) silently skip this.
	if pa, ok := cfg.Backend.(platform.PubSubAware); ok {
		pa.SetAccountPubSubHooks(cfg.AccountID, platform.PubSubHooks{
			OnDropProgress: w.handlePubSubDropProgress,
			OnDropClaim:    w.handlePubSubDropClaim,
			OnStreamDown:   w.handlePubSubStreamDown,
			OnStreamUp:     w.handlePubSubStreamUp,
		})
	}
	return w
}

// handlePubSubDropProgress is invoked from the PubSub read loop when a
// drop-progress message arrives. Updates lastProgressMin so the
// dashboard reflects real-time progress without waiting for the next
// inventory poll, and resets the vanish-detect counter (B2.5).
func (w *Watcher) handlePubSubDropProgress(dropID string, curMin, _ int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.currentBenefit == nil || w.currentBenefit.ID != dropID {
		return
	}
	w.lastProgressMin = int(curMin)
	w.noProgressTicks = 0
}

// handlePubSubDropClaim is invoked when Twitch emits a drop-claim
// event. Captures the per-account drop_instance_id and fast-paths the
// watcher into StateClaiming so the next step issues the claim
// mutation without waiting for an inventory poll.
func (w *Watcher) handlePubSubDropClaim(dropID, instanceID string) {
	w.mu.Lock()
	if w.currentBenefit == nil || w.currentBenefit.ID != dropID {
		w.mu.Unlock()
		return
	}
	if instanceID != "" {
		w.currentBenefit.InstanceID = instanceID
	}
	w.mu.Unlock()
	w.setState(context.Background(), StateClaiming)
}

// handlePubSubStreamDown fires when the channel we're watching emits a
// video-playback stream-down. Stops the current watch and re-enters
// PickStream so the watcher swaps to another live broadcaster without
// waiting for the periodic liveness probe.
func (w *Watcher) handlePubSubStreamDown(channelID string) {
	w.mu.Lock()
	if w.currentStream == nil || w.currentStream.ChannelID != channelID || w.handle == nil {
		w.mu.Unlock()
		return
	}
	handle := *w.handle
	w.mu.Unlock()
	_ = w.cfg.Backend.StopWatch(context.Background(), handle)
	if cs, ok := w.cfg.Backend.(platform.ChannelSubscriber); ok && channelID != "" {
		cs.UnsubscribeChannel(w.cfg.AccountID, channelID)
	}
	w.setState(context.Background(), StatePickStream)
}

// handlePubSubStreamUp is currently a noop — pickStream will discover
// the channel naturally on the next tick. Reserved for future
// back-off reset logic.
func (w *Watcher) handlePubSubStreamUp(_ string) {}

// campaignMinRemaining returns the smallest (RequiredMinutes -
// MinutesWatched) across the campaign's benefits. Benefits with no
// recorded progress are treated as if they need their full required
// time. Returns MaxInt32 when the campaign has no benefits (so it
// sorts last). Used by P5 — get_active_campaign remaining-minutes
// tiebreak.
func campaignMinRemaining(c platform.Campaign, progressByID map[string]int) int {
	const maxInt = 1 << 30
	best := maxInt
	for _, b := range c.Benefits {
		watched := progressByID[b.ID]
		remain := b.RequiredMinutes - watched
		if remain < 0 {
			remain = 0
		}
		if remain < best {
			best = remain
		}
	}
	return best
}

// unsubscribeCurrentChannel drops the active video-playback PubSub
// subscription if the backend supports it. Safe to call when no stream
// is selected — silently noops.
func (w *Watcher) unsubscribeCurrentChannel() {
	cs, ok := w.cfg.Backend.(platform.ChannelSubscriber)
	if !ok {
		return
	}
	w.mu.Lock()
	var channelID string
	if w.currentStream != nil {
		channelID = w.currentStream.ChannelID
	}
	w.mu.Unlock()
	if channelID == "" {
		return
	}
	cs.UnsubscribeChannel(w.cfg.AccountID, channelID)
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
			// Scheduler.Reload (e.g. after a fresh login) tears down
			// existing watcher contexts. The in-flight RPC returns
			// "context canceled"; that's not a real error, just our
			// own teardown propagating. Exit cleanly so the next
			// builder spins up a fresh entry without spamming
			// ERROR/WARN lines.
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return ctx.Err()
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

// vanishThreshold is how many consecutive tickWatch calls must report
// "no progress row for current benefit" before the watcher concludes
// the benefit has been externally claimed or expired. 3 ticks at the
// default minute-cadence ≈ 3 minutes of grace.
const vanishThreshold = 3

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
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("list campaigns: %w", err)
		}
		slog.Error("watcher list campaigns failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		return fmt.Errorf("list campaigns: %w", err)
	}
	// Inventory is only meaningful as a dedupe filter against discovered
	// campaigns. Skip when discovery returned nothing so a transient
	// inventory backend failure (PerimeterX, CSP) doesn't poison an
	// otherwise-empty cycle. When inventory fails with campaigns
	// present, log + continue with empty progress — we'd rather re-mine
	// a benefit than stall the watcher.
	var progress []platform.Progress
	if len(campaigns) > 0 {
		progress, err = w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
		if err != nil {
			slog.Warn("watcher inventory failed; treating as no progress yet", "kind", "error", "account", w.cfg.AccountID, "err", err)
			progress = nil
		}
	}
	claimed := map[string]bool{}
	for _, p := range progress {
		if p.Claimed {
			claimed[p.BenefitID] = true
		}
	}

	// Persist EVERY campaign the backend returned — whitelisted and
	// non-whitelisted alike. The /drops page renders whitelisted ones
	// in the main tabs and non-whitelisted ones in the Discoverable
	// tab. Persistence failures are non-fatal — we still want to mine
	// even if the DB hiccups.
	if w.cfg.Persister != nil && len(campaigns) > 0 {
		if err := w.cfg.Persister.PersistCampaigns(ctx, campaigns); err != nil {
			slog.Warn("watcher persist campaigns failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		}
	}

	// Apply the per-account whitelist to EVERYTHING the backend returned —
	// active, expired, upcoming. Non-whitelisted campaigns are dropped
	// here so they never reach the mining loop. (They DID reach the
	// persister above so /drops Discoverable can list them.)
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

	// Cache the FULL (unfiltered) discovery so the dashboard's
	// DiscoverySnapshot can compute SourceAccounts — accounts whose
	// backend saw a campaign even if they don't have its game
	// whitelisted. EligibleAccounts is computed downstream by re-applying
	// each watcher's AllowGame to this cache. Copy first so callers
	// can't mutate our slice.
	cached := make([]platform.Campaign, len(campaigns))
	copy(cached, campaigns)
	w.mu.Lock()
	w.lastDiscovery = cached
	w.lastDiscoveryAt = time.Now()
	w.mu.Unlock()

	// For mining, keep only ACTIVE + ACCOUNT-LINKED campaigns. Sort by
	// whitelist rank (lower = higher priority). Empty Status is treated
	// as "active" for backwards compatibility with the platformtest
	// MockBackend. Non-linked campaigns stay visible in the discovery
	// cache (so the dashboard can prompt "Link account →") but the
	// watcher skips them — minutes watched on an unlinked campaign
	// don't translate to a claimable drop.
	matched := make([]platform.Campaign, 0, len(whitelisted))
	skippedUnlinked := 0
	skippedReward := 0
	for _, c := range whitelisted {
		if c.Status != "" && c.Status != "active" {
			continue
		}
		// Reward campaigns are one-click claims from /drops/inventory
		// — no watch-time accrues. Drop them from the mining loop;
		// the reward reaper handles them out-of-band.
		if c.Kind == "reward" {
			skippedReward++
			continue
		}
		// AccountLinked defaults false for platforms that don't
		// surface the flag yet (e.g. Kick), AND for backends that
		// haven't populated it. Twitch DOES populate it. To avoid
		// regressing Kick mining, only skip when the field is
		// explicitly present and false — gated by Platform.
		if c.Platform == "twitch" && !c.AccountLinked {
			skippedUnlinked++
			continue
		}
		// P3: exclude-game set short-circuits the pick. Whitelist
		// already passed; exclude is a finer-grained skip without
		// rewriting the per-account games table.
		if w.cfg.ExcludeGame != nil && w.cfg.ExcludeGame(c.Game) {
			continue
		}
		matched = append(matched, c)
	}
	if skippedUnlinked > 0 {
		slog.Info("watcher skipped unlinked campaigns",
			"kind", "discovery",
			"account", w.cfg.AccountID,
			"count", skippedUnlinked)
	}
	if skippedReward > 0 {
		slog.Info("watcher skipped reward campaigns",
			"kind", "discovery",
			"account", w.cfg.AccountID,
			"count", skippedReward)
	}
	// Reaper: fires whenever the account has at least one whitelisted
	// campaign whose benefits we can't mine (kind="reward" OR
	// scrape-synth ID with no benefits). The reaper itself filters by
	// /drops/inventory state — it just clicks visible Claim buttons,
	// so over-firing is harmless. Backends that don't support reward
	// claiming (Kick) drop through via the type-assert.
	if rc, ok := w.cfg.Backend.(platform.RewardClaimer); ok {
		needsReap := false
		allowed := make([]string, 0, len(whitelisted))
		seen := map[string]bool{}
		for _, c := range whitelisted {
			isReward := c.Kind == "reward" || len(c.Benefits) == 0
			if !isReward {
				continue
			}
			needsReap = true
			if !seen[c.Game] {
				allowed = append(allowed, c.Game)
				seen[c.Game] = true
			}
		}
		if needsReap {
			sess := w.cfg.Session
			sess.AccountID = w.cfg.AccountID
			claimed, err := rc.ClaimRewards(ctx, sess, allowed)
			if err != nil {
				slog.Warn("watcher reward reaper failed",
					"kind", "error",
					"account", w.cfg.AccountID,
					"err", err)
			} else if len(claimed) > 0 {
				for _, cr := range claimed {
					slog.Info("watcher reward claimed",
						"kind", "claim",
						"account", w.cfg.AccountID,
						"game", cr.Game,
						"title", cr.Title)
				}
			} else {
				slog.Info("watcher reward reaper: nothing to claim",
					"kind", "discovery",
					"account", w.cfg.AccountID,
					"games", allowed)
			}
		}
	}
	// progressByID feeds the remaining-minutes tiebreak (P5). Built
	// once outside the sort comparator so the closures stay O(1).
	progressByID := map[string]int{}
	for _, p := range progress {
		progressByID[p.BenefitID] = p.MinutesWatched
	}
	if w.cfg.PriorityMode == "ending_soonest" {
		sort.SliceStable(matched, func(i, j int) bool {
			// Treat 0/missing EndsAt as MaxInt so they sort last —
			// don't pick a campaign whose end we don't know first.
			ai := matched[i].EndsAt
			aj := matched[j].EndsAt
			if ai.IsZero() && aj.IsZero() {
				return campaignMinRemaining(matched[i], progressByID) < campaignMinRemaining(matched[j], progressByID)
			}
			if ai.IsZero() {
				return false
			}
			if aj.IsZero() {
				return true
			}
			if ai.Equal(aj) {
				return campaignMinRemaining(matched[i], progressByID) < campaignMinRemaining(matched[j], progressByID)
			}
			return ai.Before(aj)
		})
	} else if w.cfg.PriorityMode == "low_avbl_first" {
		// DevilXD LOW_AVBL_FIRST: prefer campaigns whose allow-list is
		// smaller (scarcer broadcasters). 0 means "any channel for the
		// game" — treat as effectively infinite so unrestricted
		// campaigns sort last. Ties fall back to fewest-remaining-min
		// (P5) so we finish the closest-to-claim benefit first.
		sort.SliceStable(matched, func(i, j int) bool {
			ai := matched[i].AllowedChannelCount
			aj := matched[j].AllowedChannelCount
			if ai == 0 {
				ai = 1 << 30
			}
			if aj == 0 {
				aj = 1 << 30
			}
			if ai == aj {
				return campaignMinRemaining(matched[i], progressByID) < campaignMinRemaining(matched[j], progressByID)
			}
			return ai < aj
		})
	} else if w.cfg.GameRank != nil {
		sort.SliceStable(matched, func(i, j int) bool {
			ri := w.cfg.GameRank(matched[i].Game)
			rj := w.cfg.GameRank(matched[j].Game)
			if ri == rj {
				// P5 tiebreak: same whitelist rank → prefer the
				// campaign with the fewest minutes remaining to claim
				// (already in progress > unstarted).
				return campaignMinRemaining(matched[i], progressByID) < campaignMinRemaining(matched[j], progressByID)
			}
			return ri < rj
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
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("list channels: %w", err)
		}
		slog.Error("watcher list channels failed", "kind", "error", "account", w.cfg.AccountID, "campaign", camp.Name, "err", err)
		return fmt.Errorf("list channels: %w", err)
	}
	if len(streams) == 0 {
		slog.Info("watcher no eligible streams live, sleeping", "kind", "discovery", "account", w.cfg.AccountID, "campaign", camp.Name)
		w.setState(ctx, StateSleeping)
		return nil
	}
	// Prefer highest-viewer-count streams: they stay live longer +
	// have steadier metadata, so the watcher swaps less often.
	// Backends already sort by VIEWER_COUNT desc when querying the
	// directory page; redo it here so allow-list / sparse-API paths
	// also benefit.
	sort.SliceStable(streams, func(i, j int) bool {
		return streams[i].ViewerCount > streams[j].ViewerCount
	})
	// P4: if the backend supports AvailableDrops, walk the sorted list
	// until we find a channel that actually serves the target drop.
	// Errors and "unknown" responses fall back to the head pick.
	w.mu.Lock()
	target := ""
	if w.currentBenefit != nil {
		target = w.currentBenefit.ID
	}
	w.mu.Unlock()
	s := streams[0]
	if checker, ok := w.cfg.Backend.(platform.AvailableDropsChecker); ok && target != "" {
		for _, cand := range streams {
			if cand.ChannelID == "" {
				continue
			}
			drops, err := checker.AvailableDropIDs(ctx, w.cfg.Session, cand.ChannelID)
			if err != nil {
				slog.Debug("watcher AvailableDropIDs failed; falling back", "account", w.cfg.AccountID, "channel", cand.Channel, "err", err)
				break
			}
			if len(drops) == 0 {
				// "unknown" — accept this channel and move on.
				s = cand
				break
			}
			if _, hit := drops[target]; hit {
				s = cand
				break
			}
		}
	}
	slog.Info("watcher starting watch", "kind", "state", "account", w.cfg.AccountID, "channel", s.Channel, "campaign", camp.Name, "eligible_count", len(streams))
	h, err := w.cfg.Backend.StartWatch(ctx, w.cfg.Session, s)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("start watch: %w", err)
		}
		slog.Error("watcher StartWatch failed", "kind", "error", "account", w.cfg.AccountID, "channel", s.Channel, "err", err)
		return fmt.Errorf("start watch: %w", err)
	}
	w.mu.Lock()
	w.currentStream = &s
	w.handle = &h
	w.watchStartedAt = time.Now()
	w.lastProgressMin = 0
	w.noProgressTicks = 0
	w.mu.Unlock()
	// Subscribe to video-playback PubSub so stream-down events fire
	// the moment Twitch flips the broadcast off, without waiting for
	// the periodic liveness probe (F2).
	if cs, ok := w.cfg.Backend.(platform.ChannelSubscriber); ok && s.ChannelID != "" {
		cs.SubscribeChannel(w.cfg.AccountID, s.ChannelID)
	}
	w.setState(ctx, StateWatching)
	return nil
}

func (w *Watcher) tickWatch(ctx context.Context) error {
	w.mu.Lock()
	handle := *w.handle
	benefit := *w.currentBenefit
	campaign := *w.currentCampaign
	w.tickCount++
	tickN := w.tickCount
	w.mu.Unlock()

	if err := w.cfg.Backend.Heartbeat(ctx, handle); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("heartbeat: %w", err)
		}
		slog.Error("watcher heartbeat failed", "kind", "error", "account", w.cfg.AccountID, "channel", handle.Channel, "err", err)
		return fmt.Errorf("heartbeat: %w", err)
	}

	// Every 5 ticks (~2.5s at 500ms cadence), check that the channel
	// we're watching is still live + still eligible. Twitch's
	// SendEvents heartbeat is fire-and-forget — minutes don't accrue
	// after a stream ends but the mutation keeps returning 200, so we
	// need this active probe to know when to swap.
	if tickN%5 == 0 {
		streams, err := w.cfg.Backend.ListEligibleChannels(ctx, w.cfg.Session, campaign)
		if err == nil {
			stillLive := false
			for _, s := range streams {
				if s.Channel == handle.Channel {
					stillLive = true
					break
				}
			}
			if !stillLive {
				slog.Info("watcher channel went offline; swapping",
					"kind", "state",
					"account", w.cfg.AccountID,
					"channel", handle.Channel,
					"alternatives", len(streams))
				_ = w.cfg.Backend.StopWatch(ctx, handle)
				w.unsubscribeCurrentChannel()
				w.setState(ctx, StatePickStream)
				return nil
			}
		}
	}

	progress, err := w.cfg.Backend.InventoryProgress(ctx, w.cfg.Session)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("inventory: %w", err)
		}
		slog.Error("watcher inventory failed", "kind", "error", "account", w.cfg.AccountID, "err", err)
		return fmt.Errorf("inventory: %w", err)
	}
	matched := false
	for _, p := range progress {
		if p.BenefitID == benefit.ID {
			matched = true
			slog.Info("watcher progress", "kind", "progress", "account", w.cfg.AccountID, "benefit", benefit.ID, "min_watched", p.MinutesWatched, "required", benefit.RequiredMinutes, "claimed", p.Claimed)
			w.mu.Lock()
			w.lastProgressMin = p.MinutesWatched
			w.noProgressTicks = 0
			// Capture the per-account drop-instance ID at progress
			// time so Backend.Claim can send it. Without this Twitch
			// rejects claims with INVALID_DROP_INSTANCE.
			if p.InstanceID != "" && w.currentBenefit != nil {
				w.currentBenefit.InstanceID = p.InstanceID
			}
			w.mu.Unlock()
			if p.MinutesWatched >= benefit.RequiredMinutes {
				slog.Info("watcher benefit complete, claiming", "kind", "claim", "account", w.cfg.AccountID, "benefit", benefit.ID, "benefit_name", benefit.Name, "instance", p.InstanceID, "channel", handle.Channel)
				w.setState(ctx, StateClaiming)
				return nil
			}
		}
	}

	// Vanish-detect (B2.5): if the benefit had progress previously and
	// has now been absent from dropCampaignsInProgress for N consecutive
	// ticks, treat it as externally claimed (code-style drop that left
	// the in-progress list once Twitch issued the redemption code) or
	// campaign-expired. Stop the watch and let pickCampaign find the
	// next eligible target.
	if !matched {
		w.mu.Lock()
		w.noProgressTicks++
		n := w.noProgressTicks
		prevProgress := w.lastProgressMin
		w.mu.Unlock()
		if n >= vanishThreshold && prevProgress > 0 {
			slog.Info("watcher benefit vanished from inventory; treating as externally claimed",
				"kind", "state",
				"account", w.cfg.AccountID,
				"benefit", benefit.ID,
				"benefit_name", benefit.Name,
				"channel", handle.Channel,
				"last_progress_min", prevProgress)
			_ = w.cfg.Backend.StopWatch(ctx, handle)
			w.unsubscribeCurrentChannel()
			w.mu.Lock()
			w.currentBenefit = nil
			w.currentStream = nil
			w.handle = nil
			w.mu.Unlock()
			w.setState(ctx, StatePickCampaign)
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
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return fmt.Errorf("claim: %w", err)
		}
		slog.Error("watcher claim failed", "kind", "error", "account", w.cfg.AccountID, "benefit", benefit.ID, "err", err)
		return fmt.Errorf("claim: %w", err)
	}
	_ = w.cfg.Backend.StopWatch(ctx, handle)
	w.unsubscribeCurrentChannel()

	// P6: post-claim consistency probe. Soft signal — log drift but
	// don't roll back the claim. DropCurrentSession returning the same
	// drop after claim means Twitch hasn't yet cleared the in-progress
	// row; usually catches up within a few seconds.
	if checker, ok := w.cfg.Backend.(platform.CurrentSessionChecker); ok {
		if cs, err := checker.CurrentSession(ctx, w.cfg.Session); err == nil && cs.DropID == benefit.ID {
			slog.Info("watcher post-claim: drop still in current session, server lag expected",
				"kind", "claim",
				"account", w.cfg.AccountID,
				"benefit", benefit.ID,
				"current_min", cs.CurrentMinute,
				"required_min", cs.RequiredMinute)
		}
	}

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
