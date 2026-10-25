package runner

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

type notifyRec struct {
	event  string
	fields map[string]any
}

type spyNotifier struct {
	mu  sync.Mutex
	got []notifyRec
}

func (n *spyNotifier) Notify(_ context.Context, ev string, f map[string]any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, notifyRec{ev, f})
	return nil
}

func (n *spyNotifier) of(ev string) []notifyRec {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []notifyRec
	for _, g := range n.got {
		if g.event == ev {
			out = append(out, g)
		}
	}
	return out
}

type recClaims struct {
	fakeClaims
	mu  sync.Mutex
	rec []string
}

func (c *recClaims) RecordClaim(_ context.Context, acc string, b platform.DropBenefit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rec = append(c.rec, acc+":"+b.ID)
	return nil
}

func tiered(id, game string, mins ...int) platform.Campaign {
	c := platform.Campaign{ID: id, Platform: "kick", Game: game, Status: "active", AccountLinked: true}
	for i, m := range mins {
		c.Benefits = append(c.Benefits, platform.DropBenefit{
			ID: "d" + string(rune('1'+i)), Name: "Drop" + string(rune('1'+i)), CampaignID: id, RequiredMinutes: m,
		})
	}
	return c
}

func claimSetup(f *platformtest.Fake, mins ...int) (*Runner, *spyNotifier, *recClaims, *fakeSkips) {
	f.Campaigns = []platform.Campaign{tiered("c1", "Rust", mins...)}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	for _, b := range f.Campaigns[0].Benefits {
		f.Progress[b.ID] = &platform.Progress{BenefitID: b.ID}
	}
	n := &spyNotifier{}
	cl := &recClaims{}
	sk := &fakeSkips{}
	r := newRunner(f, func(c *Config) { c.Notifier = n; c.Claims = cl; c.Skips = sk })
	return r, n, cl, sk
}

func TestRunner_MinesUntilClaim(t *testing.T) {
	f := platformtest.NewFake()
	r, n, cl, _ := claimSetup(f, 2)
	steps(t, r, 5) // pick, start, tick, tick->claiming, claim
	if r.State() != StatePicking {
		t.Fatalf("state=%s", r.State())
	}
	if c := f.CallCount("Claim:d1"); c != 1 {
		t.Fatalf("claims=%d", c)
	}
	if len(cl.rec) != 1 || cl.rec[0] != "acc:d1" {
		t.Fatalf("rec=%v", cl.rec)
	}
	cn := n.of("claim")
	if len(cn) != 1 || cn[0].fields["drop"] != "Drop1" || cn[0].fields["channel"] != "a" ||
		cn[0].fields["cur_min"] != 2 || cn[0].fields["req_min"] != 2 {
		t.Fatalf("notify=%+v", cn)
	}
	if r.Snapshot().Current != nil {
		t.Fatal("current not cleared")
	}
}

// toClaiming drives the runner to StateClaiming on d1.
func toClaiming(t *testing.T, r *Runner) {
	t.Helper()
	steps(t, r, 4)
	if r.State() != StateClaiming {
		t.Fatalf("state=%s", r.State())
	}
}

func TestClaim_RepeatedFailureSkips(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, sk := claimSetup(f, 2, 2)
	f.ClaimErr = errors.New("boom")
	toClaiming(t, r)
	for i := 0; i < claimFailSkipThreshold; i++ {
		if err := r.claim(context.Background()); err == nil {
			t.Fatalf("attempt %d: want error", i)
		}
	}
	if len(sk.recorded) != 1 || sk.recorded[0] != "d1" {
		t.Fatalf("recorded=%v", sk.recorded)
	}
	f.ClaimErr = nil
	r.setState(context.Background(), StatePicking) // Run does this after a step error
	steps(t, r, 1)
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "d2" {
		t.Fatalf("current=%+v", cur)
	}
}

func TestClaim_TransientFailureNotSkipped(t *testing.T) {
	f := platformtest.NewFake()
	r, _, cl, sk := claimSetup(f, 2)
	toClaiming(t, r)
	f.ClaimErr = errors.New("flaky")
	for i := 0; i < 2; i++ {
		if err := r.claim(context.Background()); err == nil {
			t.Fatal("want error")
		}
	}
	f.ClaimErr = nil
	if err := r.claim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sk.recorded) != 0 || len(cl.rec) != 1 || len(r.claimFailures) != 0 {
		t.Fatalf("skips=%v rec=%v failures=%v", sk.recorded, cl.rec, r.claimFailures)
	}
}

func TestClaim_FailSkipKeptWhileStillComplete(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, sk := claimSetup(f, 2)
	toClaiming(t, r)
	f.ClaimErr = errors.New("boom")
	for i := 0; i < claimFailSkipThreshold; i++ {
		_ = r.claim(context.Background())
	}
	// Inventory keeps the row at >= required and unclaimed.
	f.Mu.Lock()
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 2}
	f.Mu.Unlock()
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1)
	if len(sk.cleared) != 0 {
		t.Fatalf("cleared=%v", sk.cleared)
	}
}

func TestClaim_MultiTierContinuesAfterFirstClaim(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, _ := claimSetup(f, 1, 3)
	steps(t, r, 4) // pick, start, tick -> claiming (1 min), claim
	if f.CallCount("Claim:d1") != 1 {
		t.Fatalf("calls=%v", f.Calls)
	}
	steps(t, r, 1) // pick
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "d2" {
		t.Fatalf("current=%+v", cur)
	}
}

func TestSweep_NotifiesSiblingsOnceExcludingCurrent(t *testing.T) {
	f := platformtest.NewFake()
	f.Campaigns = []platform.Campaign{tiered("c1", "Rust", 60)}
	f.Streams["c1"] = []platform.Stream{{Channel: "a", ChannelID: "ida", ViewerCount: 10}}
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1"}
	sf := &platformtest.SweepFake{Fake: f}
	sf.Sweep = func() []platform.ClaimedReward {
		return []platform.ClaimedReward{{Title: "Sib", Game: "Rust"}, {Title: "Drop1"}, {Title: ""}}
	}
	n := &spyNotifier{}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: sf, Games: rustGames, Notifier: n})
	steps(t, r, 4) // pick, start, tick, tick
	cn := n.of("claim")
	if len(cn) != 1 || cn[0].fields["drop"] != "Sib" || cn[0].fields["game"] != "Rust" {
		t.Fatalf("notify=%+v", cn)
	}
}

func TestReaper_CallsOnceWithRewardGames(t *testing.T) {
	f := platformtest.NewFake()
	rf := &platformtest.RewardFake{Fake: f, Rewards: []platform.ClaimedReward{{Game: "Rust", Title: "R"}}}
	a := platform.Campaign{ID: "r1", Platform: "kick", Game: "Rust", Kind: "reward", Status: "active"}
	b := platform.Campaign{ID: "r2", Platform: "kick", Game: "Rust", Kind: "reward", Status: "active"}
	other := platform.Campaign{ID: "r3", Platform: "kick", Game: "Other", Kind: "reward", Status: "active"}
	f.Campaigns = []platform.Campaign{a, b, other}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: rf, Games: rustGames})
	steps(t, r, 1)
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	if len(rf.RewardCalls) != 1 || len(rf.RewardCalls[0]) != 1 || rf.RewardCalls[0][0] != "Rust" {
		t.Fatalf("calls=%v", rf.RewardCalls)
	}
}

func TestReaper_SkippedWithoutRewardCampaigns(t *testing.T) {
	f := platformtest.NewFake()
	rf := &platformtest.RewardFake{Fake: f}
	f.Campaigns = []platform.Campaign{tiered("c1", "Rust", 60)}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: rf, Games: rustGames})
	steps(t, r, 1)
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	if len(rf.RewardCalls) != 0 {
		t.Fatalf("calls=%v", rf.RewardCalls)
	}
}
