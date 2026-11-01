package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// wakePool runs runners "a" and "b" on one platform, each on its own fake,
// with their scans feeding one CampaignWaker that rescans through the pool.
func wakePool(t *testing.T, fa, fb *platformtest.Fake) (a, b *Runner) {
	t.Helper()
	p := NewPool(nil)
	w := NewCampaignWaker(p.RescanPlatform, 10*time.Millisecond)
	build := func(id string, f *platformtest.Fake, out **Runner) Builder {
		return func() Entry {
			r := newRunner(f, func(c *Config) {
				c.AccountID = id
				c.TickInterval = time.Hour
				c.OnScan = func(_ context.Context, s Scan) { w.OnScan(s) }
			})
			*out = r
			return Entry{ID: id, Unit: r}
		}
	}
	if err := p.Reload(context.Background(), []Builder{build("a", fa, &a), build("b", fb, &b)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	return a, b
}

func sleepingRunner(s Snapshot) bool { return s.State == StateSleeping }

// listCalls waits past the waker's debounce and returns f's scan count.
func listCalls(f *platformtest.Fake) int {
	time.Sleep(100 * time.Millisecond)
	return f.CallCount("List")
}

// A's scan finds a new campaign: B leaves its hour-long idle wait at once.
func TestCampaignWaker_NewCampaignWakesOtherAccount(t *testing.T) {
	fa, fb := platformtest.NewFake(), platformtest.NewFake()
	a, b := wakePool(t, fa, fb)
	waitSnap(t, a, sleepingRunner)
	waitSnap(t, b, sleepingRunner)

	x := camp("cx", "Rust", "dx")
	for _, f := range []*platformtest.Fake{fa, fb} {
		f.Mu.Lock()
		f.Campaigns = []platform.Campaign{x}
		f.Streams["cx"] = []platform.Stream{{Channel: "sx", ViewerCount: 1}}
		f.Mu.Unlock()
	}
	a.Rescan() // A's own scan clock runs out
	waitSnap(t, b, mining("dx"))
}

// A rescan that finds nothing new wakes nobody.
func TestCampaignWaker_NothingNewNoWake(t *testing.T) {
	fa, fb := platformtest.NewFake(), platformtest.NewFake()
	fa.Campaigns = []platform.Campaign{camp("cx", "Rust", "dx")}
	fb.Campaigns = fa.Campaigns
	a, b := wakePool(t, fa, fb)
	waitSnap(t, a, sleepingRunner)
	waitSnap(t, b, sleepingRunner)
	before := listCalls(fb)

	a.Rescan()
	waitSnap(t, a, func(s Snapshot) bool { return fa.CallCount("List") >= 2 && s.State == StateSleeping })
	if got := listCalls(fb); got != before {
		t.Fatalf("b scanned %d times, want %d", got, before)
	}
}

// Every account's first scan only seeds the set: campaigns already up at
// startup wake nobody.
func TestCampaignWaker_StartupNoStorm(t *testing.T) {
	fa, fb := platformtest.NewFake(), platformtest.NewFake()
	fa.Campaigns = []platform.Campaign{camp("cx", "Rust", "dx"), camp("cy", "Rust", "dy")}
	fb.Campaigns = []platform.Campaign{camp("cx", "Rust", "dx")}
	a, b := wakePool(t, fa, fb)
	waitSnap(t, a, sleepingRunner)
	waitSnap(t, b, sleepingRunner)
	if ga, gb := listCalls(fa), listCalls(fb); ga != 1 || gb != 1 {
		t.Fatalf("startup scans a=%d b=%d, want 1 each", ga, gb)
	}
}

// A burst of new campaigns from one finder makes one wake, which skips the
// finder. A failed scan is ignored.
func TestCampaignWaker_DebouncesBurst(t *testing.T) {
	type call struct{ plat, except string }
	var (
		mu    sync.Mutex
		calls []call
	)
	w := NewCampaignWaker(func(plat, except string) {
		mu.Lock()
		calls = append(calls, call{plat, except})
		mu.Unlock()
	}, 50*time.Millisecond)
	scan := func(acc string, ids ...string) Scan {
		s := Scan{AccountID: acc, Platform: "twitch"}
		for _, id := range ids {
			s.Result.Queue = append(s.Result.Queue, plan.Target{Campaign: platform.Campaign{ID: id}})
		}
		return s
	}
	w.OnScan(scan("a", "c1"))
	w.OnScan(scan("b", "c1"))
	w.OnScan(scan("a", "c1", "c2"))
	w.OnScan(scan("a", "c1", "c2", "c3"))
	w.OnScan(Scan{AccountID: "a", Platform: "twitch", Err: context.Canceled})
	w.OnScan(scan("a", "c1", "c2", "c3"))
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != (call{"twitch", "a"}) {
		t.Fatalf("rescans = %+v, want one twitch rescan skipping a", calls)
	}
}

// An idle runner with an empty queue wakes at the earliest upcoming
// whitelisted campaign start, not after the full idle recheck.
func TestRunner_IdleWaitCappedAtUpcomingStart(t *testing.T) {
	old := startJitter
	startJitter = 10 * time.Millisecond
	t.Cleanup(func() { startJitter = old })

	f := platformtest.NewFake()
	soon := camp("cs", "Rust", "ds")
	soon.Status = "upcoming"
	soon.StartsAt = time.Now().Add(300 * time.Millisecond)
	other := camp("co", "Valorant", "do") // not whitelisted: no cap
	other.Status = "upcoming"
	other.StartsAt = time.Now().Add(100 * time.Millisecond)
	f.Campaigns = []platform.Campaign{soon, other}
	_, r := minePool(t, f, nil)

	s := waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping && !s.NextScanAt.IsZero() })
	if s.NextScanAt.Before(soon.StartsAt) || s.NextScanAt.After(soon.StartsAt.Add(time.Second)) {
		t.Fatalf("NextScanAt=%v, want just after start %v", s.NextScanAt, soon.StartsAt)
	}
	deadline := time.Now().Add(3 * time.Second)
	for f.CallCount("List") < 2 {
		if time.Now().After(deadline) {
			t.Fatal("no rescan at the campaign start")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// sampledFake discovers campaigns from a sample of live channels, like a
// Twitch TV-client session.
type sampledFake struct{ *platformtest.Fake }

func (sampledFake) SampledDiscovery(platform.Session) bool { return true }

// A sampled-discovery account with an empty queue rechecks at the short
// pace: a campaign whose channels were not sampled can show up soon.
func TestRunner_SampledEmptyQueueRechecksShort(t *testing.T) {
	f := platformtest.NewFake()
	r := newRunner(f, func(c *Config) { c.Backend = sampledFake{f}; c.TickInterval = time.Hour })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	s := waitSnap(t, r, func(s Snapshot) bool { return s.State == StateSleeping && !s.NextScanAt.IsZero() })
	if left := time.Until(s.NextScanAt); left > recheckInterval || left < recheckInterval-time.Minute {
		t.Fatalf("next scan in %v, want about %v", left, recheckInterval)
	}
}
