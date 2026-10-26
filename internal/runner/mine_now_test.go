package runner

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// minePool runs one Runner for "acc" in a pool, reading the pool's order.
func minePool(t *testing.T, f *platformtest.Fake) (*Pool, *Runner) {
	t.Helper()
	p := NewPool(nil)
	var r *Runner
	b := func() Entry {
		r = newRunner(f, func(c *Config) {
			c.TickInterval = time.Hour // a switch must not wait for the next beacon
			c.Order = p.OrderFunc("acc")
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

func TestPool_MineNowSwitchesWatch(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2"), camp("c3", "Rust", "d3")}
	f.Streams["c1"] = []platform.Stream{{Channel: "s1", ViewerCount: 1}}
	f.Streams["c2"] = []platform.Stream{{Channel: "s2", ViewerCount: 1}}
	f.Streams["c3"] = []platform.Stream{{Channel: "s3", ViewerCount: 1}}
	p, r := minePool(t, f)

	s := waitSnap(t, r, mining("d1"))
	if got := upNextIDs(s); !slices.Equal(got, []string{"d2", "d3"}) {
		t.Fatalf("up next before = %v", got)
	}

	if err := p.MineNow("acc", "d3"); err != nil {
		t.Fatal(err)
	}
	s = waitSnap(t, r, mining("d3"))
	if s.Channel != "s3" {
		t.Fatalf("channel = %q", s.Channel)
	}
	if f.CallCount("Stop:s1") != 1 {
		t.Fatalf("old watch not stopped: %v", f.Calls)
	}
	if got := upNextIDs(s); !slices.Equal(got, []string{"d1", "d2"}) {
		t.Fatalf("up next after = %v, want old current back at the front", got)
	}
	if got := p.OrderFunc("acc")(); !slices.Equal(got, []string{"d3", "d1", "d2"}) {
		t.Fatalf("order = %v", got)
	}
}

func TestPool_MineNowWakesSleepingRunner(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2")}
	p, r := minePool(t, f) // no live streams: sleeps for recheckInterval

	waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping })
	f.Mu.Lock()
	f.Streams["c2"] = []platform.Stream{{Channel: "s2", ViewerCount: 1}}
	f.Mu.Unlock()
	// Sleeping with no current: the whole queue is Up next.
	if err := p.MineNow("acc", "d2"); err != nil {
		t.Fatal(err)
	}
	waitSnap(t, r, mining("d2"))
}

func TestPool_MineNowRejects(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1"), camp("c2", "Rust", "d2")}
	f.Streams["c1"] = []platform.Stream{{Channel: "s1", ViewerCount: 1}}
	p, r := minePool(t, f)
	waitSnap(t, r, mining("d1"))

	if err := p.MineNow("acc", "ghost"); !errors.Is(err, ErrNotQueued) {
		t.Fatalf("unknown benefit: %v", err)
	}
	if err := p.MineNow("acc", "d1"); !errors.Is(err, ErrNotQueued) {
		t.Fatalf("current benefit: %v", err)
	}
	if err := p.MineNow("nope", "d2"); !errors.Is(err, ErrNotRunning) {
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
	if err := idle.MineNow("x", "d1"); !errors.Is(err, ErrNotRunning) {
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
