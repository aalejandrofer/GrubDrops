package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

var errLink = fmt.Errorf("kick claim: %w", platform.ErrClaimNeedsLink)

func blockedReason(r *Runner, id string) plan.Reason {
	for _, b := range r.Snapshot().Blocked {
		if b.Benefit.ID == id {
			return b.Reason
		}
	}
	return ""
}

func TestClaim_LinkRefusalBlocksAndMovesOn(t *testing.T) {
	f := platformtest.NewFake()
	r, _, cl, sk := claimSetup(f, 2, 2)
	toClaiming(t, r)
	f.ClaimErr = errLink
	for i := 0; i < claimFailSkipThreshold+1; i++ {
		if i > 0 {
			r.setState(context.Background(), StateClaiming)
			r.mu.Lock()
			r.current = &plan.Target{Campaign: f.Campaigns[0], Benefit: f.Campaigns[0].Benefits[0]}
			r.mu.Unlock()
		}
		if err := r.claim(context.Background()); err != nil {
			t.Fatalf("attempt %d: link refusal must not be a step error: %v", i, err)
		}
	}
	if len(sk.recorded) != 0 || len(r.claimFailures) != 0 {
		t.Fatalf("link refusal must not count or skip: skips=%v failures=%v", sk.recorded, r.claimFailures)
	}
	if r.State() != StatePicking || r.Snapshot().Current != nil {
		t.Fatalf("state=%s current=%+v", r.State(), r.Snapshot().Current)
	}
	n := f.CallCount("Claim:d1")
	steps(t, r, 1) // pick
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "d2" {
		t.Fatalf("current=%+v, want d2", cur)
	}
	if got := blockedReason(r, "d1"); got != plan.ReasonNotLinked {
		t.Fatalf("d1 reason=%q", got)
	}
	if c := f.CallCount("Claim:d1"); c != n {
		t.Fatalf("pick right after a refusal must not retry: claims %d -> %d", n, c)
	}
	if len(cl.recorded()) != 0 {
		t.Fatalf("rec=%v", cl.recorded())
	}
}

func TestClaim_LinkRefusalOnlyDropAwaitsConnect(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, _ := claimSetup(f, 2)
	toClaiming(t, r)
	f.ClaimErr = errLink
	if err := r.claim(context.Background()); err != nil {
		t.Fatal(err)
	}
	steps(t, r, 1)
	if r.State() != StateAwaitingConnect {
		t.Fatalf("state=%s", r.State())
	}
}

func TestScan_RetriesLinkRefusalAfterInterval(t *testing.T) {
	f := platformtest.NewFake()
	r, n, cl, _ := claimSetup(f, 2, 2)
	toClaiming(t, r)
	f.ClaimErr = errLink
	if err := r.claim(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The user linked; the throttle window passed.
	f.ClaimErr = nil
	r.mu.Lock()
	r.linkRefused["d1"] = time.Now().Add(-recheckInterval - time.Second)
	r.mu.Unlock()
	steps(t, r, 1)
	if c := f.CallCount("Claim:d1"); c != 2 {
		t.Fatalf("claims=%d", c)
	}
	if len(cl.recorded()) != 1 || cl.recorded()[0] != "acc:d1:" {
		t.Fatalf("rec=%v", cl.recorded())
	}
	if got := blockedReason(r, "d1"); got != "" {
		t.Fatalf("d1 still blocked: %q", got)
	}
	if len(r.linkRefused) != 0 {
		t.Fatalf("linkRefused=%v", r.linkRefused)
	}
	if cn := n.of("claim"); len(cn) != 1 || cn[0].fields["drop"] != "Drop1" {
		t.Fatalf("notify=%+v", cn)
	}
}

// After a restart the runner has no memory; the backend's persisted
// ClaimNeedsLink flag on a completed drop triggers one retry, then a block.
func TestScan_CampaignFlagBlocksCompletedDrop(t *testing.T) {
	f := platformtest.NewFake()
	r, _, _, _ := claimSetup(f, 2, 5)
	f.Campaigns[0].ClaimNeedsLink = true
	f.Progress["d1"] = &platform.Progress{BenefitID: "d1", MinutesWatched: 2}
	f.ClaimErr = errLink
	steps(t, r, 1)
	if c := f.CallCount("Claim:d1"); c != 1 {
		t.Fatalf("claims=%d", c)
	}
	if got := blockedReason(r, "d1"); got != plan.ReasonNotLinked {
		t.Fatalf("d1 reason=%q", got)
	}
	if cur := r.Snapshot().Current; cur == nil || cur.Benefit.ID != "d2" {
		t.Fatalf("current=%+v; the unfinished drop keeps mining", cur)
	}
	r.setState(context.Background(), StatePicking)
	steps(t, r, 1)
	if c := f.CallCount("Claim:d1"); c != 1 {
		t.Fatalf("throttled retry ran: claims=%d", c)
	}
}

type recheckFake struct {
	*platformtest.Fake
	claimed []platform.ClaimedReward
	still   int
	calls   int
}

func (f *recheckFake) RecheckLinks(context.Context, platform.Session) ([]platform.ClaimedReward, int, error) {
	f.calls++
	return f.claimed, f.still, nil
}

func TestRunner_RecheckLinks(t *testing.T) {
	f := platformtest.NewFake()
	r, n, _, _ := claimSetup(f, 2)
	rf := &recheckFake{Fake: f, claimed: []platform.ClaimedReward{{Title: "Drop1"}}}
	r.cfg.Backend = rf
	r.linkRefused = map[string]time.Time{"d1": time.Now()}
	got, err := r.RecheckLinks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (LinkRecheck{Claimed: 1}) || rf.calls != 1 {
		t.Fatalf("got=%+v calls=%d", got, rf.calls)
	}
	if len(r.linkRefused) != 0 || !r.forceScan.Load() {
		t.Fatalf("linkRefused=%v forceScan=%v", r.linkRefused, r.forceScan.Load())
	}
	if len(n.of("claim")) != 1 {
		t.Fatalf("notify=%+v", n.of("claim"))
	}
}
