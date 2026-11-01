package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// sleepSwitch is a scripted Config.SleepUntil: asleep while on and before
// wake.
type sleepSwitch struct {
	mu   sync.Mutex
	on   bool
	wake time.Time
}

func (s *sleepSwitch) set(on bool, wake time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on, s.wake = on, wake
}

func (s *sleepSwitch) fn(now time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.on && now.Before(s.wake) {
		return s.wake, true
	}
	return time.Time{}, false
}

func liveDrop(f *platformtest.Fake) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 1}}
}

func asleep(s Snapshot) bool { return s.State == StateSleepWindow }

// The window stops a plan watch within a beat, shows the wake time, and the
// runner resumes watching by itself once the window ends.
func TestSleepWindow_StopsPlanWatchAndResumes(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	sw := &sleepSwitch{}
	r := newRunner(f, func(c *Config) {
		c.TickInterval = 5 * time.Millisecond
		c.SleepUntil = sw.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))

	wake := time.Now().Add(300 * time.Millisecond)
	sw.set(true, wake)
	s := waitSnap(t, r, asleep)
	if !hasCall(f, "Stop:a") {
		t.Fatalf("watch not stopped: %v", f.Calls)
	}
	if !s.SleepUntil.Equal(wake) || !s.NextScanAt.Equal(wake) {
		t.Fatalf("sleep_until=%v next_scan=%v, want %v", s.SleepUntil, s.NextScanAt, wake)
	}
	if s.Current != nil || s.Channel != "" {
		t.Fatalf("still showing a watch: %+v", s)
	}
	starts := f.CallCount("Start:")
	beats := f.CallCount("Heartbeat:")
	time.Sleep(100 * time.Millisecond)
	if f.CallCount("Heartbeat:") != beats {
		t.Fatal("beaconed during the sleep window")
	}

	// No Rescan or switch: the wake timer alone resumes the watch.
	s = waitSnap(t, r, mining("d1"))
	if !s.SleepUntil.IsZero() {
		t.Fatalf("sleep_until=%v after wake", s.SleepUntil)
	}
	if f.CallCount("Start:") <= starts {
		t.Fatalf("no new watch after wake: %v", f.Calls)
	}
	if time.Now().Before(wake) {
		t.Fatal("resumed before the wake time")
	}
}

func TestSleepWindow_StopsForceWatch(t *testing.T) {
	f := platformtest.NewFake()
	setLive(f, "b", true)
	sw := &sleepSwitch{}
	r := newRunner(f, func(c *Config) {
		forceCfg(f, "b")(c)
		c.TickInterval = 5 * time.Millisecond
		c.SleepUntil = sw.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateForceWatching })

	sw.set(true, time.Now().Add(time.Hour))
	waitSnap(t, r, asleep)
	if !hasCall(f, "Stop:b") {
		t.Fatalf("force watch not stopped: %v", f.Calls)
	}
}

// A runner already idle for its long scan wait enters the window at the
// next poll, without a scan.
func TestSleepWindow_IdleRunnerEntersOnPoll(t *testing.T) {
	old := sleepPoll
	sleepPoll = 10 * time.Millisecond
	t.Cleanup(func() { sleepPoll = old })

	f := platformtest.NewFake() // no campaigns: sleeping for idleRecheck
	sw := &sleepSwitch{}
	r := newRunner(f, func(c *Config) {
		c.TickInterval = time.Millisecond
		c.SleepUntil = sw.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	lists := f.CallCount("List")

	sw.set(true, time.Now().Add(time.Hour))
	waitSnap(t, r, asleep)
	if f.CallCount("List") != lists {
		t.Fatalf("scanned on entering the window: %v", f.Calls)
	}
}

// A Rescan during the window (a settings save) does not scan or watch.
func TestSleepWindow_RescanStaysAsleep(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	sw := &sleepSwitch{}
	sw.set(true, time.Now().Add(time.Hour))
	var mu sync.Mutex
	var states []State
	r := newRunner(f, func(c *Config) {
		c.TickInterval = time.Millisecond
		c.SleepUntil = sw.fn
		c.OnChange = func(s Snapshot) {
			mu.Lock()
			states = append(states, s.State)
			mu.Unlock()
		}
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, asleep)
	for range 5 {
		r.Rescan()
		time.Sleep(10 * time.Millisecond)
	}
	if f.CallCount("List") != 0 || f.CallCount("Start:") != 0 {
		t.Fatalf("worked during the window: %v", f.Calls)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range states {
		if s != StateSleepWindow {
			t.Fatalf("states=%v, want only %s", states, StateSleepWindow)
		}
	}
}

// Ending the window early (a settings save) and rescanning resumes at once.
func TestSleepWindow_DisableAndRescanResumes(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	sw := &sleepSwitch{}
	sw.set(true, time.Now().Add(time.Hour))
	r := newRunner(f, func(c *Config) {
		c.TickInterval = 5 * time.Millisecond
		c.SleepUntil = sw.fn
	})
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, asleep)
	sw.set(false, time.Time{})
	r.Rescan()
	waitSnap(t, r, mining("d1"))
}

// Without a SleepUntil (feature off, or the account skips the window) the
// runner watches as before.
func TestSleepWindow_NilNeverSleeps(t *testing.T) {
	f := platformtest.NewFake()
	liveDrop(f)
	r := newRunner(f, func(c *Config) { c.TickInterval = 5 * time.Millisecond })
	stop := runUntil(t, r)
	defer stop()
	waitSnap(t, r, mining("d1"))
	time.Sleep(50 * time.Millisecond)
	if s := r.Snapshot(); s.State != StateWatching {
		t.Fatalf("state=%s", s.State)
	}
}

func TestSleepWindow_StepIsIdle(t *testing.T) {
	r := newRunner(platformtest.NewFake(), nil)
	r.mu.Lock()
	r.state = StateSleepWindow
	r.mu.Unlock()
	if err := r.step(context.Background()); !errors.Is(err, errIdle) {
		t.Fatalf("step=%v", err)
	}
}

func TestSleepWindow_MineNowRefused(t *testing.T) {
	f := platformtest.NewFake()
	sw := &sleepSwitch{}
	sw.set(true, time.Now().Add(time.Hour))
	r := newRunner(f, func(c *Config) { c.SleepUntil = sw.fn })
	c1 := camp("c1", "Rust", "d1")
	r.mu.Lock()
	r.queue = []plan.Target{{Campaign: c1, Benefit: c1.Benefits[0]}}
	r.state = StateSleepWindow
	r.mu.Unlock()
	if err := r.MineNow("d1"); !errors.Is(err, ErrSleepWindow) {
		t.Fatalf("MineNow=%v, want ErrSleepWindow", err)
	}
	if r.switchNow.Load() {
		t.Fatal("switch requested during the window")
	}

	// The pool says why even with nothing queued, rather than not_queued.
	r.mu.Lock()
	r.queue = nil
	r.mu.Unlock()
	p := NewPool(nil)
	defer p.Stop()
	if err := p.Reload(context.Background(), []Builder{func() Entry { return Entry{ID: "acc", Unit: r} }}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.MineNow("acc"); !errors.Is(err, ErrSleepWindow) {
		t.Fatalf("pool MineNow=%v, want ErrSleepWindow", err)
	}

	sw.set(false, time.Time{})
	if err := r.MineNow("d1"); errors.Is(err, ErrSleepWindow) {
		t.Fatal("refused outside the window")
	}
}

// A drop that completes as the sleep window starts is claimed first: the
// gate leaves StateClaiming to the claim step, then holds the window on the
// next loop.
func TestSleepWindow_PendingClaimRunsFirst(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, _ := claimSetup(f, 2)
	sw := &sleepSwitch{}
	r.cfg.SleepUntil = sw.fn
	toClaiming(t, r)
	sw.set(true, time.Now().Add(time.Hour))

	ctx := context.Background()
	if r.sleepGate(ctx) {
		t.Fatal("the gate slept over a pending claim")
	}
	if s := r.State(); s != StateClaiming {
		t.Fatalf("state=%s, want claiming", s)
	}
	if err := r.step(ctx); err != nil {
		t.Fatal(err)
	}
	if c := f.CallCount("Claim:d1"); c != 1 {
		t.Fatalf("claims=%d, want 1", c)
	}
	if !r.sleepGate(ctx) || r.State() != StateSleepWindow {
		t.Fatalf("not asleep after the claim: state=%s", r.State())
	}
}
