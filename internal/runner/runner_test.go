package runner

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

var rustGames = []plan.Game{{Name: "Rust", Slug: "rust"}}

func camp(id, game string, ids ...string) platform.Campaign {
	c := platform.Campaign{ID: id, Platform: "kick", Game: game, Status: "active", AccountLinked: true}
	for _, b := range ids {
		c.Benefits = append(c.Benefits, platform.DropBenefit{ID: b, CampaignID: id, RequiredMinutes: 60})
	}
	return c
}

func newRunner(f *platformtest.Fake, mod func(*Config)) *Runner {
	cfg := Config{AccountID: "acc", Platform: "kick", Backend: f, Games: rustGames}
	if mod != nil {
		mod(&cfg)
	}
	return New(cfg)
}

type fakeClaims struct{ ids map[string]bool }

func (c fakeClaims) RecordClaim(context.Context, string, platform.DropBenefit) error { return nil }
func (c fakeClaims) RecordClaimWithCode(context.Context, string, platform.DropBenefit, string) error {
	return nil
}
func (c fakeClaims) ClaimedBenefitIDs(context.Context, string) (map[string]bool, error) {
	return c.ids, nil
}

type fakeSkips struct {
	mu      sync.Mutex
	skips   map[string]bool
	cleared []string
}

func (s *fakeSkips) Skips(string) (map[string]bool, error) { return s.skips, nil }
func (s *fakeSkips) RecordSkip(_, id string) error         { return nil }
func (s *fakeSkips) ClearSkip(_, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleared = append(s.cleared, id)
	return nil
}

func TestRunner_NoGamesDoesNotScan(t *testing.T) {
	f := platformtest.NewFake()
	r := newRunner(f, func(c *Config) { c.Games = nil })
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State() != StateNoGames || f.CallCount("List") != 0 {
		t.Fatalf("state=%s list=%d", r.State(), f.CallCount("List"))
	}
	if err := r.step(context.Background()); err != errStop {
		t.Fatalf("want errStop, got %v", err)
	}
}

func TestRunner_PickSetsStartingWithQueueHead(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	r := newRunner(f, nil)
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if s.State != StateStarting || s.Current == nil || s.Current.Benefit.ID != "d1" || s.LastScanAt.IsZero() {
		t.Fatalf("%+v", s)
	}
}

func TestRunner_IntegrityBlockedNeedsLogin(t *testing.T) {
	f := platformtest.NewFake()
	f.ListErr = fmt.Errorf("x: %w", platform.ErrIntegrityBlocked)
	r := newRunner(f, nil)
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State() != StateNeedsLogin {
		t.Fatal(r.State())
	}
	if err := r.step(context.Background()); err != errStop {
		t.Fatalf("want errStop, got %v", err)
	}
}

func TestRunner_NothingMinableSleeps(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", Claimed: true}
	r := newRunner(f, nil)
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State() != StateSleeping {
		t.Fatal(r.State())
	}
	if err := r.step(context.Background()); err != errIdle {
		t.Fatalf("want errIdle, got %v", err)
	}
}

func TestRunner_OnlyUnlinkedAwaitsConnect(t *testing.T) {
	f := platformtest.NewFake()
	c := camp("c1", "Rust", "d1")
	c.Platform, c.AccountLinked = "twitch", false
	f.Campaigns = []platform.Campaign{c}
	r := newRunner(f, nil)
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if s.State != StateAwaitingConnect || len(s.Blocked) != 1 || s.Blocked[0].Reason != plan.ReasonNotLinked {
		t.Fatalf("%+v", s)
	}
}

func TestRunner_ClaimLogExcludesBenefit(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1", "d2")}
	r := newRunner(f, func(c *Config) { c.Claims = fakeClaims{ids: map[string]bool{"d1": true}} })
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().Current.Benefit.ID; got != "d2" {
		t.Fatal(got)
	}
}

func TestRunner_GhostSkipSelfHeals(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 5}
	sk := &fakeSkips{skips: map[string]bool{"d1": true}}
	r := newRunner(f, func(c *Config) { c.Skips = sk })
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sk.cleared) != 1 || sk.cleared[0] != "d1" {
		t.Fatalf("cleared=%v", sk.cleared)
	}
	if s := r.Snapshot(); s.Current == nil || s.Current.Benefit.ID != "d1" {
		t.Fatalf("%+v", s)
	}
}

func TestRunner_ClaimFailSkipKeptWhileComplete(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 60}
	sk := &fakeSkips{skips: map[string]bool{"d1": true}}
	r := newRunner(f, func(c *Config) { c.Skips = sk })
	r.claimFailSkipped = map[string]int{"d1": 60}
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sk.cleared) != 0 || r.State() != StateSleeping {
		t.Fatalf("cleared=%v state=%s", sk.cleared, r.State())
	}
}

func TestRunner_UpNextAppliesSessionOrder(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1", "d2", "d3")}
	var order []string
	r := newRunner(f, func(c *Config) { c.Order = func() []string { return order } })
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	order = []string{"d3"}
	s := r.Snapshot()
	if s.Current.Benefit.ID != "d1" || len(s.UpNext) != 2 || s.UpNext[0].Benefit.ID != "d3" || s.UpNext[1].Benefit.ID != "d2" {
		t.Fatalf("%+v", s.UpNext)
	}
}

type sessionSpy struct {
	*platformtest.Fake
	got platform.Session
}

func (s *sessionSpy) ListActiveCampaigns(ctx context.Context, sess platform.Session) ([]platform.Campaign, error) {
	s.got = sess
	return s.Fake.ListActiveCampaigns(ctx, sess)
}

func TestRunner_SessionGamesAndFilterPlumbed(t *testing.T) {
	spy := &sessionSpy{Fake: platformtest.NewFake()}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: spy, Games: rustGames})
	if err := r.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	g := spy.got
	if g.AccountID != "acc" || len(g.Games) != 1 || g.Games[0] != "Rust" || g.GameFilter == nil || !g.GameFilter("RUST") || g.GameFilter("Other") {
		t.Fatalf("%+v", g)
	}
}

func TestRunner_RunBacksOffAndRecovers(t *testing.T) {
	old := stepErrBackoff
	stepErrBackoff = time.Millisecond
	defer func() { stepErrBackoff = old }()

	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{camp("c1", "Rust", "d1")}
	f.ListErr = fmt.Errorf("boom")
	r := newRunner(f, func(c *Config) { c.TickInterval = time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for f.CallCount("List") < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	f.Mu.Lock()
	f.ListErr = nil
	f.Mu.Unlock()
	for r.State() != StateStarting && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.State() != StateStarting {
		t.Fatal(r.State())
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Run=%v", err)
	}
}

func TestRunner_IdleKeepsStateDuringWait(t *testing.T) {
	old := recheckInterval
	recheckInterval = 150 * time.Millisecond
	t.Cleanup(func() { recheckInterval = old })

	f := platformtest.NewFake() // no campaigns -> sleeping
	var mu sync.Mutex
	type ev struct {
		s State
		t time.Time
	}
	var evs []ev
	r := newRunner(f, func(c *Config) {
		c.TickInterval = time.Millisecond
		c.OnChange = func(s Snapshot) {
			mu.Lock()
			evs = append(evs, ev{s.State, time.Now()})
			mu.Unlock()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(evs)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if st := r.State(); st != StateSleeping && st != StatePicking {
		t.Fatal(st)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(evs) < 2 || evs[0].s != StateSleeping || evs[1].s != StatePicking {
		t.Fatalf("events=%v", evs)
	}
	if d := evs[1].t.Sub(evs[0].t); d < recheckInterval {
		t.Fatalf("picking set after %v, want >= %v", d, recheckInterval)
	}
}

func TestNextBackoff(t *testing.T) {
	old := stepErrBackoff
	stepErrBackoff = 5 * time.Second
	t.Cleanup(func() { stepErrBackoff = old })
	d := time.Duration(0)
	var got []time.Duration
	for range 9 {
		d = nextBackoff(d)
		got = append(got, d)
	}
	want := []time.Duration{5, 10, 20, 40, 80, 160, 300, 300, 300}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("step %d: %v want %vs", i, got[i], want[i])
		}
	}
}
