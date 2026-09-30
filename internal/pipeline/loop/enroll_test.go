package loop

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeClock is a settable Config.Now.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// enrollSetup is an idle TV account: no campaigns visible, whitelist
// [A, B], each game's directory listing one drops-enabled channel.
func enrollSetup(t *testing.T) (*fakeBackend, Config) {
	f, _, _, cfg := setup(t)
	f.camps, f.progress, f.inventory = nil, nil, nil
	f.gameLive = map[string][]platform.Stream{
		// A higher-viewer stream without drops must never be picked.
		"A": {{Channel: "nodrops", ViewerCount: 500}, {Channel: "a1", ViewerCount: 100, DropsEnabled: true}},
		"B": {{Channel: "b1", ViewerCount: 50, DropsEnabled: true}},
	}
	cfg.Games = []string{"A", "B"}
	cfg.AllowGame = func(g string) bool { return g == "A" || g == "B" }
	cfg.EnrollDiscovery = true
	cfg.EnrollWatch = time.Hour // tests shorten it where they need a finish
	return f, cfg
}

func (f *fakeBackend) watched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.watching...)
}

func (f *fakeBackend) reconciles() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listGames)
}

func TestEnroll_RoundRobinWithCooldown(t *testing.T) {
	f, cfg := enrollSetup(t)
	cfg.EnrollWatch = 80 * time.Millisecond
	l, _ := run(t, cfg)

	require.Eventually(t, func() bool {
		s := l.Snapshot()
		return s.State == "discovering" && s.Channel == "a1"
	}, 2*time.Second, 2*time.Millisecond, "enroll starts on A's top drops-enabled channel")
	assert.Equal(t, "A", l.Snapshot().CampaignGame)
	recBefore := f.reconciles()

	// After EnrollWatch the A session stops, a reconcile runs, and the next
	// idle cycle moves on to B (A is on cooldown).
	require.Eventually(t, func() bool {
		w := f.watched()
		return len(w) == 2 && w[1] == "b1"
	}, 2*time.Second, 2*time.Millisecond)
	assert.Greater(t, f.reconciles(), recBefore, "post-enroll reconcile ran")
	f.mu.Lock()
	assert.GreaterOrEqual(t, f.stops, 1, "A session stopped before B started")
	f.mu.Unlock()
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "b1" }, time.Second, 2*time.Millisecond)

	// Both games probed: back to plain idle, no further StartWatch.
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 2*time.Millisecond)
	l.Nudge()
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, []string{"a1", "b1"}, f.watched(), "every game on cooldown: stay idle")
	assert.Equal(t, "idle", l.Snapshot().State)
}

func TestEnroll_ReconcileFindsCampaignThenMines(t *testing.T) {
	f, cfg := enrollSetup(t)
	cfg.EnrollWatch = 60 * time.Millisecond
	// Watching a1 enrolls the account: the next reconcile lists c1 (game A).
	f.onStartWatch = func(ch string) {
		if ch != "a1" {
			return
		}
		f.camps = []platform.Campaign{{ID: "c1", Platform: "twitch", Game: "A", Name: "Camp", Status: "active",
			AccountLinked: true, AccountLinkChecked: true,
			Benefits: []platform.DropBenefit{{ID: "d1", CampaignID: "c1", Name: "Drop", RequiredMinutes: 60}}}}
		f.progress = []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 0, Required: 60, Known: true}}
		f.live = []platform.Stream{{Channel: "ch1", ViewerCount: 10}}
	}
	l, _ := run(t, cfg)

	require.Eventually(t, func() bool {
		s := l.Snapshot()
		return s.State == "watching" && s.Channel == "ch1"
	}, 2*time.Second, 2*time.Millisecond, "post-enroll reconcile surfaced a mineable drop: mine it")
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, []string{"a1", "ch1"}, f.watched(), "mining preempts further enrolling (no b1)")
	assert.Equal(t, "watching", l.Snapshot().State)
}

func TestEnroll_MineMidEnrollPreempts(t *testing.T) {
	f, cfg := enrollSetup(t) // EnrollWatch 1h: only preemption can end it
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().State == "discovering" }, 2*time.Second, 2*time.Millisecond)

	f.mu.Lock()
	f.camps = []platform.Campaign{{ID: "c1", Platform: "twitch", Game: "B", Name: "Camp", Status: "active",
		AccountLinked: true, AccountLinkChecked: true,
		Benefits: []platform.DropBenefit{{ID: "d1", CampaignID: "c1", Name: "Drop", RequiredMinutes: 60}}}}
	f.progress = []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 0, Required: 60, Known: true}}
	f.live = []platform.Stream{{Channel: "ch1", ViewerCount: 10}}
	f.mu.Unlock()
	l.Nudge()

	require.Eventually(t, func() bool {
		s := l.Snapshot()
		return s.State == "watching" && s.Channel == "ch1"
	}, 2*time.Second, 2*time.Millisecond)
	assert.Equal(t, []string{"a1", "ch1"}, f.watched())
	f.mu.Lock()
	assert.GreaterOrEqual(t, f.stops, 1, "enroll session stopped")
	f.mu.Unlock()
}

func TestEnroll_DisabledNeverEnrolls(t *testing.T) {
	f, cfg := enrollSetup(t)
	cfg.EnrollDiscovery = false // Android / Kick
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 2*time.Millisecond)
	l.Nudge()
	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, f.watched())
	f.mu.Lock()
	assert.Empty(t, f.gameLookups, "no directory lookups when enroll discovery is off")
	f.mu.Unlock()
}

func TestEnroll_CooldownFollowsConfigNow(t *testing.T) {
	f, cfg := enrollSetup(t)
	cfg.Games = []string{"A"}
	cfg.EnrollWatch = 40 * time.Millisecond
	clk := &fakeClock{t: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	cfg.Now = clk.Now
	l, _ := run(t, cfg)

	require.Eventually(t, func() bool { return len(f.watched()) == 1 }, 2*time.Second, 2*time.Millisecond)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 2*time.Millisecond)
	clk.Advance(EnrollGameCooldown - time.Minute)
	l.Nudge()
	time.Sleep(150 * time.Millisecond)
	assert.Len(t, f.watched(), 1, "A still on cooldown: stay idle, no StartWatch")

	clk.Advance(2 * time.Minute) // past the 6h cooldown
	l.Nudge()
	require.Eventually(t, func() bool { return len(f.watched()) == 2 }, 2*time.Second, 2*time.Millisecond)
	assert.Equal(t, []string{"a1", "a1"}, f.watched())
}

func TestEnroll_SkipsGameWithoutChannelsAndBacksOff(t *testing.T) {
	f, cfg := enrollSetup(t)
	f.gameLive["A"] = nil // nothing live for A
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "b1" }, 2*time.Second, 2*time.Millisecond)
	for i := 0; i < 5; i++ {
		l.Nudge()
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, 1, f.gameLookups["A"], "an empty game is not re-queried while B enrolls")
	assert.Equal(t, []string{"b1"}, f.watching)
}

func TestEnroll_AllDarkWhitelistBacksOffLookups(t *testing.T) {
	f, cfg := enrollSetup(t)
	f.gameLive = map[string][]platform.Stream{} // nothing live anywhere
	clk := &fakeClock{t: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	cfg.Now = clk.Now
	cfg.LiveEvery = time.Hour
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 2*time.Millisecond)
	for i := 0; i < 5; i++ {
		l.Nudge()
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	assert.Equal(t, 1, f.gameLookups["A"], "a dark game is looked up once per LiveEvery, not every iteration")
	assert.Equal(t, 1, f.gameLookups["B"])
	assert.Empty(t, f.watching)
	f.gameLive["B"] = []platform.Stream{{Channel: "b1", ViewerCount: 5, DropsEnabled: true}}
	f.mu.Unlock()

	clk.Advance(time.Hour + time.Minute)
	l.Nudge()
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "b1" }, 2*time.Second, 2*time.Millisecond,
		"after LiveEvery the dark games are retried")
}

func TestEnroll_ChannelDownCoolsItAndPicksNext(t *testing.T) {
	f, cfg := enrollSetup(t)
	f.gameLive["A"] = []platform.Stream{
		{Channel: "a1", ViewerCount: 100, DropsEnabled: true},
		{Channel: "a2", ViewerCount: 50, DropsEnabled: true},
	}
	f.beatFailOn = "a1"
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool {
		s := l.Snapshot()
		return s.State == "discovering" && s.Channel == "a2"
	}, 2*time.Second, 2*time.Millisecond, "a1 went down: cooled, A retried on a2")
	assert.Equal(t, []string{"a1", "a2"}, f.watched())
}
