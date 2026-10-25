package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

func steps(t *testing.T, r *Runner, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := r.step(context.Background()); err != nil {
			t.Fatalf("step %d (%s): %v", i, r.State(), err)
		}
	}
}

// watching returns a runner already in StateWatching on channel "a" for d1.
func watching(t *testing.T, f *platformtest.Fake, mod func(*Config)) *Runner {
	t.Helper()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	r := newRunner(f, mod)
	steps(t, r, 2)
	if r.State() != StateWatching {
		t.Fatalf("state=%s", r.State())
	}
	return r
}

func hasCall(f *platformtest.Fake, c string) bool {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for _, x := range f.Calls {
		if x == c {
			return true
		}
	}
	return false
}

func TestWatch_PicksHighestViewers(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ViewerCount: 10}, {Channel: "b", ViewerCount: 50}}
	r := newRunner(f, nil)
	steps(t, r, 2)
	if !hasCall(f, "Start:b") || r.State() != StateWatching {
		t.Fatalf("calls=%v state=%s", f.Calls, r.State())
	}
}

func TestWatch_NoLiveStreamTriesNextCampaign(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2")}
	f.Streams["c2"] = []platform.Stream{{Channel: "x", ViewerCount: 1}}
	r := newRunner(f, nil)
	steps(t, r, 1)
	first := r.Snapshot().Current.Campaign.ID
	if first != "c1" {
		t.Fatalf("first=%s", first)
	}
	steps(t, r, 1) // start: no stream for c1, reuses the queue and starts c2
	s := r.Snapshot()
	if s.Current.Campaign.ID != "c2" || s.State != StateWatching {
		t.Fatalf("%+v", s)
	}
}

func TestWatch_ProbeSkipsChannelWithoutDrop(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{
		{Channel: "a", ChannelID: "ida", ViewerCount: 50},
		{Channel: "b", ChannelID: "idb", ViewerCount: 10},
	}
	pf := &platformtest.ProbeFake{Fake: f, Available: map[string]map[string]struct{}{
		"ida": {"other": {}},
		"idb": {"d1": {}},
	}}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: pf, Games: rustGames})
	steps(t, r, 2)
	if !hasCall(f, "Start:b") || hasCall(f, "Start:a") {
		t.Fatalf("calls=%v", f.Calls)
	}
}

func TestWatch_BeaconEveryTick(t *testing.T) {
	f := platformtest.NewFake()
	r := watching(t, f, nil)
	steps(t, r, 3)
	if n := f.CallCount("Heartbeat:"); n != 3 {
		t.Fatalf("heartbeats=%d", n)
	}
}

func TestWatch_CompleteGoesToClaiming(t *testing.T) {
	f := platformtest.NewFake()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 59, InstanceID: "i1"}
	r := watching(t, f, nil)
	steps(t, r, 1)
	s := r.Snapshot()
	if s.State != StateClaiming || s.Current.Benefit.InstanceID != "i1" {
		t.Fatalf("%+v", s)
	}
}

func TestWatch_LiveCheckSwapsOfflineChannel(t *testing.T) {
	f := platformtest.NewFake()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	r := watching(t, f, func(c *Config) { c.TickInterval = liveCheckEvery / 2 })
	steps(t, r, 1) // tick 1: no live check
	if r.State() != StateWatching {
		t.Fatal(r.State())
	}
	f.Mu.Lock()
	f.Streams["c1"] = nil
	f.Mu.Unlock()
	steps(t, r, 1) // tick 2: live check
	if r.State() != StateStarting || !hasCall(f, "Stop:a") {
		t.Fatalf("state=%s calls=%v", r.State(), f.Calls)
	}
}

func TestWatch_FreezeRotatesAndCoolsDownChannel(t *testing.T) {
	f := platformtest.NewFake()
	f.OnHeartbeat = func(*platformtest.Fake) {}
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 5}
	r := watching(t, f, nil)
	// poll 1 sees 5 > lastMin 0 (advance); polls 2-6 are flat.
	steps(t, r, 5)
	if r.State() != StateWatching {
		t.Fatalf("rotated early: %s", r.State())
	}
	steps(t, r, 1)
	if r.State() != StateStarting || !hasCall(f, "Stop:a") {
		t.Fatalf("state=%s calls=%v", r.State(), f.Calls)
	}
	if r.Snapshot().Current == nil {
		t.Fatal("target must be kept")
	}
	steps(t, r, 1) // start: only channel a is on cooldown
	if r.State() != StateSleeping || f.CallCount("Start:") != 1 {
		t.Fatalf("state=%s starts=%d", r.State(), f.CallCount("Start:"))
	}
}

func TestWatch_FreezeNotTriggeredWhenAdvancing(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 1000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ViewerCount: 1}}
	r := newRunner(f, nil)
	steps(t, r, 12)
	if f.CallCount("Stop:") != 0 || r.State() != StateWatching {
		t.Fatalf("state=%s calls=%v", r.State(), f.Calls)
	}
}

func TestWatch_VanishSkipsBenefit(t *testing.T) {
	f := platformtest.NewFake()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	sk := &fakeSkips{}
	r := watching(t, f, func(c *Config) { c.Skips = sk })
	steps(t, r, 2)
	f.Mu.Lock()
	delete(f.Progress, "d1")
	f.Mu.Unlock()
	steps(t, r, 2)
	if r.State() != StateWatching {
		t.Fatalf("early: %s", r.State())
	}
	steps(t, r, 1)
	if r.State() != StatePicking || len(sk.recorded) != 1 || sk.recorded[0] != "d1" || !hasCall(f, "Stop:a") {
		t.Fatalf("state=%s rec=%v", r.State(), sk.recorded)
	}
	if !r.skipped["d1"] {
		t.Fatal("not skipped in memory")
	}
}

func TestWatch_NeverEnrolledSkipsAfterSix(t *testing.T) {
	f := platformtest.NewFake()
	sk := &fakeSkips{}
	r := watching(t, f, func(c *Config) { c.Skips = sk })
	steps(t, r, 5)
	if r.State() != StateWatching || len(sk.recorded) != 0 {
		t.Fatalf("early: %s %v", r.State(), sk.recorded)
	}
	steps(t, r, 1)
	if r.State() != StatePicking || len(sk.recorded) != 1 {
		t.Fatalf("state=%s rec=%v", r.State(), sk.recorded)
	}
}

func TestWatch_HeartbeatErrorStopsWatchOnRetry(t *testing.T) {
	old := stepErrBackoff
	stepErrBackoff = time.Millisecond
	t.Cleanup(func() { stepErrBackoff = old })

	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ViewerCount: 1}}
	r := newRunner(f, func(c *Config) { c.TickInterval = time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for r.State() != StateWatching && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	f.Mu.Lock()
	f.HeartbeatErr = context.DeadlineExceeded
	f.ListErr = context.DeadlineExceeded // keep pick failing so start's stale-handle stop can't mask the Run branch
	f.Mu.Unlock()
	for !hasCall(f, "Stop:a") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stopped := hasCall(f, "Stop:a")
	// Cancel after the check: exit() also stops, but only if a handle is left.
	cancel()
	<-done
	if !stopped {
		t.Fatal("Run error branch did not stop the watch")
	}
}

func TestRunner_CancelStopsWatch(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ViewerCount: 1}}
	r := newRunner(f, func(c *Config) { c.TickInterval = time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for r.State() != StateWatching && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if !hasCall(f, "Stop:a") {
		t.Fatalf("calls=%v", strings.Join(f.Calls, ","))
	}
}

func TestShouldNotifyProgress_MilestoneSteps(t *testing.T) {
	const req = 100
	r := &Runner{lastMilestone: -1}
	r.cfg.ProgressNotifyStepPct = 25
	for _, c := range []struct {
		cur  int
		want bool
	}{{0, true}, {0, false}, {10, false}, {25, true}, {30, false}, {50, true}, {75, true}, {99, false}, {100, true}, {120, false}} {
		if got := r.shouldNotifyProgress(c.cur, req); got != c.want {
			t.Fatalf("cur=%d got %v want %v", c.cur, got, c.want)
		}
	}
}

func TestShouldNotifyProgress_StepZeroDisables(t *testing.T) {
	r := &Runner{lastMilestone: -1}
	for _, m := range []int{0, 60, 120} {
		if r.shouldNotifyProgress(m, 120) {
			t.Fatalf("fired at %d", m)
		}
	}
}

func TestWatch_PollNotifiesOnChange(t *testing.T) {
	f := platformtest.NewFake() // Kick-style: no PubSub, polls are the only progress source
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	var mins []int
	r := watching(t, f, func(c *Config) {
		c.OnChange = func(s Snapshot) {
			if s.State == StateWatching {
				mins = append(mins, s.MinutesWatched)
			}
		}
	})
	mins = nil
	steps(t, r, 3)
	if len(mins) < 3 || mins[0] != 1 || mins[1] != 2 || mins[2] != 3 {
		t.Fatalf("OnChange minutes=%v, want 1,2,3", mins)
	}
}
