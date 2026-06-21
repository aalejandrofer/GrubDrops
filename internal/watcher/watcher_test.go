package watcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// TestShouldNotifyProgress_MilestoneSteps verifies progress notifications land
// only on milestone crossings (0% start, each step%, 100%) — never repeatedly
// for the same milestone, which was the Discord spam (Kick stuck at 0/120
// polled every ~60s). Step 0 disables progress notifications entirely.
func TestShouldNotifyProgress_MilestoneSteps(t *testing.T) {
	const req = 120
	w := &Watcher{lastNotifiedMilestone: -1}
	w.cfg.ProgressNotifyStepPct = 50

	if !w.shouldNotifyProgress(0, req) {
		t.Fatal("0% (start) should notify once")
	}
	if w.shouldNotifyProgress(0, req) {
		t.Fatal("repeated 0% must not re-notify (this was the spam)")
	}
	if w.shouldNotifyProgress(30, req) { // 25%
		t.Fatal("25% should not notify with a 50% step")
	}
	if !w.shouldNotifyProgress(60, req) { // 50%
		t.Fatal("50% should notify")
	}
	if w.shouldNotifyProgress(60, req) {
		t.Fatal("repeated 50% must not re-notify")
	}
	if w.shouldNotifyProgress(90, req) { // 75%
		t.Fatal("75% should not notify with a 50% step")
	}
	if !w.shouldNotifyProgress(120, req) { // 100%
		t.Fatal("100% should notify")
	}
	if w.shouldNotifyProgress(130, req) { // capped at 100%
		t.Fatal("past 100% must not re-notify")
	}
}

func TestShouldNotifyProgress_StepZeroDisables(t *testing.T) {
	w := &Watcher{lastNotifiedMilestone: -1}
	w.cfg.ProgressNotifyStepPct = 0
	for _, m := range []int{0, 60, 120} {
		if w.shouldNotifyProgress(m, 120) {
			t.Fatalf("step 0 disables progress notifications; fired at %d", m)
		}
	}
}

type recordingNotifier struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	return nil
}

// has reports whether the given event was recorded. Safe to call while
// the watcher goroutine is still emitting events.
func (r *recordingNotifier) has(ev string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e == ev {
			return true
		}
	}
	return false
}

func TestWatcher_MinesUntilClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := platformtest.New()
	notif := &recordingNotifier{}

	sess, err := backend.PollDeviceLogin(ctx, platform.DeviceChallenge{})
	require.NoError(t, err)

	w := New(Config{
		AccountID:         "acc1",
		Backend:           backend,
		Session:           sess,
		Notifier:          notif,
		TickInterval:      5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
	})

	// Sleeping is no longer terminal — after claiming the only benefit the
	// watcher enters StateSleeping then re-arms (recheckInterval) so it can
	// pick up newly-active campaigns / freshly-linked accounts without a
	// manual Reload. So Run no longer returns on its own; drive it in a
	// goroutine, wait until it has claimed and reached sleeping, then cancel
	// and assert a clean context-cancelled exit.
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	require.Eventually(t, func() bool {
		return notif.has("claim")
	}, 4*time.Second, 5*time.Millisecond, "watcher should claim the only benefit")

	// Having claimed everything, the watcher parks in the sleeping/recheck
	// cycle rather than exiting. Cancel and confirm a clean shutdown.
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// New() must plumb AllowGame into Session.GameFilter so backends can
// short-circuit non-whitelisted games at the source. Regression guard:
// if someone removes the plumbing the whitelist degrades to a
// watcher-only filter and backends waste bandwidth.
func TestWatcher_New_PropagatesAllowGameToSession(t *testing.T) {
	allow := func(g string) bool { return g == "Rust" }
	w := New(Config{
		AccountID: "acc1",
		Backend:   platformtest.New(),
		Session:   platform.Session{AccessToken: "tok"},
		Notifier:  &recordingNotifier{},
		AllowGame: allow,
	})
	require.NotNil(t, w.cfg.Session.GameFilter)
	assert.True(t, w.cfg.Session.GameFilter("Rust"))
	assert.False(t, w.cfg.Session.GameFilter("Fortnite"))
	assert.Equal(t, "acc1", w.cfg.Session.AccountID)
}

// recordingPersister captures every batch the watcher pushes to it so we
// can assert the whitelist gate and status filter run BEFORE persistence.
type recordingPersister struct {
	batches [][]platform.Campaign
}

func (r *recordingPersister) PersistCampaigns(_ context.Context, camps []platform.Campaign) error {
	r.batches = append(r.batches, append([]platform.Campaign(nil), camps...))
	return nil
}

// multiStatusBackend mimics a Twitch backend that returns active +
// expired + upcoming campaigns. It exists so the watcher test can verify
// the persister sees ALL statuses while mining only touches the ACTIVE
// one.
type multiStatusBackend struct{ *platformtest.MockBackend }

func (m *multiStatusBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{
		{ID: "c-active", Game: "Rust", Status: "active", Name: "Active",
			Benefits: []platform.DropBenefit{{ID: "drop1", CampaignID: "c-active", Name: "Drop", RequiredMinutes: 2}}},
		{ID: "c-expired", Game: "Rust", Status: "expired", Name: "Past"},
		{ID: "c-upcoming", Game: "Rust", Status: "upcoming", Name: "Future"},
		{ID: "c-blocked", Game: "Fortnite", Status: "active", Name: "Blocked"},
	}, nil
}

// Watcher must persist EVERY campaign it sees — whitelisted and
// non-whitelisted alike (the /drops Discoverable tab depends on the
// latter). Status filtering happens downstream, not at the persister.
func TestWatcher_PersistsAllWhitelistedStatuses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec := &recordingPersister{}
	notif := &recordingNotifier{}
	w := New(Config{
		AccountID:    "acc1",
		Backend:      &multiStatusBackend{platformtest.New()},
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     notif,
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
		Persister:    rec,
	})

	_ = w.Run(ctx)
	require.NotEmpty(t, rec.batches, "persister must be invoked at least once")
	batch := rec.batches[0]

	ids := map[string]string{}
	for _, c := range batch {
		ids[c.ID] = c.Status
	}
	assert.Equal(t, "active", ids["c-active"])
	assert.Equal(t, "expired", ids["c-expired"])
	assert.Equal(t, "upcoming", ids["c-upcoming"])
	// Non-whitelisted campaigns are persisted as shell rows so the
	// /drops Discoverable tab can list them; the watcher's mining
	// loop still skips them via AllowGame.
	assert.Equal(t, "active", ids["c-blocked"], "non-whitelisted campaign must reach persister for Discoverable")
}

// rewardOnlyBackend returns a single ACTIVE Twitch campaign of kind="reward"
// with empty Benefits. The watcher MUST skip it from the mining loop AND
// dispatch the reward reaper if the backend implements RewardClaimer.
type rewardOnlyBackend struct {
	*platformtest.MockBackend
	calls    int
	games    []string
	claimRet []platform.ClaimedReward
}

func (r *rewardOnlyBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{
		{ID: "minecraft|builder-cape", Platform: "twitch", Game: "Minecraft",
			Status: "active", Kind: "reward", AccountLinked: true, Name: "Builder Cape"},
	}, nil
}

func (r *rewardOnlyBackend) ClaimRewards(_ context.Context, _ platform.Session, allowed []string) ([]platform.ClaimedReward, error) {
	r.calls++
	r.games = append([]string{}, allowed...)
	return r.claimRet, nil
}

// vanishBackend simulates a Twitch code-style drop: progress accrues
// for a few ticks, then the benefit silently disappears from
// dropCampaignsInProgress (Twitch behaviour when a code drop is issued
// or campaign expires mid-watch). The watcher must detect the vanish
// and re-enter PickCampaign instead of mining a ghost benefit forever
// (B2.5).
type vanishBackend struct {
	*platformtest.MockBackend
	mu         sync.Mutex
	inv        []platform.Progress
	stopCalled int
}

func (v *vanishBackend) InventoryProgress(_ context.Context, _ platform.Session) ([]platform.Progress, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]platform.Progress, len(v.inv))
	copy(out, v.inv)
	return out, nil
}

func (v *vanishBackend) setProgress(p []platform.Progress) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.inv = p
}

func (v *vanishBackend) StopWatch(_ context.Context, _ platform.WatchHandle) error {
	v.mu.Lock()
	v.stopCalled++
	v.mu.Unlock()
	return nil
}

func (v *vanishBackend) stops() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.stopCalled
}

// vanishBackend.ListActiveCampaigns returns one drop benefit with a
// high RequiredMinutes so the watcher stays in tickWatch (never
// transitions to Claiming on its own).
func (v *vanishBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "camp1", Game: "Rust", Name: "Rust Camp", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{{ID: "drop1", CampaignID: "camp1", Name: "Drop", RequiredMinutes: 999}},
	}}, nil
}

// TestWatcher_VanishDetect: progress 1/999 then 2/999, then benefit
// disappears from inventory for 3 consecutive ticks. Watcher must call
// StopWatch and re-enter PickCampaign rather than continue heartbeating
// against a benefit that will never finish.
func TestWatcher_VanishDetect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &vanishBackend{MockBackend: platformtest.New()}
	backend.setProgress([]platform.Progress{{BenefitID: "drop1", MinutesWatched: 2}})

	notif := &recordingNotifier{}
	w := New(Config{
		AccountID:         "acc-vanish",
		Backend:           backend,
		Session:           platform.Session{AccessToken: "tok"},
		Notifier:          notif,
		TickInterval:      2 * time.Millisecond,
		HeartbeatInterval: 2 * time.Millisecond,
	})

	go func() { _ = w.Run(ctx) }()

	// Wait for watcher to reach watching state and accumulate at least
	// one matched-progress tick.
	require.Eventually(t, func() bool {
		s := w.Snapshot()
		return s.MinutesWatched >= 1
	}, time.Second, 5*time.Millisecond, "watcher never observed progress")

	// Now vanish the benefit. Watcher should StopWatch + reset within
	// vanishThreshold ticks.
	backend.setProgress(nil)
	require.Eventually(t, func() bool {
		return backend.stops() >= 1
	}, time.Second, 5*time.Millisecond, "watcher did not StopWatch after benefit vanished")
}

// excludeBackend returns two ACTIVE Rust campaigns; the watcher must
// skip the one whose Game is in the ExcludeGame predicate (P3).
type excludeBackend struct {
	*platformtest.MockBackend
	mu     sync.Mutex
	picked string
}

func (e *excludeBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{
		{ID: "skip", Game: "Skipme", Status: "active", AccountLinked: true,
			Benefits: []platform.DropBenefit{{ID: "d_skip", RequiredMinutes: 2}}},
		{ID: "ok", Game: "Rust", Status: "active", AccountLinked: true,
			Benefits: []platform.DropBenefit{{ID: "d_ok", RequiredMinutes: 2}}},
	}, nil
}

func (e *excludeBackend) ListEligibleChannels(_ context.Context, _ platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	e.mu.Lock()
	if e.picked == "" {
		e.picked = c.ID
	}
	e.mu.Unlock()
	return []platform.Stream{{Channel: "streamer"}}, nil
}

func (e *excludeBackend) firstPicked() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.picked
}

// TestWatcher_ExcludeGame skips campaigns whose game matches the
// exclude predicate (P3).
func TestWatcher_ExcludeGame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &excludeBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:    "acc_excl",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 2 * time.Millisecond,
		AllowGame:    func(g string) bool { return true },
		ExcludeGame:  func(g string) bool { return g == "Skipme" },
	})

	go func() { _ = w.Run(ctx) }()
	require.Eventually(t, func() bool { return backend.firstPicked() != "" },
		time.Second, 5*time.Millisecond, "watcher never picked any campaign")
	assert.Equal(t, "ok", backend.firstPicked(), "ExcludeGame must skip Skipme")
}

// lowAvblBackend returns multiple ACTIVE campaigns with varying
// AllowedChannelCount values so we can verify PriorityMode
// "low_avbl_first" picks the scarcest campaign first (P1).
type lowAvblBackend struct {
	*platformtest.MockBackend
	picked string
	mu     sync.Mutex
}

func (l *lowAvblBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{
		{ID: "wide", Game: "Rust", Status: "active", AccountLinked: true,
			AllowedChannelCount: 50,
			Benefits:            []platform.DropBenefit{{ID: "drop_wide", CampaignID: "wide", RequiredMinutes: 2}}},
		{ID: "narrow", Game: "Rust", Status: "active", AccountLinked: true,
			AllowedChannelCount: 3,
			Benefits:            []platform.DropBenefit{{ID: "drop_narrow", CampaignID: "narrow", RequiredMinutes: 2}}},
		{ID: "any", Game: "Rust", Status: "active", AccountLinked: true,
			AllowedChannelCount: 0, // unrestricted — sorted last
			Benefits:            []platform.DropBenefit{{ID: "drop_any", CampaignID: "any", RequiredMinutes: 2}}},
	}, nil
}

func (l *lowAvblBackend) ListEligibleChannels(_ context.Context, _ platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	l.mu.Lock()
	if l.picked == "" {
		l.picked = c.ID
	}
	l.mu.Unlock()
	return []platform.Stream{{Channel: "streamer"}}, nil
}

func (l *lowAvblBackend) firstPicked() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.picked
}

// TestWatcher_LowAvblFirst_PicksScarceFirst (P1)
func TestWatcher_LowAvblFirst_PicksScarceFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &lowAvblBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:    "acc_low",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 2 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
		PriorityMode: "low_avbl_first",
	})

	go func() { _ = w.Run(ctx) }()

	require.Eventually(t, func() bool {
		return backend.firstPicked() != ""
	}, time.Second, 5*time.Millisecond, "watcher never reached pickStream")

	assert.Equal(t, "narrow", backend.firstPicked(),
		"low_avbl_first must prefer scarcest campaign (narrow), not wide or unrestricted")
}

// offlineFirstBackend returns two ACTIVE campaigns of the same game.
// The higher-priority one ("dead", listed first) has NO live channels;
// the lower-priority one ("live") does. The watcher must skip the dead
// campaign and advance to the live one instead of sleeping forever on
// the highest-priority pick (esports-channel trap).
type offlineFirstBackend struct {
	*platformtest.MockBackend
	mu     sync.Mutex
	picked string
}

func (o *offlineFirstBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{
		{ID: "dead", Game: "Rust", Status: "active", AccountLinked: true,
			Benefits: []platform.DropBenefit{{ID: "d_dead", CampaignID: "dead", RequiredMinutes: 2}}},
		{ID: "live", Game: "Rust", Status: "active", AccountLinked: true,
			Benefits: []platform.DropBenefit{{ID: "d_live", CampaignID: "live", RequiredMinutes: 2}}},
	}, nil
}

func (o *offlineFirstBackend) ListEligibleChannels(_ context.Context, _ platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	if c.ID == "dead" {
		return nil, nil // no live broadcaster
	}
	o.mu.Lock()
	if o.picked == "" {
		o.picked = c.ID
	}
	o.mu.Unlock()
	return []platform.Stream{{Channel: "streamer"}}, nil
}

func (o *offlineFirstBackend) firstPicked() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.picked
}

// TestWatcher_SkipsOfflineCampaignToNextLive: when the top-priority
// campaign has no live channel, the watcher must advance to the next
// eligible campaign that does — not get stuck re-picking the dead one.
func TestWatcher_SkipsOfflineCampaignToNextLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &offlineFirstBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:    "acc_offline",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 2 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
	})

	go func() { _ = w.Run(ctx) }()
	require.Eventually(t, func() bool { return backend.firstPicked() != "" },
		time.Second, 5*time.Millisecond, "watcher never advanced to a live campaign")
	assert.Equal(t, "live", backend.firstPicked(),
		"watcher must skip the offline 'dead' campaign and mine the live one")
}

// restrictedFirstBackend returns an OPEN campaign (no AllowedChannels)
// ahead of a channel-RESTRICTED one. On Kick the open campaign accrues
// passively on any participating channel, so the watcher must actively
// mine the restricted one first.
type restrictedFirstBackend struct {
	*platformtest.MockBackend
	mu     sync.Mutex
	picked string
}

func (r *restrictedFirstBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{
		{ID: "open", Game: "Rust", Status: "active", AccountLinked: true,
			Benefits: []platform.DropBenefit{{ID: "d_open", CampaignID: "open", RequiredMinutes: 2}}},
		{ID: "team", Game: "Rust", Status: "active", AccountLinked: true,
			AllowedChannels: []string{"teamchan"},
			Benefits:        []platform.DropBenefit{{ID: "d_team", CampaignID: "team", RequiredMinutes: 2}}},
	}, nil
}

func (r *restrictedFirstBackend) ListEligibleChannels(_ context.Context, _ platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	r.mu.Lock()
	if r.picked == "" {
		r.picked = c.ID
	}
	r.mu.Unlock()
	return []platform.Stream{{Channel: "streamer"}}, nil
}

func (r *restrictedFirstBackend) firstPicked() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.picked
}

// TestWatcher_KickPicksRestrictedCampaignFirst: on Kick, open campaigns
// (empty AllowedChannels) accrue watch-time passively while any
// participating channel is watched, so the watcher must spend its watch
// slot on channel-restricted (team) campaigns first.
func TestWatcher_KickPicksRestrictedCampaignFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &restrictedFirstBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:    "acc_kick_restricted",
		Platform:     "kick",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 2 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
	})

	go func() { _ = w.Run(ctx) }()
	require.Eventually(t, func() bool { return backend.firstPicked() != "" },
		time.Second, 5*time.Millisecond, "watcher never picked a campaign")
	assert.Equal(t, "team", backend.firstPicked(),
		"kick watcher must mine the channel-restricted campaign first; open ones accrue passively")
}

// TestWatcher_TwitchPicksRestrictedCampaignFirst: the restricted-first
// partition applies to Twitch too — restricted campaigns are limited to
// specific broadcasters live only in narrow windows, so they're mined ahead
// of open campaigns (which can be done from any channel for the game anytime).
func TestWatcher_TwitchPicksRestrictedCampaignFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &restrictedFirstBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:    "acc_twitch_order",
		Platform:     "twitch",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 2 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
	})

	go func() { _ = w.Run(ctx) }()
	require.Eventually(t, func() bool { return backend.firstPicked() != "" },
		time.Second, 5*time.Millisecond, "watcher never picked a campaign")
	assert.Equal(t, "team", backend.firstPicked(),
		"twitch watcher must mine the channel-restricted campaign first")
}

// pubsubAwareBackend captures hook registration calls so we can verify
// the watcher registers per-account PubSub hooks at construction time.
type pubsubAwareBackend struct {
	*platformtest.MockBackend
	hooks   map[string]platform.PubSubHooks
	subs    []string
	unsubs  []string
	hooksMu sync.Mutex
}

func (p *pubsubAwareBackend) SetAccountPubSubHooks(accountID string, h platform.PubSubHooks) {
	p.hooksMu.Lock()
	defer p.hooksMu.Unlock()
	if p.hooks == nil {
		p.hooks = map[string]platform.PubSubHooks{}
	}
	p.hooks[accountID] = h
}

func (p *pubsubAwareBackend) SubscribeChannel(_, channelID string) {
	p.hooksMu.Lock()
	defer p.hooksMu.Unlock()
	p.subs = append(p.subs, channelID)
}

func (p *pubsubAwareBackend) UnsubscribeChannel(_, channelID string) {
	p.hooksMu.Lock()
	defer p.hooksMu.Unlock()
	p.unsubs = append(p.unsubs, channelID)
}

// TestWatcher_RegistersPubSubHooks: the watcher constructor must call
// SetAccountPubSubHooks so the backend's lazy PubSub bootstrap picks
// up the watcher's callbacks. Without this F1 doesn't fire.
func TestWatcher_RegistersPubSubHooks(t *testing.T) {
	backend := &pubsubAwareBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID: "acc_hooks",
		Backend:   backend,
		Session:   platform.Session{AccessToken: "tok"},
		Notifier:  &recordingNotifier{},
	})
	_ = w

	backend.hooksMu.Lock()
	defer backend.hooksMu.Unlock()
	hooks, ok := backend.hooks["acc_hooks"]
	require.True(t, ok, "hooks not registered for account")
	assert.NotNil(t, hooks.OnDropProgress)
	assert.NotNil(t, hooks.OnDropClaim)
	assert.NotNil(t, hooks.OnStreamDown)
	assert.NotNil(t, hooks.OnStreamUp)
}

// TestWatcher_PubSubHooks_UpdateProgress: the OnDropProgress hook must
// update lastProgressMin so the dashboard reflects real-time progress
// between inventory polls.
func TestWatcher_PubSubHooks_UpdateProgress(t *testing.T) {
	backend := &pubsubAwareBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID: "acc1",
		Backend:   backend,
		Session:   platform.Session{AccessToken: "tok"},
		Notifier:  &recordingNotifier{},
	})

	// Simulate watcher having picked a benefit.
	w.mu.Lock()
	w.currentBenefit = &platform.DropBenefit{ID: "drop1", RequiredMinutes: 10}
	w.mu.Unlock()

	backend.hooksMu.Lock()
	hooks := backend.hooks["acc1"]
	backend.hooksMu.Unlock()
	require.NotNil(t, hooks.OnDropProgress)

	hooks.OnDropProgress("drop1", 7, 10)

	snap := w.Snapshot()
	assert.Equal(t, 7, snap.MinutesWatched, "OnDropProgress must advance lastProgressMin")
}

// Watcher must fire the reward reaper when a whitelisted kind=reward
// campaign is discovered and the backend implements RewardClaimer.
func TestWatcher_RewardReaperFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec := &recordingPersister{}
	notif := &recordingNotifier{}
	backend := &rewardOnlyBackend{
		MockBackend: platformtest.New(),
		claimRet:    []platform.ClaimedReward{{Game: "Minecraft", Title: "Builder Cape"}},
	}
	w := New(Config{
		AccountID:    "acc1",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     notif,
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Minecraft" },
		Persister:    rec,
	})

	_ = w.Run(ctx)
	assert.GreaterOrEqual(t, backend.calls, 1, "ClaimRewards must be invoked at least once")
	assert.Contains(t, backend.games, "Minecraft", "reaper must scope claim to whitelisted game")
}

// errHeartbeatBackend starts a watch successfully, then fails every
// Heartbeat with a TRANSIENT (non-cancel) error — simulating a Kick WS
// presence loop that dies mid-watch. tickWatch surfaces that as a step
// error, so Run takes its transient-error → PickCampaign branch. Each
// StartWatch hands back a distinct live handle; StopWatch records how many
// of those handles the watcher actually tore down.
type errHeartbeatBackend struct {
	*platformtest.MockBackend
	mu          sync.Mutex
	startCalled int
	stopCalled  int
}

func (e *errHeartbeatBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "camp1", Game: "Rust", Name: "Rust Camp", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{{ID: "drop1", CampaignID: "camp1", Name: "Drop", RequiredMinutes: 9999}},
	}}, nil
}

func (e *errHeartbeatBackend) ListEligibleChannels(_ context.Context, _ platform.Session, _ platform.Campaign) ([]platform.Stream, error) {
	return []platform.Stream{{Channel: "chan1", DropsEnabled: true}}, nil
}

func (e *errHeartbeatBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	e.mu.Lock()
	e.startCalled++
	n := e.startCalled
	e.mu.Unlock()
	// Non-nil Internal so the handle looks like a real, live watch the
	// watcher is obligated to stop (mirrors the WS path's *kickWSWatch).
	return platform.WatchHandle{Channel: s.Channel, Internal: n}, nil
}

func (e *errHeartbeatBackend) Heartbeat(_ context.Context, _ platform.WatchHandle) error {
	return errors.New("ws presence loop died")
}

func (e *errHeartbeatBackend) StopWatch(_ context.Context, _ platform.WatchHandle) error {
	e.mu.Lock()
	e.stopCalled++
	e.mu.Unlock()
	return nil
}

func (e *errHeartbeatBackend) starts() int { e.mu.Lock(); defer e.mu.Unlock(); return e.startCalled }
func (e *errHeartbeatBackend) stops() int  { e.mu.Lock(); defer e.mu.Unlock(); return e.stopCalled }

// TestWatcher_StopsWatchOnTransientError: when a watch is live and the tick
// fails with a transient error (e.g. the Kick WS presence loop died), Run
// must STOP that watch before falling back to PickCampaign. Otherwise the
// pure-WS path leaks the background presence goroutine (it runs on its own
// context, only stoppable via the handle we're about to discard) and the
// next StartWatch opens a SECOND presence for the same account — the server
// credits one watch per account, so the new one accrues nothing and the
// watcher death-loops join→bounce→pick_campaign forever.
func TestWatcher_StopsWatchOnTransientError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &errHeartbeatBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:         "acc-err",
		Backend:           backend,
		Session:           platform.Session{AccessToken: "tok"},
		Notifier:          &recordingNotifier{},
		TickInterval:      2 * time.Millisecond,
		HeartbeatInterval: 2 * time.Millisecond,
	})

	go func() { _ = w.Run(ctx) }()

	// The watcher must reach the watch and then fail the heartbeat tick.
	require.Eventually(t, func() bool {
		return backend.starts() >= 1
	}, time.Second, 2*time.Millisecond, "watcher never started a watch")

	// Every started watch must be torn down: a live handle abandoned on the
	// error path is a leaked WS presence.
	require.Eventually(t, func() bool {
		return backend.stops() >= 1
	}, time.Second, 2*time.Millisecond, "watcher abandoned a live watch on the transient-error path (leaked WS presence)")
}

// freezeBackend simulates a server-side stall: the current benefit is
// ALWAYS present in inventory (so the vanish-detect path never fires) but
// its MinutesWatched is controllable. When held flat it must trip the
// freeze detector; when advancing it must not. RequiredMinutes is high so
// the watcher never reaches the claim path on its own.
type freezeBackend struct {
	*platformtest.MockBackend
	mu         sync.Mutex
	minutes    int
	advance    bool // when true, each InventoryProgress poll bumps minutes
	stopCalled int
}

func (f *freezeBackend) InventoryProgress(_ context.Context, _ platform.Session) ([]platform.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.advance {
		f.minutes++
	}
	return []platform.Progress{{BenefitID: "drop1", MinutesWatched: f.minutes}}, nil
}

func (f *freezeBackend) StopWatch(_ context.Context, _ platform.WatchHandle) error {
	f.mu.Lock()
	f.stopCalled++
	f.mu.Unlock()
	return nil
}

func (f *freezeBackend) stops() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalled
}

func (f *freezeBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "camp1", Game: "Rust", Name: "Rust Camp", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{{ID: "drop1", CampaignID: "camp1", Name: "Drop", RequiredMinutes: 9999}},
	}}, nil
}

// TestWatcher_FreezeDetect_RotatesOnStalledMinutes: the benefit stays
// present in inventory but its MinutesWatched never advances. After
// freezeThreshold consecutive flat polls the watcher must rotate off the
// channel — StopWatch is called and the state leaves StateWatching — even
// though the vanish-detect path (benefit absent) never triggers.
func TestWatcher_FreezeDetect_RotatesOnStalledMinutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	backend := &freezeBackend{MockBackend: platformtest.New(), minutes: 5}

	notif := &recordingNotifier{}
	w := New(Config{
		AccountID:         "acc-freeze",
		Backend:           backend,
		Session:           platform.Session{AccessToken: "tok"},
		Notifier:          notif,
		TickInterval:      2 * time.Millisecond,
		HeartbeatInterval: 2 * time.Millisecond,
	})

	go func() { _ = w.Run(ctx) }()

	// Watcher reaches watching and observes the (flat) progress.
	require.Eventually(t, func() bool {
		return w.Snapshot().MinutesWatched >= 5
	}, time.Second, 2*time.Millisecond, "watcher never observed progress")

	// With minutes held flat, the freeze detector must rotate off the
	// channel within freezeThreshold polls.
	require.Eventually(t, func() bool {
		return backend.stops() >= 1
	}, 2*time.Second, 2*time.Millisecond, "watcher did not StopWatch on a frozen (non-advancing) benefit")

	// And it must not be sitting in StateWatching against the stalled
	// channel (the only channel is on cooldown, so it parks elsewhere).
	require.Eventually(t, func() bool {
		return w.State() != StateWatching
	}, time.Second, 2*time.Millisecond, "watcher stayed in StateWatching after freeze")
}

// TestWatcher_FreezeDetect_NoRotateWhenAdvancing: when MinutesWatched
// advances by 1 every poll the watch is healthy and must NOT rotate —
// guards against the freeze detector false-positiving on normal accrual.
func TestWatcher_FreezeDetect_NoRotateWhenAdvancing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	backend := &freezeBackend{MockBackend: platformtest.New(), advance: true}

	w := New(Config{
		AccountID:         "acc-advance",
		Backend:           backend,
		Session:           platform.Session{AccessToken: "tok"},
		Notifier:          &recordingNotifier{},
		TickInterval:      2 * time.Millisecond,
		HeartbeatInterval: 2 * time.Millisecond,
	})

	go func() { _ = w.Run(ctx) }()

	// Let many polls accrue (well past freezeThreshold) with minutes
	// advancing each time.
	require.Eventually(t, func() bool {
		return w.Snapshot().MinutesWatched >= freezeThreshold+5
	}, time.Second, 2*time.Millisecond, "watcher never accrued advancing progress")

	// A healthy, advancing watch must never have rotated.
	assert.Equal(t, 0, backend.stops(), "advancing watch must not trip the freeze detector")
	assert.Equal(t, StateWatching, w.State(), "advancing watch must stay in StateWatching")
}

func TestFirstUnmetPrecondition(t *testing.T) {
	claimed := map[string]bool{"dropA": true}
	// No preconditions -> always met.
	if got := firstUnmetPrecondition(nil, claimed); got != "" {
		t.Fatalf("empty preconditions should be met, got %q", got)
	}
	// All preconditions claimed -> met.
	if got := firstUnmetPrecondition([]string{"dropA"}, claimed); got != "" {
		t.Fatalf("claimed precondition should be met, got %q", got)
	}
	// Unmet precondition -> returns its id.
	if got := firstUnmetPrecondition([]string{"dropA", "dropB"}, claimed); got != "dropB" {
		t.Fatalf("want dropB unmet, got %q", got)
	}
}

// fieldRecordingNotifier captures both the event name and fields of every
// notification so a test can assert WHICH reward triggered a claim embed.
type fieldRecordingNotifier struct {
	mu     sync.Mutex
	events []string
	fields []map[string]any
}

func (r *fieldRecordingNotifier) Notify(_ context.Context, ev string, f map[string]any) error {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.fields = append(r.fields, f)
	r.mu.Unlock()
	return nil
}

// claimDrops returns the "drop" field of every recorded "claim" event.
func (r *fieldRecordingNotifier) claimDrops() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i, ev := range r.events {
		if ev != "claim" {
			continue
		}
		if d, ok := r.fields[i]["drop"].(string); ok {
			out = append(out, d)
		}
	}
	return out
}

// A reward picked up by the multi-reward sweep (Kick CompletedSweeper) must
// fire the same "claim" Discord notification the benefit-complete path uses —
// otherwise swept sibling rewards are silent. The actively-mined
// currentBenefit must NOT be double-notified (the StateClaiming flow owns it),
// and a reward that resurfaces on a later poll must notify only once.
func TestWatcher_NotifySwept_SiblingNotifiesCurrentExcludedAndDeduped(t *testing.T) {
	ctx := context.Background()
	notif := &fieldRecordingNotifier{}
	w := New(Config{
		AccountID: "acc1",
		Backend:   platformtest.New(),
		Session:   platform.Session{AccessToken: "tok"},
		Notifier:  notif,
		Platform:  "kick",
	})
	// The currentBenefit is what the benefit-complete path claims + notifies.
	w.currentBenefit = &platform.DropBenefit{ID: "logo", Name: "Kick + Rust Wallpaper Logo"}

	sibling := platform.ClaimedReward{Game: "Rust", Title: "Kick + Rust Wallpaper Pattern"}
	current := platform.ClaimedReward{Game: "Rust", Title: "Kick + Rust Wallpaper Logo"}

	// Sweep returns both the sibling AND the currentBenefit (the dedupe case).
	w.notifySwept(ctx, sibling)
	w.notifySwept(ctx, current)
	// Next poll resurfaces the sibling (claim POST raced ahead of progress).
	w.notifySwept(ctx, sibling)

	drops := notif.claimDrops()
	require.Equal(t, []string{"Kick + Rust Wallpaper Pattern"}, drops,
		"only the sibling notifies: currentBenefit excluded, sibling deduped to one")
}

// nullGameBackend returns a single ACTIVE campaign with no game and one
// participating channel — the Kick "Football Drop" shape. ListEligibleChannels
// is inherited from MockBackend (returns a live stream), so a campaign that
// passes the filter will accrue heartbeats.
type nullGameBackend struct{ *platformtest.MockBackend }

func (n *nullGameBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "c-football", Game: "", Status: "active", Name: "Football Drop",
		AllowedChannels: []string{"adrianozendejas32"},
		Benefits: []platform.DropBenefit{
			{ID: "drop1", CampaignID: "c-football", Name: "Jersey", RequiredMinutes: 2},
		},
	}}, nil
}

func TestWatcher_MinesNullGameWhenChannelWhitelisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &nullGameBackend{platformtest.New()}
	w := New(Config{
		AccountID:    "acc1",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" }, // null game NOT whitelisted
		AllowChannel: func(chs []string) bool {
			for _, c := range chs {
				if c == "adrianozendejas32" {
					return true
				}
			}
			return false
		},
	})
	_ = w.Run(ctx)
	assert.Greater(t, backend.Heartbeats(), int64(0),
		"null-game campaign with a whitelisted channel must be mined")
}

func TestWatcher_SkipsNullGameWhenChannelNotWhitelisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	backend := &nullGameBackend{platformtest.New()}
	w := New(Config{
		AccountID:    "acc1",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
		AllowChannel: func(chs []string) bool { return false },
	})
	_ = w.Run(ctx)
	assert.Equal(t, int64(0), backend.Heartbeats(),
		"null-game campaign with no matching channel must not be mined")
}
