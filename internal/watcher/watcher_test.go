package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/platform/platformtest"
)

type recordingNotifier struct{ events []string }

func (r *recordingNotifier) Notify(_ context.Context, ev string, _ map[string]any) error {
	r.events = append(r.events, ev)
	return nil
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

	err = w.Run(ctx)
	require.NoError(t, err)

	assert.Contains(t, notif.events, "claim")
	// After claiming the only benefit, pickCampaign finds nothing unclaimed
	// and transitions to StateSleeping before Run returns.
	assert.Equal(t, StateSleeping, w.State())
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

// Watcher must persist EVERY whitelisted campaign it sees (active +
// expired + upcoming), regardless of status. Non-whitelisted campaigns
// MUST NOT reach the persister.
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

	// Three Rust campaigns get persisted, Fortnite is dropped.
	ids := map[string]string{}
	for _, c := range batch {
		ids[c.ID] = c.Status
	}
	assert.Equal(t, "active", ids["c-active"])
	assert.Equal(t, "expired", ids["c-expired"])
	assert.Equal(t, "upcoming", ids["c-upcoming"])
	_, blockedSeen := ids["c-blocked"]
	assert.False(t, blockedSeen, "non-whitelisted campaign must NEVER reach the persister")
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
