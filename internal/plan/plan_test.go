package plan

import (
	"reflect"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func camp(id, plat, game string, bs ...platform.DropBenefit) platform.Campaign {
	return platform.Campaign{ID: id, Platform: plat, Game: game, Status: "active", Benefits: bs, AccountLinked: true, AccountLinkChecked: true}
}

func ben(id string, min int) platform.DropBenefit {
	return platform.DropBenefit{ID: id, RequiredMinutes: min}
}

func ids(q []Target) []string {
	out := []string{}
	for _, t := range q {
		out = append(out, t.Benefit.ID)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

var rust = []Game{{Name: "Rust", Slug: "rust"}}

func TestBuild_GameRankThenRestrictedThenRemaining(t *testing.T) {
	team := camp("rust-team", "kick", "Rust", ben("d2", 120))
	team.AllowedChannels = []string{"x"}
	res := Build(Input{
		Games: []Game{{Name: "Rust", Slug: "rust"}, {Name: "Overwatch", Slug: "overwatch"}},
		Campaigns: []platform.Campaign{
			camp("ow-open", "kick", "Overwatch", ben("ow1", 10)),
			camp("rust-open", "kick", "Rust", ben("d1", 60)),
			team,
		},
		Progress: []platform.Progress{{BenefitID: "d1", MinutesWatched: 50}},
	})
	eq(t, ids(res.Queue), []string{"d2", "d1", "ow1"})
}

func TestBuild_RemainingBreaksTies(t *testing.T) {
	res := Build(Input{
		Games: rust,
		Campaigns: []platform.Campaign{
			camp("a", "kick", "Rust", ben("a1", 100)),
			camp("b", "kick", "Rust", ben("b1", 100)),
		},
		Progress: []platform.Progress{{BenefitID: "b1", MinutesWatched: 90}},
	})
	eq(t, ids(res.Queue), []string{"b1", "a1"})
}

func TestBuild_BenefitsByRequiredMinutes(t *testing.T) {
	res := Build(Input{Games: rust, Campaigns: []platform.Campaign{
		camp("c", "kick", "Rust", ben("x120", 120), ben("x30", 30), ben("x60", 60)),
	}})
	eq(t, ids(res.Queue), []string{"x30", "x60", "x120"})
}

func TestBuild_DropsClaimedZeroMinuteRewardAndInactive(t *testing.T) {
	reward := camp("rw", "kick", "Rust", ben("rw1", 10))
	reward.Kind = "reward"
	up := camp("up", "kick", "Rust", ben("up1", 10))
	up.Status = "upcoming"
	res := Build(Input{
		Games: rust,
		Campaigns: []platform.Campaign{
			camp("c", "kick", "Rust", ben("inv", 10), ben("log", 10), ben("zero", 0), ben("ok", 10)),
			reward, up,
		},
		Progress: []platform.Progress{{BenefitID: "inv", Claimed: true}},
		Claimed:  map[string]bool{"log": true},
	})
	eq(t, ids(res.Queue), []string{"ok"})
	if len(res.Blocked) != 0 {
		t.Fatalf("blocked %v", res.Blocked)
	}
}

func TestBuild_BlockedReasons(t *testing.T) {
	tw := camp("tw", "twitch", "Rust", ben("t1", 10))
	tw.AccountLinked = false
	res := Build(Input{Games: rust, Campaigns: []platform.Campaign{tw}})
	if len(res.Queue) != 0 || len(res.Blocked) != 1 || res.Blocked[0].Reason != ReasonNotLinked {
		t.Fatalf("unlinked: %+v", res)
	}
	res = Build(Input{Games: rust, Campaigns: []platform.Campaign{tw}, ForceLinked: map[string]bool{"tw": true}})
	eq(t, ids(res.Queue), []string{"t1"})

	k := camp("k", "kick", "Rust", ben("k1", 10))
	k.AccountLinked, k.AccountLinkChecked = false, false
	res = Build(Input{Games: rust, Campaigns: []platform.Campaign{k}})
	eq(t, ids(res.Queue), []string{"k1"})

	res = Build(Input{Games: rust, Campaigns: []platform.Campaign{k}, Skipped: map[string]bool{"k1": true}})
	if len(res.Queue) != 0 || len(res.Blocked) != 1 || res.Blocked[0].Reason != ReasonSkipped {
		t.Fatalf("skipped: %+v", res)
	}

	p := ben("p1", 10)
	p.Preconditions = []string{"p"}
	pc := camp("pc", "kick", "Rust", p)
	res = Build(Input{Games: rust, Campaigns: []platform.Campaign{pc}})
	if len(res.Queue) != 0 || len(res.Blocked) != 1 || res.Blocked[0].Reason != ReasonPrecondition {
		t.Fatalf("precondition: %+v", res)
	}
	res = Build(Input{Games: rust, Campaigns: []platform.Campaign{pc}, Claimed: map[string]bool{"p": true}})
	eq(t, ids(res.Queue), []string{"p1"})
}

func TestBuild_GameMatchLenient(t *testing.T) {
	for _, g := range []string{"rust", "RUST", "Rust"} {
		res := Build(Input{Games: rust, Campaigns: []platform.Campaign{camp("c", "kick", g, ben("d", 10))}})
		if len(res.Queue) != 1 {
			t.Fatalf("%q not matched", g)
		}
	}
	res := Build(Input{Games: rust, Campaigns: []platform.Campaign{camp("c", "kick", "Rusty", ben("d", 10))}})
	if len(res.Queue) != 0 {
		t.Fatal("Rusty matched")
	}
}

func TestBuild_DuplicateBenefitQueuedOnce(t *testing.T) {
	res := Build(Input{Games: rust, Campaigns: []platform.Campaign{
		camp("a", "kick", "Rust", ben("d1", 10)),
		camp("b", "kick", "Rust", ben("d1", 10)),
	}})
	eq(t, ids(res.Queue), []string{"d1"})
}

func TestApplyOrder_MovesNamedFirst(t *testing.T) {
	q := []Target{{Benefit: ben("a", 1)}, {Benefit: ben("b", 1)}, {Benefit: ben("c", 1)}}
	eq(t, ids(ApplyOrder(q, []string{"c", "a"})), []string{"c", "a", "b"})
}

func TestApplyOrder_UnknownIDsIgnored(t *testing.T) {
	q := []Target{{Benefit: ben("a", 1)}, {Benefit: ben("b", 1)}}
	eq(t, ids(ApplyOrder(q, []string{"zz", "b"})), []string{"b", "a"})
}

func TestBuild_TargetMinutesFromInventory(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("d1", 60))},
		Progress:  []platform.Progress{{BenefitID: "d1", MinutesWatched: 17}},
	})
	if res.Queue[0].Minutes != 17 {
		t.Fatalf("minutes %d", res.Queue[0].Minutes)
	}
}

func TestBuild_RestrictedDoesNotJumpHigherGame(t *testing.T) {
	b := camp("b", "kick", "Overwatch", ben("b1", 10))
	b.AllowedChannels = []string{"x"}
	res := Build(Input{
		Games:     []Game{{Name: "Rust", Slug: "rust"}, {Name: "Overwatch", Slug: "overwatch"}},
		Campaigns: []platform.Campaign{b, camp("a", "kick", "Rust", ben("a1", 100))},
	})
	eq(t, ids(res.Queue), []string{"a1", "b1"})
}

func TestBuild_SkippedBenefitDoesNotAffectCampaignRemaining(t *testing.T) {
	res := Build(Input{
		Games: rust,
		Campaigns: []platform.Campaign{
			camp("far", "kick", "Rust", ben("sk", 10), ben("f1", 100)),
			camp("near", "kick", "Rust", ben("n1", 50)),
		},
		Progress: []platform.Progress{{BenefitID: "sk", MinutesWatched: 10}},
		Skipped:  map[string]bool{"sk": true},
	})
	eq(t, ids(res.Queue), []string{"n1", "f1"})
}

func TestBuild_SharedClaimedBenefitKeepsSiblingQueued(t *testing.T) {
	res := Build(Input{
		Games: rust,
		Campaigns: []platform.Campaign{
			camp("a", "kick", "Rust", ben("d1", 30)),
			camp("b", "kick", "Rust", ben("d1", 30), ben("d2", 60)),
		},
		Claimed: map[string]bool{"d1": true},
	})
	eq(t, ids(res.Queue), []string{"d2"})
}
