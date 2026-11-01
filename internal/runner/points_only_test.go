package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// noDrops is a points-claiming Fake that fails the test on any drop
// campaign discovery call while forbid is set.
type noDrops struct {
	*pointsFake
	t      *testing.T
	forbid atomic.Bool
}

func (g *noDrops) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	if g.forbid.Load() {
		g.t.Errorf("points only: ListActiveCampaigns called")
	}
	return g.pointsFake.ListActiveCampaigns(ctx, s)
}

func (g *noDrops) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	if g.forbid.Load() {
		g.t.Errorf("points only: ListEligibleChannels(%s) called", c.ID)
	}
	return g.pointsFake.ListEligibleChannels(ctx, s, c)
}

// liveMode is a Config.PointsMode the test switches.
type liveMode struct{ v atomic.Value }

func newMode(m platform.PointsMode) *liveMode {
	l := &liveMode{}
	l.v.Store(m)
	return l
}

func (l *liveMode) get() platform.PointsMode  { return l.v.Load().(platform.PointsMode) }
func (l *liveMode) set(m platform.PointsMode) { l.v.Store(m) }

// pointsOnlyRunner is a runner on g whose force list is channels, gated on
// the points mode as the roster does. A drop d1 is live on channel "x", so
// any drop-mode pick would watch it.
func pointsOnlyRunner(t *testing.T, g *noDrops, lm *liveMode, channels ...string) *Runner {
	t.Helper()
	g.Mu.Lock()
	g.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	g.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	g.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	g.Streams["c1"] = []platform.Stream{{Channel: "x", ChannelID: "idx", ViewerCount: 10}}
	g.Mu.Unlock()
	r := New(Config{
		AccountID: "acc", Platform: "twitch", Backend: g, Games: rustGames,
		TickInterval: time.Nanosecond,
		PointsMode:   lm.get,
		ForceChannels: func() []string {
			if !lm.get().On() {
				return nil
			}
			return append([]string(nil), channels...)
		},
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return g.LookupChannel(ctx, platform.Session{}, login)
		},
	})
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	return r
}

func newNoDrops(t *testing.T) *noDrops {
	return &noDrops{pointsFake: &pointsFake{Fake: platformtest.NewFake()}, t: t}
}

// A points-only account never scans campaigns, even with a live drop: it
// watches the first live force channel in order, claims points on it, fails
// over to the next live one in order, and sleeps at the 5-minute pace when
// none is live.
func TestPointsOnly_WatchesForceInOrderNeverScans(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "a", false)
	setLive(g.Fake, "b", true)
	setLive(g.Fake, "c", true)
	r := pointsOnlyRunner(t, g, newMode(platform.PointsOnly), "a", "b", "c")

	steps(t, r, 1) // pick
	forceOn(t, r, "b")
	if !waitFor(func() bool { return g.count() == 1 }, time.Second) || g.channels()[0] != "b" {
		t.Fatalf("points claims %v, want one on b", g.channels())
	}
	for range 3 {
		dueRecheck(r)
		steps(t, r, 1) // force tick with its recheck: no drop ends it
		forceOn(t, r, "b")
	}
	r.Rescan()
	steps(t, r, 1) // a Rescan recheck: still no scan
	forceOn(t, r, "b")
	if s := r.Snapshot(); len(s.UpNext) != 0 {
		t.Fatalf("up next %v, want none", s.UpNext)
	}

	// b goes offline: fail over to c, the next live one.
	setLive(g.Fake, "b", false)
	dueRecheck(r)
	steps(t, r, 1)
	forceOn(t, r, "c")
	// a goes live: it ranks first.
	setLive(g.Fake, "a", true)
	dueRecheck(r)
	steps(t, r, 1)
	forceOn(t, r, "a")
	// A failed start falls through to the next live channel.
	r.stopWatch(context.Background())
	g.Mu.Lock()
	g.StartErrs = map[string]error{"a": context.DeadlineExceeded}
	g.Mu.Unlock()
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1)
	forceOn(t, r, "c")

	setLive(g.Fake, "a", false)
	setLive(g.Fake, "c", false)
	dueRecheck(r)
	steps(t, r, 1)
	s := r.Snapshot()
	if s.State != StateSleeping || s.Channel != "" {
		t.Fatalf("%+v", s)
	}
	if left := time.Until(s.NextScanAt); left > recheckInterval || left < recheckInterval-time.Minute {
		t.Fatalf("next check in %v, want about %v", left, recheckInterval)
	}
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1) // a re-pick with nothing live sleeps short too
	if s := r.Snapshot(); s.State != StateSleeping || time.Until(s.NextScanAt) > recheckInterval {
		t.Fatalf("%+v", s)
	}

	for _, c := range []string{"List", "Channels:", "Start:x"} {
		if n := g.CallCount(c); n != 0 {
			t.Fatalf("%d %q calls on a points-only account: %v", n, c, g.Calls)
		}
	}
	for c, want := range map[string]int{"Start:b": 1, "Stop:b": 1, "Start:c": 2, "Stop:c": 2, "Start:a": 2, "Stop:a": 1} {
		if n := g.CallCount(c); n != want {
			t.Fatalf("%s=%d, want %d: %v", c, n, want, g.Calls)
		}
	}
}

// A points-only account needs no games. With no force channel it idles as
// no_channels, not no_games: Run keeps going, and a force list edit wakes it
// to watch.
func TestPointsOnly_NoGamesNoChannels(t *testing.T) {
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "b", true)
	var list atomic.Value
	list.Store([]string(nil))
	r := New(Config{
		AccountID: "acc", Platform: "twitch", Backend: g,
		TickInterval:  time.Hour,
		PointsMode:    fixedPoints(platform.PointsOnly),
		ForceChannels: func() []string { return list.Load().([]string) },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return g.LookupChannel(ctx, platform.Session{}, login)
		},
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateNoChannels })
	if err := r.MineNow("d1"); err != ErrNotQueued {
		t.Fatalf("MineNow err=%v", err)
	}

	list.Store([]string{"b"})
	r.ForceListChanged()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })
	if n := g.CallCount("List"); n != 0 {
		t.Fatalf("%d scans: %v", n, g.Calls)
	}
}

// drops_points to points_only mid drop watch ends the drop watch at once and
// watches the force list; no scan follows.
func TestPointsOnly_LiveFromDropsEndsDropWatch(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	g := newNoDrops(t)
	setLive(g.Fake, "b", true)
	lm := newMode(platform.PointsDropsPoints)
	r := pointsOnlyRunner(t, g, lm, "b")
	r.cfg.TickInterval = time.Hour
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))

	lm.set(platform.PointsOnly)
	g.forbid.Store(true)
	r.SettingsChanged()
	s := waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })
	if s.Current != nil || len(s.UpNext) != 0 {
		t.Fatalf("drop plan kept: %+v", s)
	}
	if !hasCall(g.Fake, "Stop:x") {
		t.Fatalf("drop watch not stopped: %v", g.Calls)
	}
	r.Rescan()
	time.Sleep(30 * time.Millisecond)
	forceOn(t, r, "b")
	for c, want := range map[string]int{"Start:x": 1, "Stop:x": 1, "Start:b": 1, "Stop:b": 0} {
		if n := g.CallCount(c); n != want {
			t.Fatalf("%s=%d, want %d: %v", c, n, want, g.Calls)
		}
	}
}

// points_only to drops_points under a force watch rechecks the plan at once:
// a watchable drop ends the force watch, without waiting the 5-minute
// recheck.
func TestPointsOnly_LiveToDropsLetsDropWin(t *testing.T) {
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "b", true)
	lm := newMode(platform.PointsOnly)
	r := pointsOnlyRunner(t, g, lm, "b")
	r.cfg.TickInterval = time.Hour
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })

	g.forbid.Store(false)
	lm.set(platform.PointsDropsPoints)
	r.SettingsChanged()
	waitSnap(t, r, mining("d1"))
	for c, want := range map[string]int{"Start:b": 1, "Stop:b": 1, "Start:x": 1} {
		if n := g.CallCount(c); n != want {
			t.Fatalf("%s=%d, want %d: %v", c, n, want, g.Calls)
		}
	}
}

// points_only to drops_points with no watchable drop keeps the force watch:
// the recheck scans, finds nothing live, and keeps the channel.
func TestPointsOnly_LiveToDropsKeepsForceWithoutDrop(t *testing.T) {
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "b", true)
	lm := newMode(platform.PointsOnly)
	r := pointsOnlyRunner(t, g, lm, "b")
	r.cfg.TickInterval = time.Hour
	g.Mu.Lock()
	g.Streams["c1"] = nil // the drop has no live channel
	g.Mu.Unlock()
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })

	g.forbid.Store(false)
	lm.set(platform.PointsDropsPoints)
	r.SettingsChanged()
	if !waitFor(func() bool { return g.CallCount("List") > 0 }, time.Second) {
		t.Fatalf("no rescan on leaving points only: %v", g.Calls)
	}
	time.Sleep(20 * time.Millisecond)
	forceOn(t, r, "b")
	if n := g.CallCount("Stop:b"); n != 0 {
		t.Fatalf("force watch restarted: %v", g.Calls)
	}
}

// points_only to off ends the force watch and its points claims.
func TestPointsOnly_LiveToOffEndsForceWatch(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "b", true)
	lm := newMode(platform.PointsOnly)
	r := pointsOnlyRunner(t, g, lm, "b")
	r.cfg.TickInterval = time.Hour
	g.Mu.Lock()
	g.Campaigns = nil // nothing to mine once drops are back
	g.Mu.Unlock()
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })

	g.forbid.Store(false)
	lm.set(platform.PointsOff)
	r.SettingsChanged()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	if !hasCall(g.Fake, "Stop:b") {
		t.Fatalf("force watch not stopped: %v", g.Calls)
	}
	r.mu.Lock()
	task := r.points
	r.mu.Unlock()
	if task != nil {
		t.Fatal("points task kept after points off")
	}
}

// Leaving points only for off while idle with no channels re-picks: with no
// games the runner stops as no_games.
func TestPointsOnly_NoChannelsToOffStops(t *testing.T) {
	g := newNoDrops(t)
	lm := newMode(platform.PointsOnly)
	r := New(Config{
		AccountID: "acc", Platform: "twitch", Backend: g, TickInterval: time.Hour,
		PointsMode:    lm.get,
		ForceChannels: func() []string { return nil },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return g.LookupChannel(ctx, platform.Session{}, login)
		},
	})
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background()) }()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateNoChannels })
	lm.set(platform.PointsOff)
	r.SettingsChanged()
	select {
	case err := <-done:
		if err != nil || r.State() != StateNoGames {
			t.Fatalf("err=%v state=%s", err, r.State())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runner did not stop; state=%s", r.State())
	}
}

// Emptying the force list under a points-only force watch ends it at the next
// recheck and idles as no_channels, not after a 5-minute offline sleep.
// no_channels schedules no scan.
func TestPointsOnly_ListEmptiedEndsForceWatch(t *testing.T) {
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "b", true)
	var list atomic.Value
	list.Store([]string{"b"})
	r := New(Config{
		AccountID: "acc", Platform: "twitch", Backend: g,
		TickInterval:  time.Nanosecond,
		PointsMode:    fixedPoints(platform.PointsOnly),
		ForceChannels: func() []string { return list.Load().([]string) },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return g.LookupChannel(ctx, platform.Session{}, login)
		},
	})
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	steps(t, r, 1)
	forceOn(t, r, "b")

	list.Store([]string(nil))
	dueRecheck(r)
	steps(t, r, 1)
	if s := r.State(); s != StatePicking {
		t.Fatalf("state=%s, want picking", s)
	}
	if !hasCall(g.Fake, "Stop:b") {
		t.Fatalf("force watch not stopped: %v", g.Calls)
	}
	steps(t, r, 1)
	if s := r.Snapshot(); s.State != StateNoChannels || !s.NextScanAt.IsZero() {
		t.Fatalf("state=%s next=%v, want no_channels with no next scan", s.State, s.NextScanAt)
	}
}

// off to points_only mid drop watch ends the drop watch before any points
// claim goes out on the drop channel.
func TestPointsOnly_LiveFromOffNoClaimOnDropChannel(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	g := newNoDrops(t)
	setLive(g.Fake, "b", true)
	lm := newMode(platform.PointsOff)
	r := pointsOnlyRunner(t, g, lm, "b")
	steps(t, r, 2) // pick, start
	if s := r.Snapshot(); s.State != StateWatching || s.Channel != "x" {
		t.Fatalf("%+v", s)
	}

	lm.set(platform.PointsOnly)
	if !r.applyLive(context.Background()) {
		t.Fatal("applyLive kept the drop watch")
	}
	if s := r.State(); s != StatePicking {
		t.Fatalf("state=%s", s)
	}
	if got := g.channels(); len(got) != 0 {
		t.Fatalf("points claims %v, want none", got)
	}
}

// A Mine now still pending from drops mode is dropped once points only
// picks: it never restarts the force watch.
func TestPointsOnly_PendingMineNowDropped(t *testing.T) {
	g := newNoDrops(t)
	g.forbid.Store(true)
	setLive(g.Fake, "b", true)
	r := pointsOnlyRunner(t, g, newMode(platform.PointsOnly), "b")
	r.switchNow.Store(true)
	steps(t, r, 1)
	forceOn(t, r, "b")
	if r.switchNow.Load() {
		t.Fatal("pending Mine now kept after the points-only pick")
	}
	// One that lands after the pick is not taken either.
	r.switchNow.Store(true)
	if r.takeSwitch() {
		t.Fatal("takeSwitch took a Mine now in points only")
	}
	if n := g.CallCount("Stop:b"); n != 0 {
		t.Fatalf("force watch restarted: %v", g.Calls)
	}
}

// A points change that lands while pick still reads the old mode leaves the
// runner stopped as no_games with its signal unread. Run picks again once the
// mode gives it work, instead of returning.
func TestPointsOnly_NoGamesRacePicksAgain(t *testing.T) {
	g := newNoDrops(t)
	lm := newMode(platform.PointsOff)
	var flip atomic.Bool
	r := New(Config{
		AccountID: "acc", Platform: "twitch", Backend: g, TickInterval: time.Hour,
		PointsMode: lm.get,
		// The save lands just after pick reads the mode: pick reads the
		// force list next.
		ForceChannels: func() []string {
			if flip.Swap(false) {
				lm.set(platform.PointsOnly)
			}
			return nil
		},
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return g.LookupChannel(ctx, platform.Session{}, login)
		},
	})
	flip.Store(true)
	r.SettingsChanged()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateNoChannels })
	select {
	case err := <-done:
		t.Fatalf("Run returned %v as %s", err, r.State())
	case <-time.After(20 * time.Millisecond):
	}
}
