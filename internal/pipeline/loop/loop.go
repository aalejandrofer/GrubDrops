// Package loop is the per-account pipeline v2 runner. One goroutine owns
// every drop state transition: it reconciles, plans, runs one session at a
// time, and claims.
package loop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/claimer"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/planner"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/reconcile"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/session"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

type Store interface {
	List(ctx context.Context, accountID string) ([]dropstate.Row, error)
	Upsert(ctx context.Context, r dropstate.Row) error
}

type ClaimHistory interface {
	RecordClaimIfNew(ctx context.Context, accountID string, b platform.DropBenefit) (bool, error)
	ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
}

type Config struct {
	AccountID    string
	AccountLabel string
	Platform     string
	Backend      platform.Backend
	Session      platform.Session
	Store        Store
	History      ClaimHistory
	Persister    reconcile.CampaignPersister
	Notifier     Notifier

	AllowGame func(game string) bool
	// Games is the whitelisted game display names, copied into
	// Session.Games when the session carries none. TV-client Twitch
	// sessions walk one game directory per name (chandisc.go); Android
	// sessions ignore it. Mirrors watcher.Config.Games.
	Games                 []string
	AllowChannel          func(channels []string) bool
	GameRank              func(game string) int
	PriorityMode          string
	ForceLinked           func(campaignID string) bool
	StreamerPriority      []string
	ForceWatch            []string
	ProgressNotifyStepPct int
	// StallClaimProbe (Twitch): on a stall, send one claim for each served
	// Eligible drop that was absent from the last inventory read. Twitch
	// drops claimed outside the app whose campaign left the Inventory are
	// otherwise invisible (DropCampaignDetails self: null) and would be
	// watched forever. Kick keeps claimed rewards listed, so it leaves this off.
	StallClaimProbe bool

	Now            func() time.Time
	ReconcileEvery time.Duration // default 15m
	LiveEvery      time.Duration // default 5m
	BeatEvery      time.Duration // default 60s; production never overrides (Twitch credit gate)
	StallPolls     int           // default 5
	StallCooldown  time.Duration // default 30m
	DownCooldown   time.Duration // default 2m
}

// maxLiveCampaigns bounds channel lookups per refresh.
const maxLiveCampaigns = 8

type pubsubEvent struct {
	kind     string // "progress" | "claimable" | "down"
	drop     string
	instance string
	minutes  int
	required int
	channel  string
}

type Loop struct {
	cfg     Config
	prog    platform.DropProgressSource
	claimer platform.DropClaimer
	prober  platform.ChannelProber
	subs    platform.ChannelSubscriber

	nudge      chan struct{}
	pubsub     chan pubsubEvent
	sessEvents chan session.Event

	mu         sync.Mutex
	snap       watcher.Snapshot
	discovery  []platform.Campaign
	discoverAt time.Time

	// Owned by the Run goroutine.
	rows           map[string]dropstate.Row
	camps          []platform.Campaign
	progress       map[string]platform.DropProgress
	live           map[string][]platform.Stream
	forceLive      []platform.Stream
	cooldowns      map[string]time.Time
	current        *planner.Decision
	lastSwap       time.Time
	sessCancel     context.CancelFunc
	sessDone       chan struct{} // closed by the session goroutine once session.Run returns
	subscribed     string
	authBlocked    bool // integrity wall hit: Run exits, the next reload re-spins the loop
	milestones     map[string]int
	gen            int             // bumped on every startSession; tags session events
	pendingHistory map[string]bool // drop ids whose RecordClaimIfNew failed, retried each iteration
	// lastInv is the set of drop ids in the current session's most recent
	// Progress event (the full inventory read). Reset on every session start.
	lastInv map[string]bool
	// stallCount counts current-channel stalls per probe-qualifying drop
	// (Eligible, absent from lastInv). The probe fires on the second stall;
	// an entry is dropped once probed or once the row leaves Eligible.
	stallCount map[string]int
}

// New assembles the loop, checking the backend's pipeline capabilities once.
func New(cfg Config) (*Loop, error) {
	prog, ok := cfg.Backend.(platform.DropProgressSource)
	if !ok {
		return nil, fmt.Errorf("backend %T has no DropProgress: pipeline v2 unsupported", cfg.Backend)
	}
	cl, ok := cfg.Backend.(platform.DropClaimer)
	if !ok {
		return nil, fmt.Errorf("backend %T has no ClaimDrop: pipeline v2 unsupported", cfg.Backend)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ReconcileEvery <= 0 {
		cfg.ReconcileEvery = 15 * time.Minute
	}
	if cfg.LiveEvery <= 0 {
		cfg.LiveEvery = 5 * time.Minute
	}
	if cfg.BeatEvery <= 0 {
		cfg.BeatEvery = 60 * time.Second
	}
	if cfg.StallPolls <= 0 {
		cfg.StallPolls = 5
	}
	if cfg.StallCooldown <= 0 {
		cfg.StallCooldown = 30 * time.Minute
	}
	if cfg.DownCooldown <= 0 {
		cfg.DownCooldown = 2 * time.Minute
	}
	cfg.Session.AccountID = cfg.AccountID
	if cfg.Session.Games == nil {
		cfg.Session.Games = cfg.Games
	}
	if cfg.AllowGame != nil {
		cfg.Session.GameFilter = cfg.AllowGame
	}
	l := &Loop{
		cfg: cfg, prog: prog, claimer: cl,
		nudge:          make(chan struct{}, 1),
		pubsub:         make(chan pubsubEvent, 64),
		sessEvents:     make(chan session.Event, 64),
		rows:           map[string]dropstate.Row{},
		progress:       map[string]platform.DropProgress{},
		live:           map[string][]platform.Stream{},
		cooldowns:      map[string]time.Time{},
		milestones:     map[string]int{},
		pendingHistory: map[string]bool{},
		stallCount:     map[string]int{},
	}
	l.prober, _ = cfg.Backend.(platform.ChannelProber)
	l.subs, _ = cfg.Backend.(platform.ChannelSubscriber)
	l.snap = watcher.Snapshot{AccountID: cfg.AccountID, State: "pick_campaign"}
	if ps, ok := cfg.Backend.(platform.PubSubAware); ok {
		ps.SetAccountPubSubHooks(cfg.AccountID, l.hooks())
	}
	return l, nil
}

func (l *Loop) hooks() platform.PubSubHooks {
	push := func(e pubsubEvent) {
		select {
		case l.pubsub <- e:
		default: // never block the PubSub reader
		}
	}
	return platform.PubSubHooks{
		OnDropProgress: func(dropID string, cur, req int64) {
			push(pubsubEvent{kind: "progress", drop: dropID, minutes: int(cur), required: int(req)})
		},
		OnDropClaim: func(dropID, instanceID string) {
			push(pubsubEvent{kind: "claimable", drop: dropID, instance: instanceID})
		},
		OnStreamDown: func(channelID string) {
			push(pubsubEvent{kind: "down", channel: channelID})
		},
	}
}

// Nudge asks the loop to reconcile and refresh channels now.
func (l *Loop) Nudge() {
	select {
	case l.nudge <- struct{}{}:
	default:
	}
}

func (l *Loop) Snapshot() watcher.Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snap
}

func (l *Loop) LastDiscovery() ([]platform.Campaign, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]platform.Campaign(nil), l.discovery...), l.discoverAt
}

func (l *Loop) AllowGame() func(game string) bool { return l.cfg.AllowGame }

func (l *Loop) Run(ctx context.Context) error {
	if err := l.load(ctx); err != nil {
		return err
	}
	defer l.stopSession()
	l.reconcile(ctx)
	if l.authBlocked {
		return nil
	}
	l.refreshLive(ctx)
	l.claimReady(ctx)
	if l.authBlocked {
		return nil
	}
	l.retryPendingHistory(ctx)
	l.replan(ctx)
	recT := time.NewTicker(l.cfg.ReconcileEvery)
	liveT := time.NewTicker(l.cfg.LiveEvery)
	defer recT.Stop()
	defer liveT.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-recT.C:
			l.reconcile(ctx)
		case <-liveT.C:
			l.refreshLive(ctx)
		case <-l.nudge:
			l.reconcile(ctx)
			l.refreshLive(ctx)
		case ev := <-l.sessEvents:
			l.onSession(ctx, ev)
		case pe := <-l.pubsub:
			l.onPubSub(ctx, pe)
		}
		if l.authBlocked {
			return nil
		}
		l.claimReady(ctx)
		if l.authBlocked {
			return nil
		}
		l.retryPendingHistory(ctx)
		l.replan(ctx)
	}
}

// load reads stored state, then bridges claim rows written by the v1-era
// mark-collected UI (that handler reloads the scheduler, so this runs).
func (l *Loop) load(ctx context.Context) error {
	rows, err := l.cfg.Store.List(ctx, l.cfg.AccountID)
	if err != nil {
		return fmt.Errorf("load drop state: %w", err)
	}
	for _, r := range rows {
		l.rows[r.DropID] = r
	}
	if l.cfg.History == nil {
		return nil
	}
	ids, err := l.cfg.History.ClaimedBenefitIDs(ctx, l.cfg.AccountID)
	if err != nil {
		slog.Warn("pipeline: read claim history failed", "kind", "error", "account", l.cfg.AccountID, "err", err)
		return nil
	}
	now := l.cfg.Now()
	for id := range ids {
		if r, ok := l.rows[id]; ok && r.Status != dropstate.Claimed {
			next := dropstate.MarkCollected(r, now)
			l.rows[id] = next
			if err := l.cfg.Store.Upsert(ctx, next); err != nil {
				slog.Warn("pipeline: persist bridged mark failed", "kind", "error", "account", l.cfg.AccountID, "drop", id, "err", err)
			}
		}
	}
	return nil
}

func (l *Loop) reconcile(ctx context.Context) {
	res, err := reconcile.Run(ctx, reconcile.Config{
		AccountID: l.cfg.AccountID, Platform: l.cfg.Platform,
		Backend: l.cfg.Backend, Progress: l.prog, Session: l.cfg.Session,
		AllowGame: l.cfg.AllowGame, AllowChannel: l.cfg.AllowChannel,
		ForceLinked: l.cfg.ForceLinked, Persister: l.cfg.Persister,
	}, l.rows, l.cfg.Now())
	if err != nil {
		if errors.Is(err, platform.ErrIntegrityBlocked) {
			l.onIntegrityBlocked(ctx)
			return
		}
		slog.Warn("pipeline reconcile failed; keeping last state", "kind", "error", "account", l.cfg.AccountID, "err", err)
		return
	}
	l.camps = res.Campaigns
	for id, dp := range res.Progress {
		l.progress[id] = dp
	}
	l.mu.Lock()
	l.discovery, l.discoverAt = res.Campaigns, l.cfg.Now()
	l.mu.Unlock()
	for _, r := range res.Rows {
		l.commit(ctx, r)
	}
}

// onIntegrityBlocked mirrors v1: stop watching, surface auth_required so the
// dashboard shows a re-auth banner, and let Run exit cleanly. The next reload
// re-spins the loop once the session is refreshed.
func (l *Loop) onIntegrityBlocked(ctx context.Context) {
	slog.Warn("pipeline: integrity blocked, marking account needs_auth", "kind", "auth", "account", l.cfg.AccountID)
	l.authBlocked = true
	l.stopSession()
	l.current = nil
	l.mu.Lock()
	l.snap = watcher.Snapshot{AccountID: l.cfg.AccountID, State: "auth_required"}
	l.mu.Unlock()
	if l.cfg.Notifier != nil {
		_ = l.cfg.Notifier.Notify(ctx, "state", map[string]any{"account": l.cfg.AccountID, "state": "auth_required"})
	}
}

func (l *Loop) benefit(dropID string) (platform.DropBenefit, platform.Campaign) {
	for _, c := range l.camps {
		for _, b := range c.Benefits {
			if b.ID == dropID {
				return b, c
			}
		}
	}
	return platform.DropBenefit{ID: dropID}, platform.Campaign{}
}

// commit stores a row and handles the transition into claimed.
func (l *Loop) commit(ctx context.Context, next dropstate.Row) {
	prev := l.rows[next.DropID]
	l.rows[next.DropID] = next
	if next.Status != dropstate.Eligible {
		delete(l.stallCount, next.DropID)
	}
	if err := l.cfg.Store.Upsert(ctx, next); err != nil {
		slog.Warn("pipeline: persist drop state failed", "kind", "error", "account", l.cfg.AccountID, "drop", next.DropID, "err", err)
	}
	if next.Status != dropstate.Claimed || prev.Status == dropstate.Claimed {
		return
	}
	b, c := l.benefit(next.DropID)
	if l.cfg.History != nil {
		if _, err := l.cfg.History.RecordClaimIfNew(ctx, l.cfg.AccountID, b); err != nil {
			slog.Warn("pipeline: record claim history failed; will retry", "kind", "error", "account", l.cfg.AccountID, "drop", next.DropID, "err", err)
			l.pendingHistory[next.DropID] = true
		}
	}
	slog.Info("pipeline drop claimed", "kind", "claim", "account", l.cfg.AccountID, "drop", next.DropID, "source", string(next.Source))
	// Only notify live transitions, not historical claims seen on first sync.
	if !prev.IsZero() && next.Source == dropstate.FromPlatform {
		l.notify(ctx, "claim", c, b, nil)
	}
}

// retryPendingHistory retries any RecordClaimIfNew calls that failed on a
// prior commit, so a transient history-store outage doesn't permanently
// drop the record (spec 4.2/7). Runs after every loop iteration.
func (l *Loop) retryPendingHistory(ctx context.Context) {
	if l.cfg.History == nil || len(l.pendingHistory) == 0 {
		return
	}
	for id := range l.pendingHistory {
		b, _ := l.benefit(id)
		if _, err := l.cfg.History.RecordClaimIfNew(ctx, l.cfg.AccountID, b); err != nil {
			slog.Warn("pipeline: retry claim history failed", "kind", "error", "account", l.cfg.AccountID, "drop", id, "err", err)
			continue
		}
		delete(l.pendingHistory, id)
	}
}

func (l *Loop) notify(ctx context.Context, event string, c platform.Campaign, b platform.DropBenefit, extra map[string]any) {
	if l.cfg.Notifier == nil {
		return
	}
	f := map[string]any{"account": l.cfg.AccountID}
	if l.cfg.AccountLabel != "" {
		f["account_label"] = l.cfg.AccountLabel
	}
	if l.cfg.Platform != "" {
		f["platform"] = l.cfg.Platform
	}
	if c.Game != "" {
		f["game"] = c.Game
	}
	if c.Name != "" {
		f["campaign"] = c.Name
	}
	if b.Name != "" {
		f["drop"] = b.Name
	}
	for k, v := range extra {
		f[k] = v
	}
	_ = l.cfg.Notifier.Notify(ctx, event, f)
}

func (l *Loop) plannerInput() planner.Input {
	return planner.Input{
		Now: l.cfg.Now(), Rows: l.rows, Campaigns: l.camps,
		Settings: planner.Settings{
			GameRank: l.cfg.GameRank, PriorityMode: l.cfg.PriorityMode,
			StreamerPriority: l.cfg.StreamerPriority, ForceWatch: l.forceLive,
		},
		Live: l.live, Cooldowns: l.cooldowns, Current: l.current, LastSwap: l.lastSwap,
	}
}

// allowedPriority returns the priority logins a campaign accepts.
func allowedPriority(c platform.Campaign, prio []string) []string {
	if len(c.AllowedChannels) == 0 {
		return prio
	}
	allowed := map[string]bool{}
	for _, a := range c.AllowedChannels {
		allowed[strings.ToLower(a)] = true
	}
	var out []string
	for _, p := range prio {
		if allowed[strings.ToLower(p)] {
			out = append(out, p)
		}
	}
	return out
}

func mergeStreams(first, rest []platform.Stream) []platform.Stream {
	seen := map[string]bool{}
	var out []platform.Stream
	for _, s := range append(append([]platform.Stream(nil), first...), rest...) {
		k := strings.ToLower(s.Channel)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

func (l *Loop) refreshLive(ctx context.Context) {
	live := map[string][]platform.Stream{}
	for i, c := range planner.Candidates(l.plannerInput()) {
		if i >= maxLiveCampaigns {
			break
		}
		streams, err := l.cfg.Backend.ListEligibleChannels(ctx, l.cfg.Session, c)
		if err != nil {
			slog.Debug("pipeline: list channels failed", "account", l.cfg.AccountID, "campaign", c.ID, "err", err)
		}
		// A restricted campaign whose allow-list the loop can't see (Twitch
		// reports only a count) would credit none of the probed priority
		// streamers it doesn't list, so skip probing it.
		restrictedUnknown := c.AllowedChannelCount > 0 && len(c.AllowedChannels) == 0
		if l.prober != nil && !restrictedUnknown {
			if logins := allowedPriority(c, l.cfg.StreamerPriority); len(logins) > 0 {
				if ps, err := l.prober.ProbeChannels(ctx, l.cfg.Session, c, logins); err == nil {
					streams = mergeStreams(ps, streams)
				}
			}
		}
		live[c.ID] = streams
	}
	l.live = live
	l.forceLive = nil
	if l.prober != nil && len(l.cfg.ForceWatch) > 0 {
		if ps, err := l.prober.ProbeChannels(ctx, l.cfg.Session, platform.Campaign{}, l.cfg.ForceWatch); err == nil {
			l.forceLive = ps
		}
	}
}

func (l *Loop) isCurrent(channel string) bool {
	return l.current != nil && l.current.Kind != planner.Idle && strings.EqualFold(l.current.Channel.Channel, channel)
}

func (l *Loop) onSession(ctx context.Context, ev session.Event) {
	if ev.Gen != l.gen {
		return // stale event from a session already stopped/restarted
	}
	now := l.cfg.Now()
	switch ev.Kind {
	case session.Progress:
		inv := make(map[string]bool, len(ev.Progress))
		for _, p := range ev.Progress {
			inv[p.BenefitID] = true
		}
		l.lastInv = inv
		l.applyProgress(ctx, ev.Progress)
	case session.StreamDown:
		if l.isCurrent(ev.Channel) {
			l.cooldowns[strings.ToLower(ev.Channel)] = now.Add(l.cfg.DownCooldown)
		}
	case session.Stalled:
		if l.isCurrent(ev.Channel) {
			if l.cfg.StallClaimProbe {
				l.stallClaimProbe(ctx, now)
			}
			l.cooldowns[strings.ToLower(ev.Channel)] = now.Add(l.cfg.StallCooldown)
			l.reconcile(ctx)
		}
	}
}

// stallClaimProbe sends one claim for a served Eligible drop the last
// inventory read did not list, on that drop's second stall (the first is a
// plain cooldown). On Twitch such a drop is usually one claimed outside the
// app whose campaign left the Inventory: nothing reports it, so the claim
// answer is the only signal. OK/ALREADY_CLAIMED marks it claimed; a failure
// blocks it not_enrolled (1h re-check) without counting a claim failure,
// since it was never a completed drop.
func (l *Loop) stallClaimProbe(ctx context.Context, now time.Time) {
	if l.current == nil {
		return
	}
	for _, id := range l.current.Serves {
		r, ok := l.rows[id]
		if !ok || r.Status != dropstate.Eligible || l.lastInv[id] {
			continue
		}
		l.stallCount[id]++
		if l.stallCount[id] < 2 {
			continue
		}
		delete(l.stallCount, id)
		dp, ok := l.progress[id]
		if !ok || dp.DropID == "" {
			dp = platform.DropProgress{DropID: id, CampaignID: r.CampaignID}
		}
		res := l.claimer.ClaimDrop(ctx, l.cfg.Session, dp)
		var next dropstate.Row
		kind := "probe"
		switch res.Outcome {
		case platform.ClaimOK, platform.ClaimAlready:
			next, kind = dropstate.ClaimOK(r, now), "claim"
		case platform.ClaimNeedsLink:
			next = dropstate.ClaimNeedsLink(r, now)
		default:
			next = dropstate.ProbeNotEnrolled(r, now)
		}
		slog.Info("pipeline stall claim-probe", "kind", kind, "account", l.cfg.AccountID, "drop", id,
			"outcome", res.Outcome.String(), "detail", res.Detail)
		l.commit(ctx, next)
	}
}

func (l *Loop) applyProgress(ctx context.Context, progs []platform.Progress) {
	now := l.cfg.Now()
	for _, p := range progs {
		prev, ok := l.rows[p.BenefitID]
		if !ok {
			continue // reconcile creates the row first
		}
		if p.InstanceID != "" {
			dp := l.progress[p.BenefitID]
			dp.DropID, dp.CampaignID, dp.InstanceID = p.BenefitID, prev.CampaignID, p.InstanceID
			l.progress[p.BenefitID] = dp
		}
		next := dropstate.Apply(prev, dropstate.Observation{
			Known: true, Claimed: p.Claimed, Minutes: p.MinutesWatched, Required: prev.Required,
		}, now)
		l.maybeNotifyProgress(ctx, next)
		l.commit(ctx, next)
	}
}

func (l *Loop) onPubSub(ctx context.Context, e pubsubEvent) {
	switch e.kind {
	case "progress":
		if r, ok := l.rows[e.drop]; ok && r.Required == 0 && e.required > 0 {
			r.Required = e.required
			l.rows[e.drop] = r
		}
		l.applyProgress(ctx, []platform.Progress{{BenefitID: e.drop, MinutesWatched: e.minutes}})
	case "claimable":
		r, ok := l.rows[e.drop]
		if !ok || r.Status == dropstate.Claimed {
			return
		}
		dp := l.progress[e.drop]
		dp.DropID, dp.CampaignID, dp.InstanceID = e.drop, r.CampaignID, e.instance
		l.progress[e.drop] = dp
		if r.Required == 0 {
			// Required isn't known yet (platforms that surface the reward
			// only via pubsub, e.g. Kick). A claimable event is itself proof
			// the drop is done; routing it through Apply/derive would read
			// required<=0 as "blocked: sub_only", which is wrong here.
			next := r
			next.Status, next.Reason = dropstate.Claimable, dropstate.NoReason
			next.RetryAfter = time.Time{}
			next.Source = dropstate.FromPlatform
			next.UpdatedAt = l.cfg.Now()
			l.commit(ctx, next)
			return
		}
		minutes := r.Minutes
		if minutes < r.Required {
			minutes = r.Required
		}
		l.commit(ctx, dropstate.Apply(r, dropstate.Observation{Known: true, Minutes: minutes, Required: r.Required}, l.cfg.Now()))
	case "down":
		if l.current != nil && l.current.Channel.ChannelID != "" && l.current.Channel.ChannelID == e.channel {
			l.cooldowns[strings.ToLower(l.current.Channel.Channel)] = l.cfg.Now().Add(l.cfg.DownCooldown)
		}
	}
}

func (l *Loop) maybeNotifyProgress(ctx context.Context, r dropstate.Row) {
	step := l.cfg.ProgressNotifyStepPct
	if step <= 0 || r.Required <= 0 || l.cfg.Notifier == nil {
		return
	}
	pct := r.Minutes * 100 / r.Required
	if pct > 100 {
		pct = 100
	}
	m := pct / step * step
	last, seen := l.milestones[r.DropID]
	if !seen {
		l.milestones[r.DropID] = m
		return // first sight records a baseline; no restart storms
	}
	if m <= last {
		return // never regress the recorded milestone (e.g. a stale re-sync)
	}
	l.milestones[r.DropID] = m
	b, c := l.benefit(r.DropID)
	l.notify(ctx, "progress", c, b, map[string]any{"cur_min": r.Minutes, "req_min": r.Required})
}

func (l *Loop) claimReady(ctx context.Context) {
	now := l.cfg.Now()
	attempted := false
	for id, r := range l.rows {
		if !dropstate.ReadyToClaim(r, now) {
			continue
		}
		dp, ok := l.progress[id]
		if !ok || dp.DropID == "" {
			dp = platform.DropProgress{DropID: id, CampaignID: r.CampaignID}
		}
		next, res := claimer.Attempt(ctx, l.claimer, l.cfg.Session, r, dp, now)
		slog.Info("pipeline claim attempt", "kind", "claim", "account", l.cfg.AccountID, "drop", id,
			"outcome", res.Outcome.String(), "detail", res.Detail, "link", res.LinkURL)
		l.commit(ctx, next)
		attempted = true
	}
	if attempted {
		l.reconcile(ctx) // corrects lost responses and auto-granted rewards
	}
}

// sameServeSet reports whether a and b hold the same drop ids, ignoring order.
func sameServeSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, id := range a {
		counts[id]++
	}
	for _, id := range b {
		counts[id]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func (l *Loop) replan(ctx context.Context) {
	d := planner.Plan(l.plannerInput())
	if l.current != nil && l.current.Same(d) {
		servesChanged := !sameServeSet(l.current.Serves, d.Serves)
		l.current.Serves, l.current.Reason = d.Serves, d.Reason
		if servesChanged && l.current.Kind != planner.Idle {
			// The running session pinned its stall/claim bookkeeping to the
			// serves set it started with (e.g. a precondition drop that just
			// got claimed, handing the baton to the drop it gated). Restart
			// on the same channel so the session tracks the new set instead
			// of reading "no progress on my old drops" as a stall. Don't
			// touch lastSwap: this isn't a channel swap, so the hysteresis
			// window keeps counting from when we actually landed here.
			// Same channel, so the PubSub subscription stays.
			l.haltSession()
			l.startSession(ctx, *l.current)
		}
		l.updateSnapshot()
		return
	}
	l.haltSession()
	if d.Kind == planner.Idle {
		l.unsubscribe()
	}
	l.current, l.lastSwap = &d, l.cfg.Now()
	slog.Info("pipeline decision", "kind", "state", "account", l.cfg.AccountID,
		"decision", d.Kind.String(), "channel", d.Channel.Channel, "serves", len(d.Serves), "reason", d.Reason)
	if l.cfg.Notifier != nil {
		_ = l.cfg.Notifier.Notify(ctx, "state", map[string]any{"account": l.cfg.AccountID, "state": stateString(&d)})
	}
	if d.Kind != planner.Idle {
		l.startSession(ctx, d)
	}
	l.updateSnapshot()
}

func (l *Loop) startSession(ctx context.Context, d planner.Decision) {
	sctx, cancel := context.WithCancel(ctx)
	l.sessCancel = cancel
	l.gen++
	l.lastInv = nil
	if want := d.Channel.ChannelID; l.subs != nil && want != l.subscribed {
		l.unsubscribe()
		if want != "" {
			l.subs.SubscribeChannel(l.cfg.AccountID, want)
			l.subscribed = want
		}
	}
	ticker := time.NewTicker(l.cfg.BeatEvery)
	cfg := session.Config{
		Backend: l.cfg.Backend, Session: l.cfg.Session, Stream: d.Channel,
		Serves: d.Serves, Ticks: ticker.C, StallPolls: l.cfg.StallPolls, Gen: l.gen,
		AccountID: l.cfg.AccountID,
	}
	if d.Kind == planner.ForceWatch {
		cfg.StallPolls = 0 // channel-points farming has no drop progress to stall on
	}
	done := make(chan struct{})
	l.sessDone = done
	go func() {
		defer close(done)
		defer ticker.Stop()
		session.Run(sctx, cfg, l.sessEvents)
	}()
}

// sessionStopWait bounds how long haltSession waits for the session
// goroutine (and its StopWatch) to finish.
const sessionStopWait = 15 * time.Second

// haltSession cancels the running session and waits, bounded, for its
// goroutine to exit so StopWatch never overlaps the next StartWatch. It
// leaves the PubSub subscription alone.
func (l *Loop) haltSession() {
	if l.sessCancel != nil {
		l.sessCancel()
		l.sessCancel = nil
	}
	if l.sessDone != nil {
		t := time.NewTimer(sessionStopWait)
		select {
		case <-l.sessDone:
		case <-t.C:
			slog.Warn("pipeline: session did not stop in time", "kind", "error", "account", l.cfg.AccountID)
		}
		t.Stop()
		l.sessDone = nil
	}
}

func (l *Loop) unsubscribe() {
	if l.subs != nil && l.subscribed != "" {
		l.subs.UnsubscribeChannel(l.cfg.AccountID, l.subscribed)
		l.subscribed = ""
	}
}

// stopSession is the final stop: halt the session and drop the subscription.
func (l *Loop) stopSession() {
	l.haltSession()
	l.unsubscribe()
}

// stateString maps a decision onto the v1 dashboard state vocabulary.
func stateString(d *planner.Decision) string {
	if d == nil {
		return "pick_campaign"
	}
	switch d.Kind {
	case planner.Mine:
		return "watching"
	case planner.ForceWatch:
		return "force_watch"
	default:
		return "idle"
	}
}

func (l *Loop) updateSnapshot() {
	s := watcher.Snapshot{AccountID: l.cfg.AccountID, State: stateString(l.current)}
	if d := l.current; d != nil && d.Kind != planner.Idle {
		s.Channel, s.ViewerCount, s.StartedAt = d.Channel.Channel, d.Channel.ViewerCount, l.lastSwap
		if len(d.Serves) > 0 {
			b, c := l.benefit(d.Serves[0])
			r := l.rows[d.Serves[0]]
			s.CampaignID, s.CampaignName, s.CampaignGame = c.ID, c.Name, c.Game
			s.BenefitID, s.BenefitName, s.BenefitImage = b.ID, b.Name, b.ImageURL
			s.MinutesWatched, s.RequiredMinutes = r.Minutes, r.Required
		}
	}
	l.mu.Lock()
	l.snap = s
	l.mu.Unlock()
}
