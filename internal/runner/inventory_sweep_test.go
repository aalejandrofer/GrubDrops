package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// refusingFake refuses the claim of the drops in refuse, as Twitch does when
// it answers a claim without granting the drop.
type refusingFake struct {
	*platformtest.Fake
	refuse map[string]bool
}

func (f *refusingFake) Claim(ctx context.Context, s platform.Session, b platform.DropBenefit) error {
	if f.refuse[b.ID] {
		f.Mu.Lock()
		f.Calls = append(f.Calls, "Claim:"+b.ID)
		f.Mu.Unlock()
		return fmt.Errorf("not granted: %w", platform.ErrClaimRefused)
	}
	return f.Fake.Claim(ctx, s, b)
}

// correctingClaims is a fakeClaims that is also a ClaimCorrector.
type correctingClaims struct {
	*fakeClaims
	cmu       sync.Mutex
	at        map[string]time.Time
	manual    map[string]bool
	forgotten []string
}

func (c *correctingClaims) LoggedClaim(_ context.Context, _, id string) (time.Time, bool, bool, error) {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	at, ok := c.at[id]
	return at, c.manual[id], ok, nil
}

func (c *correctingClaims) ForgetClaim(_ context.Context, _, id string) error {
	c.cmu.Lock()
	c.forgotten = append(c.forgotten, id)
	delete(c.at, id)
	c.cmu.Unlock()
	c.mu.Lock()
	delete(c.ids, id)
	c.mu.Unlock()
	return nil
}

func (c *correctingClaims) forgot() []string {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return append([]string(nil), c.forgotten...)
}

func noSweepGap(t *testing.T) {
	t.Helper()
	prev := sweepClaimGap
	sweepClaimGap = 0
	t.Cleanup(func() { sweepClaimGap = prev })
}

func readyDrop(id string) platform.ReadyDrop {
	return platform.ReadyDrop{
		Campaign: platform.Campaign{ID: "wt", Platform: "twitch", Name: "WTCS Pro Grand Finals", Game: "War Thunder"},
		Benefit:  platform.DropBenefit{ID: id, CampaignID: "wt", Name: "Drop " + id, RequiredMinutes: 30, InstanceID: "u#wt#" + id},
	}
}

func TestInventorySweep_ClaimsReadyDrops(t *testing.T) {
	noSweepGap(t)
	f := platformtest.NewFake()
	f.Ready = []platform.ReadyDrop{readyDrop("a"), readyDrop("b")}
	n := &spyNotifier{}
	cl := &fakeClaims{}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, Notifier: n, Claims: cl})

	r.sweepInventory(context.Background())

	if f.CallCount("Claim:a") != 1 || f.CallCount("Claim:b") != 1 {
		t.Fatalf("calls=%v", f.Calls)
	}
	got := cl.recorded()
	if len(got) != 2 || got[0] != "acc:a:" || got[1] != "acc:b:" {
		t.Fatalf("recorded=%v", got)
	}
	cl.mu.Lock()
	rec := cl.recs[0].Target
	cl.mu.Unlock()
	if rec.Campaign.Name != "WTCS Pro Grand Finals" || rec.Campaign.Game != "War Thunder" || rec.Benefit.InstanceID != "u#wt#a" {
		t.Fatalf("target=%+v", rec)
	}
	if c := n.of("claim"); len(c) != 2 {
		t.Fatalf("notifications=%+v", c)
	}
}

// GRUB-58: the claim log says claimed but the inventory still shows the drop
// finished and unclaimed. The sweep drops the stale row and claims it.
func TestInventorySweep_CorrectsStaleLoggedClaim(t *testing.T) {
	noSweepGap(t)
	f := platformtest.NewFake()
	f.Ready = []platform.ReadyDrop{readyDrop("sl")}
	cl := &correctingClaims{
		fakeClaims: &fakeClaims{ids: map[string]bool{"sl": true}},
		at:         map[string]time.Time{"sl": time.Now().Add(-48 * time.Hour)},
	}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, Claims: cl})

	r.sweepInventory(context.Background())

	if got := cl.forgot(); len(got) != 1 || got[0] != "sl" {
		t.Fatalf("forgotten=%v", got)
	}
	if f.CallCount("Claim:sl") != 1 {
		t.Fatalf("calls=%v", f.Calls)
	}
	if got := cl.recorded(); len(got) != 1 || got[0] != "acc:sl:" {
		t.Fatalf("recorded=%v", got)
	}
}

// A stale row whose claim Twitch refuses again stays forgotten, so the drop
// reads claimable, and the drop is skipped until a later sweep claims it.
func TestInventorySweep_StaleClaimRefusedStaysUnclaimed(t *testing.T) {
	noSweepGap(t)
	f := &refusingFake{Fake: platformtest.NewFake(), refuse: map[string]bool{"sl": true}}
	f.Ready = []platform.ReadyDrop{readyDrop("sl")}
	cl := &correctingClaims{
		fakeClaims: &fakeClaims{ids: map[string]bool{"sl": true}},
		at:         map[string]time.Time{"sl": time.Now().Add(-48 * time.Hour)},
	}
	sk := &fakeSkips{}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, Claims: cl, Skips: sk})

	r.sweepInventory(context.Background())

	if got := cl.forgot(); len(got) != 1 {
		t.Fatalf("forgotten=%v", got)
	}
	if got := cl.recorded(); len(got) != 0 {
		t.Fatalf("a refused claim was recorded: %v", got)
	}
	r.mu.Lock()
	skipped := r.skipped["sl"]
	r.mu.Unlock()
	if !skipped || len(sk.recorded) != 1 {
		t.Fatalf("skipped=%v persisted=%v", skipped, sk.recorded)
	}

	// The next sweep claims it and lifts the skip.
	f.refuse = nil
	r.sweepInventory(context.Background())
	if got := cl.recorded(); len(got) != 1 || got[0] != "acc:sl:" {
		t.Fatalf("recorded=%v", got)
	}
	r.mu.Lock()
	skipped = r.skipped["sl"]
	r.mu.Unlock()
	if skipped || len(sk.cleared) != 1 {
		t.Fatalf("skipped=%v cleared=%v", skipped, sk.cleared)
	}
}

// A row younger than staleClaimGrace (inventory lag), a row marked by hand
// and a logged drop without a corrector are left alone.
func TestInventorySweep_LeavesRecentManualAndUncorrectableRows(t *testing.T) {
	noSweepGap(t)
	f := platformtest.NewFake()
	f.Ready = []platform.ReadyDrop{readyDrop("recent"), readyDrop("manual")}
	cl := &correctingClaims{
		fakeClaims: &fakeClaims{ids: map[string]bool{"recent": true, "manual": true}},
		at: map[string]time.Time{
			"recent": time.Now().Add(-time.Minute),
			"manual": time.Now().Add(-48 * time.Hour),
		},
		manual: map[string]bool{"manual": true},
	}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, Claims: cl})
	r.sweepInventory(context.Background())
	if len(cl.forgot()) != 0 || f.CallCount("Claim:recent") != 0 || f.CallCount("Claim:manual") != 0 {
		t.Fatalf("forgotten=%v calls=%v", cl.forgot(), f.Calls)
	}

	plain := &fakeClaims{ids: map[string]bool{"recent": true}}
	f2 := platformtest.NewFake()
	f2.Ready = []platform.ReadyDrop{readyDrop("recent")}
	New(Config{AccountID: "acc", Platform: "twitch", Backend: f2, Games: rustGames, Claims: plain}).
		sweepInventory(context.Background())
	if f2.CallCount("Claim:recent") != 0 {
		t.Fatalf("calls=%v", f2.Calls)
	}
}

// The drop being mined is left to the runner's own claim, and an unlinked
// campaign's drop is not attempted.
func TestInventorySweep_SkipsCurrentAndUnlinked(t *testing.T) {
	noSweepGap(t)
	f := platformtest.NewFake()
	unlinked := readyDrop("u")
	unlinked.Campaign.AccountLinkChecked = true
	unlinked.Campaign.AccountLinkURL = "https://publisher.example/link"
	f.Ready = []platform.ReadyDrop{readyDrop("cur"), unlinked}
	r := New(Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames})
	cur := readyDrop("cur")
	r.mu.Lock()
	r.current = &plan.Target{Campaign: cur.Campaign, Benefit: cur.Benefit}
	r.mu.Unlock()
	r.sweepInventory(context.Background())
	if f.CallCount("Claim:cur") != 0 || f.CallCount("Claim:u") != 0 {
		t.Fatalf("calls=%v", f.Calls)
	}
}

// kickLike is a backend with the Kick sweep and no ReadyDrops.
type kickLike struct {
	platform.Backend
	sweeps int
}

func (k *kickLike) SweepCompletedClaims(context.Context, platform.Session) ([]platform.ClaimedReward, error) {
	k.sweeps++
	return nil, nil
}

func TestInventorySweep_KickUsesCompletedSweeper(t *testing.T) {
	k := &kickLike{Backend: platformtest.NewFake()}
	r := New(Config{AccountID: "acc", Platform: "kick", Backend: k, Games: rustGames})
	r.sweepInventory(context.Background())
	if k.sweeps != 1 {
		t.Fatalf("sweeps=%d", k.sweeps)
	}
}

// The sweep summary is not a claim: it stays off the Claims filter of the
// activity log and still shows under All.
func TestInventorySweep_DoneLineIsNotAClaim(t *testing.T) {
	ring := mlog.NewRing(50)
	prev := slog.Default()
	slog.SetDefault(mlog.NewWithRing(io.Discard, "info", ring))
	t.Cleanup(func() { slog.SetDefault(prev) })

	k := &kickLike{Backend: platformtest.NewFake()}
	New(Config{AccountID: "acc", Platform: "kick", Backend: k, Games: rustGames}).sweepInventory(context.Background())

	var done *mlog.LogLine
	for _, l := range ring.Snapshot() {
		if l.Msg == "runner inventory sweep done" {
			done = &l
		}
	}
	if done == nil {
		t.Fatal("no sweep done line")
	}
	if done.Kind == mlog.KindClaim {
		t.Fatalf("sweep done kind=%q, want not claim", done.Kind)
	}
}

// Run sweeps at start when InventorySweep is on, and not inside the sleep
// window.
func TestInventorySweep_RunsAtStartOutsideSleepWindow(t *testing.T) {
	noSweepGap(t)
	run := func(asleep bool) *platformtest.Fake {
		f := platformtest.NewFake()
		f.Ready = []platform.ReadyDrop{readyDrop("a")}
		cfg := Config{AccountID: "acc", Platform: "twitch", Backend: f, Games: rustGames, InventorySweep: true}
		if asleep {
			cfg.SleepUntil = func(now time.Time) (time.Time, bool) { return now.Add(time.Hour), true }
		}
		r := New(cfg)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = r.Run(ctx)
		}()
		deadline := time.Now().Add(2 * time.Second)
		for !asleep && f.CallCount("Claim:a") == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if asleep {
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
		<-done
		return f
	}
	if f := run(false); f.CallCount("Claim:a") != 1 {
		t.Fatalf("awake: calls=%v", f.Calls)
	}
	if f := run(true); f.CallCount("ReadyDrops") != 0 {
		t.Fatalf("asleep: calls=%v", f.Calls)
	}
}
