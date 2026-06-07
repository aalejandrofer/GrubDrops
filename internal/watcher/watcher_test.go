package watcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

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
		AccountID:    "acc1",
		Backend:      backend,
		Session:      sess,
		Notifier:     notif,
		TickInterval: 5 * time.Millisecond,
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
		AccountID:    "acc-vanish",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     notif,
		TickInterval: 2 * time.Millisecond,
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
