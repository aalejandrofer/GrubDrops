package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// pointsFake is a Fake that can claim channel points. It counts calls per
// channel; block makes each call wait for its context, and records why it
// ended.
type pointsFake struct {
	*platformtest.Fake
	mu     sync.Mutex
	calls  []string
	ended  []error
	block  bool
	result platform.PointsClaim
}

func (p *pointsFake) ClaimChannelPoints(ctx context.Context, _ platform.Session, channel string) (platform.PointsClaim, error) {
	p.mu.Lock()
	p.calls = append(p.calls, channel)
	block, res := p.block, p.result
	p.mu.Unlock()
	if block {
		<-ctx.Done()
		p.mu.Lock()
		p.ended = append(p.ended, ctx.Err())
		p.mu.Unlock()
		return platform.PointsClaim{Balance: -1}, ctx.Err()
	}
	return res, nil
}

func (p *pointsFake) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func (p *pointsFake) channels() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

var _ platform.PointsClaimer = (*pointsFake)(nil)

// fixedPoints is a Config.PointsMode that always reads mode.
func fixedPoints(mode platform.PointsMode) func() platform.PointsMode {
	return func() platform.PointsMode { return mode }
}

// shrinkPoints sets the claim cadence and timeout for one test.
func shrinkPoints(t *testing.T, every, timeout time.Duration) {
	t.Helper()
	oe, ot := pointsEvery, pointsTimeout
	pointsEvery, pointsTimeout = every, timeout
	t.Cleanup(func() { pointsEvery, pointsTimeout = oe, ot })
}

// pointsRunner is a runner on p watching channel "a" for d1.
func pointsRunner(t *testing.T, p *pointsFake, mode platform.PointsMode, mod func(*Config)) *Runner {
	t.Helper()
	p.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	p.Campaigns[0].Benefits[0].RequiredMinutes = 100000
	p.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	p.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	cfg := Config{AccountID: "acc", Platform: "twitch", Backend: p, Games: rustGames, PointsMode: fixedPoints(mode),
		TickInterval: time.Nanosecond}
	if mod != nil {
		mod(&cfg)
	}
	r := New(cfg)
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	return r
}

// An account with points off, or an unknown mode, sends no points request
// on a drop watch or a force watch, however long it watches.
func TestPoints_OffSendsNoRequests(t *testing.T) {
	shrinkPoints(t, time.Millisecond, time.Second)
	for _, mode := range []platform.PointsMode{"", platform.PointsOff, "bogus"} {
		p := &pointsFake{Fake: platformtest.NewFake()}
		r := pointsRunner(t, p, mode, nil)
		steps(t, r, 5) // pick, start, ticks
		if s := r.State(); s != StateWatching {
			t.Fatalf("mode %q: state %s", mode, s)
		}
		time.Sleep(20 * time.Millisecond)
		r.stopWatch(context.Background())

		f := &pointsFake{Fake: platformtest.NewFake()}
		setLive(f.Fake, "b", true)
		fr := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, PointsMode: fixedPoints(mode),
			TickInterval:  time.Nanosecond,
			ForceChannels: func() []string { return []string{"b"} },
			ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
				return f.LookupChannel(ctx, platform.Session{}, login)
			}})
		steps(t, fr, 3)
		if s := fr.State(); s != StateForceWatching {
			t.Fatalf("mode %q: force state %s", mode, s)
		}
		time.Sleep(20 * time.Millisecond)
		fr.stopWatch(context.Background())
		if n := p.count() + f.count(); n != 0 {
			t.Fatalf("mode %q: %d points requests, want 0", mode, n)
		}
	}
}

// A drop watch claims at once on the watched channel, then every pointsEvery.
func TestPoints_ClaimsAtStartThenOnCadence(t *testing.T) {
	shrinkPoints(t, 30*time.Millisecond, time.Second)
	p := &pointsFake{Fake: platformtest.NewFake(), result: platform.PointsClaim{Claimed: true, Gained: 50, Balance: 500}}
	r := pointsRunner(t, p, platform.PointsDropsPoints, nil)
	steps(t, r, 2) // pick, start
	if !waitFor(func() bool { return p.count() >= 1 }, time.Second) {
		t.Fatal("no claim at watch start")
	}
	if n := p.count(); n != 1 {
		t.Fatalf("%d claims right after start, want 1", n)
	}
	if !waitFor(func() bool { return p.count() >= 3 }, 2*time.Second) {
		t.Fatalf("claims=%d, want a claim every interval", p.count())
	}
	for _, ch := range p.channels() {
		if ch != "a" {
			t.Fatalf("claimed on %q, want a", ch)
		}
	}
}

// Stopping the watch cancels an in-flight claim and ends the goroutine; no
// claim follows.
func TestPoints_StopCancelsTask(t *testing.T) {
	shrinkPoints(t, 10*time.Millisecond, time.Minute)
	p := &pointsFake{Fake: platformtest.NewFake(), block: true}
	r := pointsRunner(t, p, platform.PointsDropsPoints, nil)
	steps(t, r, 2)
	if !waitFor(func() bool { return p.count() == 1 }, time.Second) {
		t.Fatal("no claim at watch start")
	}
	r.mu.Lock()
	task := r.points
	r.mu.Unlock()
	if task == nil {
		t.Fatal("no points task while watching")
	}

	stopped := make(chan struct{})
	go func() {
		r.stopWatch(context.Background())
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stopWatch blocked on the points task")
	}
	select {
	case <-task.done:
	default:
		t.Fatal("points goroutine still running after stopWatch")
	}
	r.mu.Lock()
	left := r.points
	r.mu.Unlock()
	if left != nil {
		t.Fatal("points task kept after stopWatch")
	}
	p.mu.Lock()
	ended := append([]error(nil), p.ended...)
	p.mu.Unlock()
	if len(ended) != 1 || !errors.Is(ended[0], context.Canceled) {
		t.Fatalf("in-flight claim ended with %v, want canceled", ended)
	}
	time.Sleep(50 * time.Millisecond)
	if n := p.count(); n != 1 {
		t.Fatalf("%d claims after stop, want 1", n)
	}
}

// Each attempt has its own timeout; a timed-out attempt does not end the task.
func TestPoints_AttemptTimeout(t *testing.T) {
	shrinkPoints(t, 5*time.Millisecond, 10*time.Millisecond)
	p := &pointsFake{Fake: platformtest.NewFake(), block: true}
	r := pointsRunner(t, p, platform.PointsDropsPoints, nil)
	steps(t, r, 2)
	if !waitFor(func() bool { return p.count() >= 3 }, 2*time.Second) {
		t.Fatalf("claims=%d, want retries after timeouts", p.count())
	}
	p.mu.Lock()
	first := p.ended[0]
	p.mu.Unlock()
	if !errors.Is(first, context.DeadlineExceeded) {
		t.Fatalf("attempt ended with %v, want deadline exceeded", first)
	}
}

// A claim stuck in flight never holds up a beacon.
func TestPoints_BeaconNotDelayed(t *testing.T) {
	shrinkPoints(t, time.Minute, time.Minute)
	p := &pointsFake{Fake: platformtest.NewFake(), block: true}
	r := pointsRunner(t, p, platform.PointsDropsPoints, nil)
	steps(t, r, 2)
	if !waitFor(func() bool { return p.count() == 1 }, time.Second) {
		t.Fatal("no claim at watch start")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			if err := r.step(context.Background()); err != nil {
				t.Errorf("tick %d: %v", i, err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ticks blocked behind the points claim")
	}
	if n := p.CallCount("Heartbeat:a"); n != 3 {
		t.Fatalf("heartbeats=%d, want 3", n)
	}
}

// A force watch claims on the force channel, and a new watch replaces the
// previous task.
func TestPoints_ForceWatchClaims(t *testing.T) {
	shrinkPoints(t, time.Minute, time.Second)
	p := &pointsFake{Fake: platformtest.NewFake()}
	setLive(p.Fake, "b", true)
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: p, PointsMode: fixedPoints(platform.PointsDropsPoints),
		TickInterval:  time.Nanosecond,
		ForceChannels: func() []string { return []string{"b"} },
		ForceLookup: func(ctx context.Context, login string) (platform.Stream, bool, error) {
			return p.LookupChannel(ctx, platform.Session{}, login)
		}})
	t.Cleanup(func() { r.stopWatch(context.Background()) })
	steps(t, r, 1)
	forceOn(t, r, "b")
	if !waitFor(func() bool { return p.count() == 1 }, time.Second) {
		t.Fatalf("claims=%d on force watch, want 1", p.count())
	}
	if got := p.channels(); got[0] != "b" {
		t.Fatalf("claimed on %v, want b", got)
	}
}

// A startPoints racing a stopWatch (Run's start against a PubSub stream-down
// push) leaves no claim task behind: stopWatch takes the task in the same
// locked section that clears the handle, so none can start on a dead watch.
func TestPoints_StopWatchRaceLeavesNoTask(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	p := &pointsFake{Fake: platformtest.NewFake()}
	r := pointsRunner(t, p, platform.PointsDropsPoints, nil)
	// A task whose stop blocks until released holds stopWatch mid-stop.
	held := &pointsTask{cancel: func() {}, done: make(chan struct{})}
	r.mu.Lock()
	r.handle = &platform.WatchHandle{Channel: "a"}
	r.stream = &platform.Stream{Channel: "a", ChannelID: "ida"}
	r.points = held
	r.mu.Unlock()

	stopped := make(chan struct{})
	go func() {
		r.stopWatch(context.Background())
		close(stopped)
	}()
	if !waitFor(func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.points == nil
	}, time.Second) {
		t.Fatal("stopWatch never took the points task")
	}
	r.startPoints(context.Background(), "a")
	close(held.done)
	<-stopped

	r.mu.Lock()
	task, h := r.points, r.handle
	r.mu.Unlock()
	if task != nil {
		task.stop()
		t.Fatalf("points task outlived its watch (handle=%v)", h)
	}
	time.Sleep(20 * time.Millisecond)
	if n := p.count(); n != 0 {
		t.Fatalf("claims=%d after the watch stopped, want 0", n)
	}
}

// Every claim attempt hands its balance to OnPointsBalance, claimed or not;
// an unknown balance is not reported.
func TestPoints_ReportsBalanceToHook(t *testing.T) {
	shrinkPoints(t, time.Hour, time.Second)
	for _, tc := range []struct {
		name   string
		result platform.PointsClaim
		want   []int
	}{
		{"no bonus", platform.PointsClaim{Balance: 450}, []int{450}},
		{"claimed", platform.PointsClaim{Claimed: true, Gained: 50, Balance: 500}, []int{500}},
		{"unknown", platform.PointsClaim{Balance: -1}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var got []int
			var chans []string
			p := &pointsFake{Fake: platformtest.NewFake(), result: tc.result}
			r := pointsRunner(t, p, platform.PointsDropsPoints, func(c *Config) {
				c.OnPointsBalance = func(_ context.Context, ch string, bal int) {
					mu.Lock()
					got = append(got, bal)
					chans = append(chans, ch)
					mu.Unlock()
				}
			})
			steps(t, r, 2) // pick, start
			if !waitFor(func() bool { return p.count() >= 1 }, time.Second) {
				t.Fatal("no claim at watch start")
			}
			r.stopWatch(context.Background())
			mu.Lock()
			defer mu.Unlock()
			if len(got) != len(tc.want) || (len(got) == 1 && (got[0] != tc.want[0] || chans[0] != "a")) {
				t.Fatalf("hook got %v on %v, want %v on a", got, chans, tc.want)
			}
		})
	}
}

// A failed claim reports no balance.
func TestPoints_FailedClaimReportsNoBalance(t *testing.T) {
	shrinkPoints(t, time.Hour, 5*time.Millisecond)
	called := false
	var mu sync.Mutex
	p := &pointsFake{Fake: platformtest.NewFake(), block: true}
	r := pointsRunner(t, p, platform.PointsDropsPoints, func(c *Config) {
		c.OnPointsBalance = func(context.Context, string, int) { mu.Lock(); called = true; mu.Unlock() }
	})
	steps(t, r, 2)
	if !waitFor(func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.ended) >= 1 }, time.Second) {
		t.Fatal("claim did not time out")
	}
	r.stopWatch(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("hook called after a failed claim")
	}
}
