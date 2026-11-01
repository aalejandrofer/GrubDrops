package runner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// livePoints is a Config.PointsMode the test flips.
type livePoints struct{ on atomic.Bool }

func (p *livePoints) mode() platform.PointsMode {
	if p.on.Load() {
		return platform.PointsDropsPoints
	}
	return platform.PointsOff
}

// liveSleep is a Config.SleepUntil like the roster's: a window that holds
// until wake while on, ignored while skip is set.
type liveSleep struct {
	mu   sync.Mutex
	on   bool
	skip bool
	wake time.Time
}

func (s *liveSleep) set(on, skip bool, wake time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on, s.skip, s.wake = on, skip, wake
}

func (s *liveSleep) fn(now time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.on && !s.skip && now.Before(s.wake) {
		return s.wake, true
	}
	return time.Time{}, false
}

// Toggling skip sleep outside the window, or moving a window that does not
// cover now, keeps the watch: no stop, no new start, no early beacon.
func TestLive_SkipSleepToggleKeepsWatch(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	sl := &liveSleep{}
	r := newRunner(f, func(c *Config) {
		c.TickInterval = time.Hour
		c.SleepUntil = sl.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))
	if !waitFor(func() bool { return f.CallCount("Heartbeat:a") == 1 }, time.Second) {
		t.Fatalf("calls=%v", f.Calls)
	}

	for _, skip := range []bool{true, false, true} {
		sl.set(false, skip, time.Time{})
		r.SettingsChanged()
		time.Sleep(20 * time.Millisecond)
	}
	sl.set(true, false, time.Now().Add(-time.Minute)) // a window that ended
	r.SettingsChanged()
	time.Sleep(20 * time.Millisecond)

	waitSnap(t, r, mining("d1"))
	if n := f.CallCount("Start:"); n != 1 {
		t.Fatalf("starts=%d, want 1: %v", n, f.Calls)
	}
	if n := f.CallCount("Stop:"); n != 0 {
		t.Fatalf("stops=%d, want 0: %v", n, f.Calls)
	}
	if n := f.CallCount("Heartbeat:a"); n != 1 {
		t.Fatalf("heartbeats=%d, want 1", n)
	}
}

// Skip sleep turned off inside an active window puts a watching runner to
// sleep at once, not at its next beat. Turned on again, it wakes at once.
func TestLive_SkipSleepOffInWindowSleeps(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	sl := &liveSleep{}
	sl.set(true, true, time.Now().Add(time.Hour)) // window holds, account skips it
	r := newRunner(f, func(c *Config) {
		c.TickInterval = time.Hour
		c.SleepUntil = sl.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))

	sl.set(true, false, time.Now().Add(time.Hour))
	r.SettingsChanged()
	waitSnap(t, r, asleep)
	if !hasCall(f, "Stop:a") {
		t.Fatalf("watch not stopped: %v", f.Calls)
	}

	sl.set(true, true, time.Now().Add(time.Hour))
	r.SettingsChanged()
	waitSnap(t, r, mining("d1"))
	if n := f.CallCount("Start:a"); n != 2 {
		t.Fatalf("starts=%d, want 2", n)
	}
}

// A sleep window saved to cover now stops a watch at once.
func TestLive_SleepWindowStartsNow(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	sl := &liveSleep{}
	r := newRunner(f, func(c *Config) {
		c.TickInterval = time.Hour
		c.SleepUntil = sl.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))
	sl.set(true, false, time.Now().Add(time.Hour))
	r.SettingsChanged()
	waitSnap(t, r, asleep)
}

// Points turned on mid-watch start claims on the current watch; turned off
// they stop. The watch itself never restarts.
func TestLive_PointsToggleMidWatch(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	p := &pointsFake{Fake: platformtest.NewFake()}
	lp := &livePoints{}
	r := pointsRunner(t, p, platform.PointsOff, func(c *Config) {
		c.TickInterval = time.Hour
		c.PointsMode = lp.mode
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))
	time.Sleep(20 * time.Millisecond)
	if n := p.count(); n != 0 {
		t.Fatalf("%d claims with points off", n)
	}

	lp.on.Store(true)
	r.SettingsChanged()
	if !waitFor(func() bool { return p.count() == 1 }, time.Second) {
		t.Fatalf("claims=%d, want a claim once points turn on", p.count())
	}
	if got := p.channels(); got[0] != "a" {
		t.Fatalf("claimed on %v, want a", got)
	}
	r.mu.Lock()
	task := r.points
	r.mu.Unlock()
	if task == nil {
		t.Fatal("no points task after points on")
	}

	lp.on.Store(false)
	r.SettingsChanged()
	if !waitFor(func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.points == nil
	}, time.Second) {
		t.Fatal("points task kept after points off")
	}
	select {
	case <-task.done:
	default:
		t.Fatal("points goroutine still running after points off")
	}

	if s := r.State(); s != StateWatching {
		t.Fatalf("state=%s", s)
	}
	if n := p.CallCount("Start:"); n != 1 {
		t.Fatalf("starts=%d, want 1", n)
	}
	if n := p.CallCount("Stop:"); n != 0 {
		t.Fatalf("stops=%d, want 0", n)
	}
}

// pointsForce gates the force list on lp, as the roster does.
func pointsForce(f *platformtest.Fake, lp *livePoints, channels ...string) func(*Config) {
	return func(c *Config) {
		forceCfg(f, channels...)(c)
		c.TickInterval = time.Hour
		c.PointsMode = lp.mode
		c.ForceChannels = func() []string {
			if !lp.on.Load() {
				return nil
			}
			return append([]string(nil), channels...)
		}
	}
}

// Points turned off while force watching end the force watch: the account
// no longer force watches.
func TestLive_PointsOffEndsForceWatch(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	lp := &livePoints{}
	lp.on.Store(true)
	r := newRunner(f, pointsForce(f, lp, "b"))
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })

	lp.on.Store(false)
	r.SettingsChanged()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	if !hasCall(f, "Stop:b") {
		t.Fatalf("force watch not stopped: %v", f.Calls)
	}
}

// Points turned on while idle let the runner pick a force channel at once,
// without a rebuild.
func TestLive_PointsOnIdlePicksForce(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	lp := &livePoints{}
	r := newRunner(f, pointsForce(f, lp, "b"))
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	if hasCall(f, "Start:b") {
		t.Fatalf("force watched with points off: %v", f.Calls)
	}

	lp.on.Store(true)
	r.SettingsChanged()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })
}

// A settings nudge to an idle runner that changes nothing leaves it idle:
// no re-pick, no scan.
func TestLive_IdleNudgeNoChange(t *testing.T) {
	f := platformtest.NewFake()
	lp := &livePoints{}
	r := newRunner(f, pointsForce(f, lp, "b"))
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	lists := f.CallCount("List")
	r.SettingsChanged()
	time.Sleep(30 * time.Millisecond)
	if n := f.CallCount("List"); n != lists {
		t.Fatalf("scans %d -> %d on a no-op nudge", lists, n)
	}
}

// Pool.RescanAll keeps a force watch while no drop is watchable, and ends it
// once one is.
func TestPool_RescanAllKeepsForceUnlessDrop(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	p := NewPool(nil)
	var r *Runner
	b := func() Entry {
		r = newRunner(f, func(c *Config) {
			forceCfg(f, "b")(c)
			c.TickInterval = time.Hour
		})
		return Entry{ID: "acc", Unit: r}
	}
	if err := p.Reload(context.Background(), []Builder{b}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })

	lists := f.CallCount("List")
	p.RescanAll()
	if !waitFor(func() bool { return f.CallCount("List") > lists }, 2*time.Second) {
		t.Fatalf("no scan after RescanAll: %v", f.Calls)
	}
	time.Sleep(20 * time.Millisecond)
	forceOn(t, r, "b")
	if hasCall(f, "Stop:b") || f.CallCount("Start:") != 1 {
		t.Fatalf("RescanAll restarted the force watch: %v", f.Calls)
	}

	f.Mu.Lock()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{{Channel: "x", ViewerCount: 3}}
	f.Mu.Unlock()
	p.RescanAll()
	waitSnap(t, r, mining("d1"))
	if !hasCall(f, "Stop:b") {
		t.Fatalf("force watch kept over a drop: %v", f.Calls)
	}
}

// Marking the watched drop collected moves the runner to the next one;
// marking a queued one keeps the watch.
func TestLive_CollectedMovesOnOnlyFromWatchedDrop(t *testing.T) {
	f := platformtest.NewFake()
	threeLive(f)
	cl := &fakeClaims{ids: map[string]bool{}, marked: map[string]bool{}}
	p, r := minePool(t, f, func(c *Config) { c.Claims = cl })
	waitSnap(t, r, mining("d1"))

	mark := func(id string) {
		cl.mu.Lock()
		cl.ids[id], cl.marked[id] = true, true
		cl.mu.Unlock()
	}
	mark("d2")
	p.MarkCollected("acc", "d2")
	time.Sleep(30 * time.Millisecond)
	waitSnap(t, r, mining("d1"))
	if hasCall(f, "Stop:s1") {
		t.Fatalf("watch stopped for a queued drop: %v", f.Calls)
	}

	mark("d1")
	p.MarkCollected("acc", "d1")
	waitSnap(t, r, mining("d3"))
	if !hasCall(f, "Stop:s1") {
		t.Fatalf("watched drop not stopped: %v", f.Calls)
	}
}

// Marking a skipped drop uncollected forgets the runner's skip and queues
// it again without a rebuild.
func TestLive_UncollectedRequeues(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	delete(f.Progress, "d1") // untracked, so the scan's self-heal keeps the skip
	p := NewPool(nil)
	builds := 0
	var r *Runner
	b := func() Entry {
		builds++
		r = newRunner(f, func(c *Config) { c.TickInterval = time.Hour })
		r.skipped["d1"] = true // as a vanish or ghost skip leaves it
		return Entry{ID: "acc", Unit: r}
	}
	if err := p.Reload(context.Background(), []Builder{b}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })

	p.MarkUncollected("acc", "d1")
	waitSnap(t, r, mining("d1"))
	if builds != 1 {
		t.Fatalf("builds=%d, want 1", builds)
	}
}

// Points turned off inside the sleep window and on again once the runner
// idles still wake it to force watch: applyLive tracks the points mode on
// the window's path too, so the later on edge is seen.
func TestLive_PointsEdgeTrackedThroughSleepWindow(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	lp := &livePoints{}
	lp.on.Store(true)
	sl := &liveSleep{}
	sl.set(true, false, time.Now().Add(time.Hour))
	r := newRunner(f, func(c *Config) {
		pointsForce(f, lp, "b")(c)
		c.SleepUntil = sl.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, asleep)

	lp.on.Store(false)
	r.SettingsChanged()
	time.Sleep(20 * time.Millisecond)
	sl.set(false, false, time.Time{})
	r.SettingsChanged()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })

	lp.on.Store(true)
	r.SettingsChanged()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching && s.Channel == "b" })
}

// Marking a drop uncollected clears its stored skip as well as the one in
// memory, so a rebuild does not load it again and re-skip the drop mid
// progress. Its link notice goes too.
func TestLive_UncollectedClearsStoredSkip(t *testing.T) {
	f := platformtest.NewFake()
	sk := &fakeSkips{skips: map[string]bool{"d1": true}}
	r := newRunner(f, func(c *Config) { c.Skips = sk })
	r.mu.Lock()
	r.linkNotified = map[string]bool{"d1": true}
	r.mu.Unlock()

	r.Uncollected("d1")
	sk.mu.Lock()
	cleared := append([]string(nil), sk.cleared...)
	sk.mu.Unlock()
	if len(cleared) != 1 || cleared[0] != "d1" {
		t.Fatalf("store cleared %v, want [d1]", cleared)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.skipped["d1"] || r.linkNotified["d1"] {
		t.Fatalf("skipped=%v linkNotified=%v", r.skipped, r.linkNotified)
	}
}
