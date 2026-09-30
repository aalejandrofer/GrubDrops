package loop

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// recordCapture is a slog.Handler that stores every record it sees, so
// tests can assert on a message's level and attrs together (a plain
// message->level map can't tell two records with the same message but
// different attrs apart, e.g. two "pipeline enroll discovery" lines with
// different "reason" values).
type recordCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordCapture) WithGroup(string) slog.Handler      { return h }

func recordAttr(r slog.Record, key string) (string, bool) {
	var val string
	var ok bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val, ok = a.Value.String(), true
			return false
		}
		return true
	})
	return val, ok
}

// has reports whether some captured record matches msg, level, and every
// key/value in attrs (extra attrs on the record are ignored).
func (h *recordCapture) has(msg string, level slog.Level, attrs map[string]string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message != msg || r.Level != level {
			continue
		}
		ok := true
		for k, want := range attrs {
			got, present := recordAttr(r, k)
			if !present || got != want {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

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

// TestEnroll_ChannelLookupErrorLogsWarn: a directory lookup failure is the
// only signal an operator gets that enroll discovery can't see a game's
// channels. It must log at WARN, not DEBUG, and (since maybeEnroll backs
// the game off via enrollEmpty on any failed lookup, the same as a
// legitimately-empty result) it must not fire again on a nudge that lands
// inside that backoff window.
func TestEnroll_ChannelLookupErrorLogsWarn(t *testing.T) {
	f, cfg := enrollSetup(t)
	cfg.Games = []string{"A"}
	f.gameErr = map[string]error{"A": errors.New("directory unavailable")}

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	cap := &recordCapture{}
	slog.SetDefault(slog.New(cap))

	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 2*time.Millisecond)
	for i := 0; i < 5; i++ {
		l.Nudge()
		time.Sleep(10 * time.Millisecond)
	}

	assert.True(t, cap.has("pipeline enroll: list channels failed", slog.LevelWarn, map[string]string{"game": "A"}),
		"a directory lookup error must log at WARN")
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, 1, f.gameLookups["A"], "the failed lookup backs the game off like an empty one: not retried every nudge")
}

// TestRun_ClearsEnrollOnExit: Run's defer must clear l.enroll itself, not
// just its timer. Without that, a re-entered Run on the same Loop would
// inherit a stale enrollRun with no timer left to end it: maybeEnroll's
// "enroll != nil" guard would block forever and updateSnapshot would
// report "discovering" forever, with nothing left running to finish it.
func TestRun_ClearsEnrollOnExit(t *testing.T) {
	_, cfg := enrollSetup(t)
	cfg.Games = []string{"A"}
	cfg.EnrollWatch = time.Hour // outlives this test; only ctx cancel ends it
	l, err := New(cfg)
	require.NoError(t, err)

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan struct{})
	go func() { _ = l.Run(ctx1); close(done1) }()
	require.Eventually(t, func() bool { return l.Snapshot().State == "discovering" }, 2*time.Second, 2*time.Millisecond)
	cancel1()
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("first Run did not exit")
	}
	assert.Nil(t, l.enroll, "Run's defer must clear the enroll run on exit, not just its timer")
	assert.Nil(t, l.enrollT)
	assert.Nil(t, l.enrollC)

	// Re-entering Run must be able to enroll again (a stale enroll would
	// wedge maybeEnroll's guard and Snapshot would never move off whatever
	// state it last held).
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := make(chan struct{})
	go func() { _ = l.Run(ctx2); close(done2) }()
	require.Eventually(t, func() bool {
		s := l.Snapshot()
		return s.State == "discovering" && s.Channel == "a1"
	}, 2*time.Second, 2*time.Millisecond, "re-entered Run must be able to start a fresh enroll watch")
	cancel2()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("second Run did not exit")
	}
}

// TestFinishEnroll_LogsDoneEvenWhenReconcileAuthBlocks: the post-enroll
// reconcile can trip the integrity wall (authBlocked). The phase=stop
// reason=done line (campaigns_found/new_drops from reconcile alone;
// refreshLive adds no rows) must still be logged, so an enroll watch that
// ends in an auth block doesn't vanish from the logs. Calls finishEnroll
// directly (no Run goroutine) to keep the integrity failure isolated to
// the reconcile finishEnroll triggers, not the loop's startup reconcile.
func TestFinishEnroll_LogsDoneEvenWhenReconcileAuthBlocks(t *testing.T) {
	f, cfg := enrollSetup(t)
	cfg.Backend = integrityBackend{f} // ListActiveCampaigns always integrity-blocks

	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	l.enroll = &enrollRun{game: "A", idx: 0, stream: platform.Stream{Channel: "a1", ViewerCount: 100}, started: l.cfg.Now()}

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	cap := &recordCapture{}
	slog.SetDefault(slog.New(cap))

	l.finishEnroll(ctx)

	assert.True(t, l.authBlocked, "reconcile hit the integrity wall")
	assert.Nil(t, l.enroll)
	assert.True(t, cap.has("pipeline enroll discovery", slog.LevelInfo, map[string]string{
		"phase": "stop", "reason": "done", "game": "A", "channel": "a1",
	}), "phase=stop reason=done must log even though the post-enroll reconcile auth-blocked")
}
