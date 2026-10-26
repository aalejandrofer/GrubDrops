package collect

import (
	"errors"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
)

var (
	now  = time.Unix(1_700_000_000, 0)
	ends = now.Add(48 * time.Hour)
)

func target(id string, minutes int) plan.Target {
	return plan.Target{
		Campaign: platform.Campaign{ID: "c1", Platform: "twitch", Game: "Rust", Name: "Camp", EndsAt: ends},
		Benefit:  platform.DropBenefit{ID: id, CampaignID: "c1", Name: "Name " + id, RequiredMinutes: 60, ImageURL: "img/" + id},
		Minutes:  minutes,
	}
}

func TestDerive_States(t *testing.T) {
	s := runner.Scan{
		AccountID: "a1", Platform: "twitch",
		Result: plan.Result{
			Queue:   []plan.Target{target("q0", 0), target("q30", 30), target("q60", 60)},
			Blocked: []plan.Blocked{{Target: target("bl", 0), Reason: plan.ReasonNotLinked}},
			Claimed: []plan.Target{target("cl", 60)},
		},
	}
	rows := Derive(s, now)
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	want := []struct {
		id, state, reason string
		minutes           int
	}{
		{"q0", StateEligible, "", 0},
		{"q30", StateAccruing, "", 30},
		{"q60", StateClaimable, "", 60},
		{"bl", StateBlocked, "not_linked", 0},
		{"cl", StateClaimed, "", 60},
	}
	for i, w := range want {
		r := rows[i]
		if r.BenefitID != w.id || r.State != w.state || r.Reason != w.reason || r.Minutes != w.minutes {
			t.Errorf("row %d = %+v, want %+v", i, r, w)
		}
		if r.AccountID != "a1" || r.Platform != "twitch" || r.Game != "Rust" || r.CampaignID != "c1" ||
			r.CampaignName != "Camp" || r.BenefitName != "Name "+w.id || r.ImageURL != "img/"+w.id ||
			r.RequiredMinutes != 60 || !r.EndsAt.Equal(ends) || !r.UpdatedAt.Equal(now) {
			t.Errorf("row %d fields = %+v", i, r)
		}
	}
}

func TestDerive_ErrScanEmpty(t *testing.T) {
	s := runner.Scan{AccountID: "a1", Err: errors.New("boom"),
		Result: plan.Result{Queue: []plan.Target{target("q", 0)}}}
	if got := Derive(s, now); got != nil {
		t.Fatalf("Derive = %v, want nil", got)
	}
	if got := Found(s, now); got != nil {
		t.Fatalf("Found = %v, want nil", got)
	}
}

func TestFound_OnlyInventoryClaimsNotLogged(t *testing.T) {
	s := runner.Scan{
		AccountID: "a1", Platform: "twitch",
		Progress: []platform.Progress{
			{BenefitID: "b1", Claimed: true},
			{BenefitID: "b2", Claimed: true},
		},
		Logged: map[string]bool{"b2": true, "b3": true},
		Result: plan.Result{Claimed: []plan.Target{target("b1", 60), target("b2", 60), target("b3", 60)}},
	}
	got := Found(s, now)
	if len(got) != 1 {
		t.Fatalf("Found = %+v, want 1 entry", got)
	}
	e := got[0]
	if e.BenefitID != "b1" || e.Source != "inventory" || !e.ClaimedAt.Equal(now) ||
		e.AccountID != "a1" || e.Platform != "twitch" || e.Game != "Rust" || e.CampaignID != "c1" ||
		e.CampaignName != "Camp" || e.BenefitName != "Name b1" || e.ImageURL != "img/b1" {
		t.Errorf("entry = %+v", e)
	}
}

func TestStateFor(t *testing.T) {
	for _, c := range []struct {
		m, r int
		want string
	}{{0, 60, StateEligible}, {1, 60, StateAccruing}, {59, 60, StateAccruing}, {60, 60, StateClaimable}, {90, 60, StateClaimable}} {
		if got := StateFor(c.m, c.r); got != c.want {
			t.Errorf("StateFor(%d,%d) = %s, want %s", c.m, c.r, got, c.want)
		}
	}
}
