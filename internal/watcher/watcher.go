package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

// ClaimRecorder writes a claims row after Backend.Claim succeeds so the
// /drops Past tab + /history surface the operator's reward history.
// Implementation lives in store; the seam keeps the watcher
// independent of sqlc-generated types.
type ClaimRecorder interface {
	RecordClaim(ctx context.Context, accountID string, benefit platform.DropBenefit) error
}

type Config struct {
	AccountID    string
	AccountLabel string // human handle (@login) for notifications; falls back to AccountID
	Platform     string // "twitch" | "kick" — for notification context
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

	// ClaimRecorder, when set, persists a claims row each time the
	// backend confirms a claim. Without it the /drops Past tab + the
	// /history view stay empty.
	ClaimRecorder ClaimRecorder

	// ForceLinked, when set and returning true for a campaign id, treats
	// that campaign as account-linked even if the backend reports it
	// unlinked. Backs the manual "I've linked it" override on /drops:
	// Kick connect_url campaigns 403 /drops/progress until the account has
	// already earned (a deadlock), so the API can't pre-confirm the link.
	// The override lets the user assert the link; the watcher then attempts
	// to mine and the live progress check confirms it. Best-effort — if the
	// account truly isn't linked the watch just accrues no progress.
	ForceLinked func(campaignID string) bool
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
	lastPollAt      time.Time // last inventory/progress poll (for the "last poll" UI)
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

	// skippedBenefits collects benefit IDs the watcher has given up
	// on for the lifetime of the Run. Synth scrape entries (no real
	// Twitch UUID, contain "|" or "_default") can land here when no
	// inventory progress ever materialises — usually because the
	// account has ALREADY completed the drop (Minecraft code-only
	// rewards never appear in dropCampaignsInProgress once Twitch
	// issues the code) or never enrolled. pickCampaign filters
	// against this set so we don't loop forever mining a ghost.
	skippedBenefits map[string]struct{}

	// noStreamCampaigns collects campaign IDs whose eligible channels
	// were all offline this round. pickStream populates it instead of
	// sleeping, then re-enters pickCampaign so the watcher advances to
	// the NEXT eligible campaign that DOES have a live broadcaster —
	// without this, a high-priority esports campaign (riotgames, lck,
	// etc., rarely live) traps the watcher in pick→sleep→repick forever
	// while lower-priority campaigns with live streams never get mined.
	// Cleared once every campaign is exhausted (true idle) so the next
	// wake retries the full set fresh.
	noStreamCampaigns map[string]struct{}

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
			OnRewardCode:   w.handlePubSubRewardCode,
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

// handlePubSubRewardCode fires when an onsite-notification carries a
// Mojang/Twitch redemption code. We don't know which benefit it maps
// to (the notification only carries body text), so we log it for the
// operator and forward to the ClaimRecorder with the current benefit
// when one is in flight. Without a current benefit the code is still
// logged so the user can recover it from logs even if state is lost.
func (w *Watcher) handlePubSubRewardCode(notificationID, code, body string) {
	w.mu.Lock()
	var benefit platform.DropBenefit
	if w.currentBenefit != nil {
		benefit = *w.currentBenefit
	}
	w.mu.Unlock()
	slog.Info("watcher reward code captured",
		"kind", "claim",
		"account", w.cfg.AccountID,
		"notification_id", notificationID,
		"code", code,
		"benefit_id", benefit.ID,
		"benefit_name", benefit.Name)
	if w.cfg.ClaimRecorder == nil || benefit.ID == "" {
		return
	}
	// Repurpose the existing claim recorder so the code lands in the
	// claims table's value_meta_json. The /drops + /history surfaces
	// already read from claims, so this gives operators the code
	// without a schema migration.
	recorderWithCode, ok := w.cfg.ClaimRecorder.(interface {
		RecordClaimWithCode(ctx context.Context, accountID string, benefit platform.DropBenefit, code string) error
	})
	if !ok {
		return
	}
	if err := recorderWithCode.RecordClaimWithCode(context.Background(), w.cfg.AccountID, benefit, code); err != nil {
		slog.Warn("watcher record claim+code failed",
			"kind", "error",
			"account", w.cfg.AccountID,
			"benefit", benefit.ID,
			"err", err)
	}
}

// isSynthBenefitID returns true when the benefit ID was fabricated by
// the scrape-fallback merge instead of being a real Twitch UUID.
// Synth IDs encode the campaign + game directly (contain "|") and
// usually have a "_default" suffix. The watcher uses this hint to
// decide whether to give up on a stuck benefit (real UUIDs that
// don't appear in inventory are kept around longer since the
// inventory poll itself may be flaky).
func isSynthBenefitID(id string) bool {
	if id == "" {
		return false
	}
	return strings.Contains(id, "|") || strings.Contains(id, " ") || strings.HasSuffix(id, "_default")
}

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

// firstUnmetPrecondition returns the id of the first precondition drop
// that has not yet been claimed, or "" when all preconditions are met
// (including the empty case). Mirrors DevilXD's TimedDrop precondition
// check: a drop only becomes minable once every drop it depends on is
// claimed.
func firstUnmetPrecondition(preconditions []string, claimed map[string]bool) string {
	for _, id := range preconditions {
		if !claimed[id] {
			return id
		}
	}
	return ""
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
	BenefitImage    string
	RequiredMinutes int
	MinutesWatched  int
	Channel         string
	ViewerCount     int
	StartedAt       time.Time
	LastPollAt      time.Time
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
		s.BenefitImage = w.currentBenefit.ImageURL
		s.RequiredMinutes = w.currentBenefit.RequiredMinutes
	}
	if w.currentStream != nil {
		s.Channel = w.currentStream.Channel
		s.ViewerCount = w.currentStream.ViewerCount
	}
	s.MinutesWatched = w.lastProgressMin
	s.StartedAt = w.watchStartedAt
	s.LastPollAt = w.lastPollAt
	return s
}

// notifyFields builds the field map for a claim/progress notification.
// "account" stays the account ID (the router keys per-account webhooks on
// it); human-facing values (account_label, game, drop, channel, image) are
// added so the Discord embed can render names instead of raw IDs. extra
// overrides/augments (e.g. progress counters).
func (w *Watcher) notifyFields(extra map[string]any) map[string]any {
	f := map[string]any{"account": w.cfg.AccountID}
	if w.cfg.AccountLabel != "" {
		f["account_label"] = w.cfg.AccountLabel
	}
	if w.cfg.Platform != "" {
		f["platform"] = w.cfg.Platform
	}
	w.mu.Lock()
	if w.currentCampaign != nil {
		if w.currentCampaign.Game != "" {
			f["game"] = w.currentCampaign.Game
		}
		if w.currentCampaign.Name != "" {
			f["campaign"] = w.currentCampaign.Name
		}
	}
	if w.currentBenefit != nil {
		if w.currentBenefit.Name != "" {
			f["drop"] = w.currentBenefit.Name
		}
		if w.currentBenefit.ImageURL != "" {
			f["image"] = w.currentBenefit.ImageURL
		}
	}
	if w.currentStream != nil && w.currentStream.Channel != "" {
		f["channel"] = w.currentStream.Channel
	}
	w.mu.Unlock()
	for k, v := range extra {
		f[k] = v
	}
	return f
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
		} else if errors.Is(err, errIdle) {
			// Idle (sleeping / awaiting connect): wait the recheck
			// cooldown, then re-discover. Cancellable so Reload/shutdown
			// still tears the watcher down promptly.
			backoff = 0
			w.setState(ctx, StatePickCampaign)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(recheckInterval):
			}
			continue
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

// errIdle marks a transient "nothing to do right now" — the watcher
// sleeps recheckInterval and re-discovers, rather than exiting. Lets a
// sleeping/awaiting-connect account pick up a newly-active campaign or a
// freshly-linked account without a manual scheduler Reload.
var errIdle = errors.New("idle; recheck later")

// recheckInterval is how long a sleeping/awaiting-connect watcher waits
// before re-discovering. Matches the discovery scraper cadence (~5m) so
// the watcher's view and the persisted /drops view converge.
const recheckInterval = 5 * time.Minute

// Watch-loop network cadences. The state machine ticks every 500ms for
// responsiveness, but the expensive gql calls are throttled to roughly
// match DevilXD/TwitchDropsMiner so we don't flood gql.twitch.tv (which
// is both rate-limited and bot-detected). Counted in 500ms ticks.
const (
	beaconEveryTicks    = 120 // ~60s — minute-watched beacon (DevilXD WATCH_INTERVAL)
	liveCheckEveryTicks = 600 // ~5m — backstop re-probe; PubSub stream-down is the primary signal
	inventoryEveryTicks = 120 // ~60s — poll drop progress (backstop; PubSub user-drop-events pushes progress in real time)
)

// vanishThreshold is how many consecutive INVENTORY POLLS must report
// "no progress row for current benefit" before the watcher concludes
// the benefit was externally claimed or expired. 3 polls at the ~20s
// inventory cadence ≈ 60 seconds of grace.
const vanishThreshold = 3

// synthSkipThreshold caps how many inventory polls we'll babysit a
// synth-scrape benefit (no real Twitch UUID) that NEVER shows up in
// dropCampaignsInProgress before skipping it permanently. 6 polls at
// ~20s ≈ 2 minutes — enough for a real enrollment to register, short
// enough that a code-only drop the user already finished doesn't pin
// the watcher forever.
const synthSkipThreshold = 6

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
	case StateSleeping, StateAwaitingConnect:
		// Not terminal: re-discover after a cool-down. A sleeping account
		// must keep checking — an upcoming campaign may go active, or the
		// user may connect a previously-unlinked account (awaiting_connect)
		// — without needing a manual scheduler Reload. errIdle tells Run to
		// wait recheckInterval, then drop back to pickCampaign.
		return errIdle
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
		// Integrity wall regression (C1): the sidecar's retry path
		// kept seeing "failed integrity check" from gql. Transition
		// to StateAuthRequired so the dashboard surfaces a re-auth
		// banner, and return errComplete so Run exits cleanly — the
		// next Reload re-spins the watcher once cookies are refreshed.
		if errors.Is(err, platform.ErrIntegrityBlocked) {
			slog.Warn("watcher: integrity blocked, marking account needs_auth",
				"kind", "auth", "account", w.cfg.AccountID)
			w.setState(ctx, StateAuthRequired)
			return errComplete
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

	// Reconcile inventory ownership into the claims table: a drop the
	// account already owns (claimed[]) but we have no claims row for — e.g.
	// claimed MANUALLY on Twitch outside the bot — gets recorded so the
	// /drops COLLECTED mark reflects it. Idempotent (RecordClaimIfNew skips
	// existing rows). Benefits were just persisted above, satisfying the
	// claims.benefit_id FK.
	if rec, ok := w.cfg.ClaimRecorder.(interface {
		RecordClaimIfNew(context.Context, string, platform.DropBenefit) (bool, error)
	}); ok && len(claimed) > 0 {
		for _, c := range campaigns {
			for _, b := range c.Benefits {
				if !claimed[b.ID] && !(b.RewardID != "" && claimed[b.RewardID]) {
					continue
				}
				if wrote, err := rec.RecordClaimIfNew(ctx, w.cfg.AccountID, b); err == nil && wrote {
					slog.Info("watcher reconciled owned drop into claims",
						"kind", "claim", "account", w.cfg.AccountID,
						"benefit", b.ID, "benefit_name", b.Name)
				}
			}
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
		// Skip campaigns the account can't earn because the required
		// external account isn't linked. Twitch always populates
		// AccountLinked (isAccountConnected). Kick now sets
		// AccountLinkChecked for connect_url campaigns (linked = the
		// account is participating). Only skip when the link status was
		// actually checked and came back false — never skip on an
		// unverified default (avoids regressing platforms/paths that
		// don't surface the flag).
		if (c.Platform == "twitch" || c.AccountLinkChecked) && !c.AccountLinked {
			// Manual "I've linked it" override: the user asserted the
			// external account is connected, so attempt to mine despite the
			// backend reporting unlinked. The live progress check confirms.
			if w.cfg.ForceLinked != nil && w.cfg.ForceLinked(c.ID) {
				slog.Info("watcher mining link-overridden campaign", "kind", "discovery", "account", w.cfg.AccountID, "campaign", c.Name)
			} else {
				skippedUnlinked++
				continue
			}
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
		// Skip campaigns whose channels were all offline earlier this
		// round (set by pickStream). Lets the loop fall through to the
		// next eligible campaign with a live broadcaster instead of
		// re-picking the same dead one every cycle.
		w.mu.Lock()
		_, noStream := w.noStreamCampaigns[c.ID]
		w.mu.Unlock()
		if noStream {
			continue
		}
		for _, b := range c.Benefits {
			// claimed[] is keyed by drop id (in-progress isClaimed) AND by
			// benefit id (gameEventDrops owned). Check both so a drop whose
			// reward the account already holds is skipped immediately,
			// instead of being re-picked and watched until the slow
			// no-progress fallback gives up.
			if claimed[b.ID] || (b.RewardID != "" && claimed[b.RewardID]) {
				continue
			}
			// Per-watcher skip set: synth benefits we already burnt
			// time on without seeing any inventory progress.
			w.mu.Lock()
			_, skip := w.skippedBenefits[b.ID]
			w.mu.Unlock()
			if skip {
				continue
			}
			// Precondition gate (DevilXD parity): a drop in a chain can't
			// accrue watch-time until its precondition drops are claimed.
			// Skip it so the earlier drop gets mined first; once that's
			// claimed this one becomes pickable on a later cycle. Empty
			// preconditions (the common case) never blocks.
			if unmet := firstUnmetPrecondition(b.Preconditions, claimed); unmet != "" {
				slog.Info("watcher skipping drop with unmet precondition",
					"kind", "discovery", "account", w.cfg.AccountID,
					"benefit", b.ID, "needs_claimed", unmet)
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
	// Reaching here means every eligible benefit was claimed, skipped,
	// or its campaign had no live stream this round. Clear the
	// no-stream set so the next wake re-probes all campaigns (a dead
	// esports channel may have come online by then).
	w.mu.Lock()
	exhaustedNoStream := len(w.noStreamCampaigns) > 0
	w.noStreamCampaigns = nil
	w.mu.Unlock()
	// Awaiting-connect takes priority over plain sleeping: if the only
	// reason we have nothing to mine is that every whitelisted+active
	// campaign is gated behind an unlinked external account, surface that
	// distinctly so the dashboard can prompt the user to connect rather
	// than implying the account is simply idle with no work.
	if len(matched) == 0 && skippedUnlinked > 0 {
		slog.Info("watcher awaiting account connect", "kind", "discovery", "account", w.cfg.AccountID, "unlinked_campaigns", skippedUnlinked)
		w.setState(ctx, StateAwaitingConnect)
		return nil
	}
	if w.cfg.AllowGame != nil && len(matched) == 0 && len(campaigns) > 0 {
		slog.Info("watcher: no whitelisted games match active campaigns, sleeping", "account", w.cfg.AccountID, "active_campaigns", len(campaigns))
	} else {
		slog.Info("watcher has no eligible benefit, sleeping", "account", w.cfg.AccountID, "scanned_campaigns", len(matched), "all_streams_offline", exhaustedNoStream)
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
		// No live broadcaster for THIS campaign right now. Mark it so
		// pickCampaign skips it and advances to the next eligible
		// campaign with a live stream — instead of sleeping and
		// re-picking this same dead campaign forever (esports channels
		// are offline most of the time). pickCampaign clears the set
		// once every campaign is exhausted, so we retry on the next wake.
		slog.Info("watcher no eligible streams live, trying next campaign", "kind", "discovery", "account", w.cfg.AccountID, "campaign", camp.Name)
		w.mu.Lock()
		if w.noStreamCampaigns == nil {
			w.noStreamCampaigns = map[string]struct{}{}
		}
		w.noStreamCampaigns[camp.ID] = struct{}{}
		w.mu.Unlock()
		w.setState(ctx, StatePickCampaign)
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
		// Cap the per-candidate AvailableDropIDs probe: each is a gql
		// call, and a directory of 30 streams all serving *other* drops
		// would otherwise fan out 30 sequential requests on every pick
		// (and pick re-fires on every stream-down). The head candidates
		// are highest-viewer/most-likely, so a small cap is plenty.
		const maxProbe = 5
		probed := 0
		for _, cand := range streams {
			if cand.ChannelID == "" {
				continue
			}
			if probed >= maxProbe {
				break
			}
			probed++
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
	// Reset the tick counter so the beacon/inventory cadence (tickN==1
	// fires immediately) aligns with the start of each watch session.
	w.tickCount = 0
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

	// Minute-watched beacon. Twitch credits watch time off this payload
	// (minutes_logged:1). DevilXD sends it ~once per 60s; sending it
	// every tick both wastes requests and looks like a bot. Fire on the
	// first watch tick, then every beaconEveryTicks. The heartbeat error
	// is also our cheapest stream-down signal, so we keep failing the
	// tick when it errors.
	if tickN == 1 || tickN%beaconEveryTicks == 0 {
		if err := w.cfg.Backend.Heartbeat(ctx, handle); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
			slog.Error("watcher heartbeat failed", "kind", "error", "account", w.cfg.AccountID, "channel", handle.Channel, "err", err)
			return fmt.Errorf("heartbeat: %w", err)
		}
		// kind=heartbeat feeds the dashboard HEARTBEATS/HR card via the
		// log ring (counted over the last hour).
		slog.Info("watcher heartbeat sent", "kind", "heartbeat", "account", w.cfg.AccountID, "channel", handle.Channel)
	}

	// Periodically re-check the channel we're watching is still live +
	// eligible (~30s). SendEvents is fire-and-forget — minutes stop
	// accruing after a stream ends but the mutation keeps returning 200,
	// so this active probe (plus PubSub video-playback) tells us when to
	// swap channels.
	if tickN%liveCheckEveryTicks == 0 {
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

	// Drop-progress poll (~20s). Only the inventory ticks below touch
	// gql; intermediate ticks just maintain the beacon/live-check above
	// and return, keeping us off Twitch's rate limiter.
	if tickN != 1 && tickN%inventoryEveryTicks != 0 {
		return nil
	}

	w.mu.Lock()
	w.lastPollAt = time.Now()
	w.mu.Unlock()
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
			if w.skippedBenefits == nil {
				w.skippedBenefits = map[string]struct{}{}
			}
			w.skippedBenefits[benefit.ID] = struct{}{}
			w.currentBenefit = nil
			w.currentStream = nil
			w.handle = nil
			w.mu.Unlock()
			w.setState(ctx, StatePickCampaign)
			return nil
		}
		// A benefit that NEVER appears in dropCampaignsInProgress after
		// the grace window is unminable here: either a synth scrape ghost
		// (no real UUID), or a REAL drop already fully claimed — claimed
		// drops drop OUT of dropCampaignsInProgress entirely (they move to
		// gameEventDrops), so claimed[] can't see them and the watcher
		// would otherwise re-pick the same done drop forever and never
		// advance to the next campaign. Skip either case after the grace
		// window. (Real drops normally enroll within a poll or two, so a
		// genuinely-mining drop resets noProgressTicks long before this.)
		if n >= synthSkipThreshold && prevProgress == 0 {
			slog.Warn("watcher: benefit never appeared in inventory (claimed or ghost); skipping",
				"kind", "state",
				"account", w.cfg.AccountID,
				"benefit", benefit.ID,
				"benefit_name", benefit.Name,
				"channel", handle.Channel,
				"synth", isSynthBenefitID(benefit.ID),
				"ticks_without_progress", n)
			_ = w.cfg.Backend.StopWatch(ctx, handle)
			w.unsubscribeCurrentChannel()
			w.mu.Lock()
			if w.skippedBenefits == nil {
				w.skippedBenefits = map[string]struct{}{}
			}
			w.skippedBenefits[benefit.ID] = struct{}{}
			w.currentBenefit = nil
			w.currentStream = nil
			w.handle = nil
			w.mu.Unlock()
			w.setState(ctx, StatePickCampaign)
			return nil
		}
	}

	_ = w.cfg.Notifier.Notify(ctx, "progress", w.notifyFields(nil))
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

	// Persist claim → claims table so /drops Past + /history surface
	// it. Logged warn on failure; we don't want a transient DB hiccup
	// to make us re-claim the same drop on the next tick.
	if w.cfg.ClaimRecorder != nil {
		if err := w.cfg.ClaimRecorder.RecordClaim(ctx, w.cfg.AccountID, benefit); err != nil {
			slog.Warn("watcher record claim failed", "kind", "error", "account", w.cfg.AccountID, "benefit", benefit.ID, "err", err)
		}
	}

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

	_ = w.cfg.Notifier.Notify(ctx, "claim", w.notifyFields(map[string]any{
		// benefit/handle are captured locals; currentStream may already be
		// cleared by claim time, so pass channel explicitly.
		"drop": benefit.Name, "channel": handle.Channel,
	}))

	w.mu.Lock()
	w.currentBenefit = nil
	w.currentStream = nil
	w.handle = nil
	w.mu.Unlock()

	w.setState(ctx, StateIdle)
	return nil
}
