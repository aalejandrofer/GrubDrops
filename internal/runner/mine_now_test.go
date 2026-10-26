package runner

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// minePool runs one Runner for "acc" in a pool, reading the pool's order.
func minePool(t *testing.T, f *platformtest.Fake, mod func(*Config)) (*Pool, *Runner) {
	t.Helper()
	p := NewPool(nil)
	var r *Runner
	b := func() Entry {
		r = newRunner(f, func(c *Config) {
			c.TickInterval = time.Hour // a switch must not wait for the next beacon
			c.Order = p.OrderFunc("acc")
			if mod != nil {
				mod(c)
			}
		})
		return Entry{ID: "acc", Unit: r}
	}
	if err := p.Reload(context.Background(), []Builder{b}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	return p, r
}

func waitSnap(t *testing.T, r *Runner, ok func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := r.Snapshot()
		if ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out; snapshot %+v", s)
		}
		time.Sleep(time.Millisecond)
	}
}

func mining(id string) func(Snapshot) bool {
	return func(s Snapshot) bool {
		return s.State == StateWatching && s.Current != nil && s.Current.Benefit.ID == id
	}
}

func upNextIDs(s Snapshot) []string {
	var ids []string
	for _, t := range s.UpNext {
		ids = append(ids, t.Benefit.ID)
	}
	return ids
}

func threeLive(f *platformtest.Fake) {
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2"), camp("c3", "Rust", "d3")}
	f.Streams["c1"] = []platform.Stream{{Channel: "s1", ViewerCount: 1}}
	f.Streams["c2"] = []platform.Stream{{Channel: "s2", ViewerCount: 1}}
	f.Streams["c3"] = []platform.Stream{{Channel: "s3", ViewerCount: 1}}
}

func TestPool_MineNowStartsFirstUpNext(t *testing.T) {
	f := platformtest.NewFake()
	threeLive(f)
	p, r := minePool(t, f, nil)

	s := waitSnap(t, r, mining("d1"))
	if got := upNextIDs(s); !slices.Equal(got, []string{"d2", "d3"}) {
		t.Fatalf("up next before = %v", got)
	}

	bid, err := p.MineNow("acc")
	if err != nil || bid != "d2" {
		t.Fatalf("MineNow = %q, %v; want d2", bid, err)
	}
	s = waitSnap(t, r, mining("d2"))
	if s.Channel != "s2" {
		t.Fatalf("channel = %q", s.Channel)
	}
	if f.CallCount("Stop:s1") != 1 {
		t.Fatalf("old watch not stopped: %v", f.Calls)
	}
	if got := upNextIDs(s); !slices.Equal(got, []string{"d1", "d3"}) {
		t.Fatalf("up next after = %v, want old current first", got)
	}
	if got := p.OrderFunc("acc")(); !slices.Equal(got, []string{"d2", "d1", "d3"}) {
		t.Fatalf("order = %v", got)
	}
}

// TestPool_MineNowSticksWhenScanOmitsTarget covers Twitch TV discovery: it
// samples live channels, so the rescan right after the switch can miss the
// campaign just queued. The switch must hold instead of reverting to d1.
func TestPool_MineNowSticksWhenScanOmitsTarget(t *testing.T) {
	f := platformtest.NewFake()
	threeLive(f)
	p, r := minePool(t, f, func(c *Config) {
		c.Platform = "twitch"
		c.Session = platform.Session{ClientID: "tv", AccessToken: "tok"}
	})
	waitSnap(t, r, mining("d1"))

	f.Mu.Lock()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c3", "Rust", "d3")}
	f.Mu.Unlock()
	if _, err := p.MineNow("acc"); err != nil {
		t.Fatal(err)
	}
	s := waitSnap(t, r, mining("d2"))
	if got := upNextIDs(s); !slices.Equal(got, []string{"d1", "d3"}) {
		t.Fatalf("up next after = %v", got)
	}

	// A later pick on a scan that still omits c2 keeps d2.
	r.mu.Lock()
	res := plan.Result{Queue: slices.Clone(r.queue)}
	got := r.pinnedLocked(res, time.Now())
	r.mu.Unlock()
	if got == nil || got.Benefit.ID != "d2" {
		t.Fatalf("pin after omitted scan = %+v", got)
	}
}

func TestPool_MineNowWakesSleepingRunner(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2")}
	p, r := minePool(t, f, nil) // no live streams: sleeps for recheckInterval

	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	f.Mu.Lock()
	f.Streams["c1"] = []platform.Stream{{Channel: "s1", ViewerCount: 1}}
	f.Mu.Unlock()
	// Sleeping with no current: the whole queue is Up next.
	if bid, err := p.MineNow("acc"); err != nil || bid != "d1" {
		t.Fatalf("MineNow = %q, %v", bid, err)
	}
	waitSnap(t, r, mining("d1"))
}

func TestPool_MineNowRejects(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Streams["c1"] = []platform.Stream{{Channel: "s1", ViewerCount: 1}}
	p, r := minePool(t, f, nil)
	waitSnap(t, r, mining("d1"))

	if _, err := p.MineNow("acc"); !errors.Is(err, ErrNotQueued) {
		t.Fatalf("empty up next: %v", err)
	}
	if _, err := p.MineNow("nope"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unknown account: %v", err)
	}
	if got := p.OrderFunc("acc")(); got != nil {
		t.Fatalf("order changed on reject: %v", got)
	}

	idle := NewPool(nil)
	_ = idle.Reload(context.Background(), []Builder{func() Entry {
		return Entry{ID: "x", Unit: &Idle{AccountID: "x", State: StateNeedsLogin}}
	}})
	defer idle.Stop()
	if _, err := idle.MineNow("x"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("idle unit: %v", err)
	}
}

func TestRunner_MineNowStoppedState(t *testing.T) {
	f := platformtest.NewFake()
	r := newRunner(f, func(c *Config) { c.Games = nil })
	_ = r.step(context.Background()) // NoGames
	if err := r.MineNow("d1"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("got %v", err)
	}
}

func pinTarget(id string) plan.Target {
	c := camp("c-"+id, "Rust", id)
	return plan.Target{Campaign: c, Benefit: c.Benefits[0], Minutes: 1}
}

func TestRunner_PinnedLocked(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ended := pinTarget("p")
	ended.Campaign.EndsAt = now.Add(-time.Minute)
	fresh := pinTarget("p")
	fresh.Minutes = 30
	cases := []struct {
		name    string
		pin     plan.Target
		res     plan.Result
		want    int // minutes of the returned target, -1 for nil
		keepPin bool
	}{
		{"queued takes fresh data", pinTarget("p"), plan.Result{Queue: []plan.Target{pinTarget("a"), fresh}}, 30, true},
		{"omitted keeps pin", pinTarget("p"), plan.Result{Queue: []plan.Target{pinTarget("a")}}, 1, true},
		{"blocked drops pin", pinTarget("p"), plan.Result{Blocked: []plan.Blocked{{Target: pinTarget("p"), Reason: plan.ReasonNotLinked}}}, -1, false},
		{"claimed drops pin", pinTarget("p"), plan.Result{Claimed: []plan.Target{pinTarget("p")}}, -1, false},
		{"ended drops pin", ended, plan.Result{}, -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRunner(platformtest.NewFake(), nil)
			pin := c.pin
			r.pinned = &pin
			got := r.pinnedLocked(c.res, now)
			switch {
			case c.want < 0 && got != nil:
				t.Fatalf("got %+v, want nil", got)
			case c.want >= 0 && (got == nil || got.Benefit.ID != "p" || got.Minutes != c.want):
				t.Fatalf("got %+v, want p with %d min", got, c.want)
			}
			if (r.pinned != nil) != c.keepPin {
				t.Fatalf("pin kept = %v, want %v", r.pinned != nil, c.keepPin)
			}
		})
	}
}

func TestRunner_PinDroppedOnClaimAndSkip(t *testing.T) {
	r := newRunner(platformtest.NewFake(), nil)
	pin := pinTarget("p")
	r.pinned, r.current = &pin, &pin
	r.finishClaim(context.Background(), pin.Benefit, "s")
	if r.pinned != nil {
		t.Fatal("pin kept after claim")
	}

	other := pinTarget("o")
	r.pinned, r.current = &pin, &other
	r.skipCurrent(context.Background(), "o")
	if r.pinned == nil {
		t.Fatal("skip of another benefit dropped the pin")
	}
	r.current = &pin
	r.skipCurrent(context.Background(), "p")
	if r.pinned != nil {
		t.Fatal("pin kept after skip")
	}
}

func TestRunner_PinDroppedWhenNoStream(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2")}
	f.Streams["c1"] = []platform.Stream{{Channel: "s1", ViewerCount: 1}}
	r := newRunner(f, nil)
	ctx := context.Background()
	if err := r.pick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.MineNow("d2"); err != nil {
		t.Fatal(err)
	}
	if err := r.pick(ctx); err != nil {
		t.Fatal(err)
	}
	if s := r.Snapshot(); s.Current == nil || s.Current.Benefit.ID != "d2" {
		t.Fatalf("pick = %+v, want pinned d2", s.Current)
	}
	if err := r.start(ctx); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if s.Current == nil || s.Current.Benefit.ID != "d1" || r.pinned != nil {
		t.Fatalf("current = %+v, pinned = %v; want d1 and no pin", s.Current, r.pinned)
	}
}
