package plan

import (
	"reflect"
	"testing"
	"time"

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

// Twitch leaves isAccountConnected false on campaigns that need no link, with
// an empty or twitch.tv accountLinkURL: those are mined, not blocked.
func TestBuild_TwitchUnconnectedWithoutPublisherLinkMines(t *testing.T) {
	for _, link := range []string{"", "https://www.twitch.tv/drops/campaigns?dropID=0f3c2a71-5b1e-4f0e-9d0a-6a2f1c9b8e47"} {
		tw := camp("tw", "twitch", "Rust", ben("t1", 10))
		tw.AccountLinked, tw.AccountLinkURL = false, link
		res := Build(Input{Games: rust, Campaigns: []platform.Campaign{tw}})
		eq(t, ids(res.Queue), []string{"t1"})
		if len(res.Blocked) != 0 {
			t.Fatalf("link %q: blocked %+v", link, res.Blocked)
		}
	}
}

func TestBuild_BlockedReasons(t *testing.T) {
	tw := camp("tw", "twitch", "Rust", ben("t1", 10))
	tw.AccountLinked = false
	tw.AccountLinkURL = "https://account.riotgames.com/#connected-accounts"
	res := Build(Input{Games: rust, Campaigns: []platform.Campaign{tw}, NotLinked: map[string]bool{"t1": true}})
	if len(res.Queue) != 0 || len(res.Blocked) != 1 || res.Blocked[0].Reason != ReasonNotLinked {
		t.Fatalf("refused: %+v", res)
	}

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

// A game Twitch renamed keeps its stored name for Kick and matches Twitch
// campaigns by its Twitch name.
func TestBuild_GameMatchTwitchName(t *testing.T) {
	cs2 := []Game{{Name: "Counter-Strike 2", Slug: "counter-strike-2", TwitchName: "Counter-Strike"}}
	res := Build(Input{Games: cs2, Campaigns: []platform.Campaign{
		camp("t", "twitch", "Counter-Strike", ben("d1", 10)),
		camp("k", "kick", "Counter-Strike 2", ben("d2", 10)),
	}})
	eq(t, ids(res.Queue), []string{"d1", "d2"})
	if _, ok := Match([]Game{{Name: "Rust", Slug: "rust"}}, ""); ok {
		t.Fatal("empty Twitch name matched an empty game")
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

func claimedIDs(r Result) []string { return ids(r.Claimed) }

func TestBuild_InventoryPartialOverridesLog(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("b1", 60))},
		Claimed:   map[string]bool{"b1": true},
		Progress:  []platform.Progress{{BenefitID: "b1", MinutesWatched: 20}},
	})
	eq(t, ids(res.Queue), []string{"b1"})
	if res.Queue[0].Minutes != 20 {
		t.Fatalf("minutes %d want 20", res.Queue[0].Minutes)
	}
	eq(t, claimedIDs(res), []string{})
}

func TestBuild_MarkedClaimBeatsPartialInventory(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("b1", 60))},
		Claimed:   map[string]bool{"b1": true},
		Marked:    map[string]bool{"b1": true},
		Progress:  []platform.Progress{{BenefitID: "b1", MinutesWatched: 20}},
	})
	eq(t, ids(res.Queue), []string{})
	eq(t, claimedIDs(res), []string{"b1"})
}

func TestBuild_LogClaimSurvivesFullUnclaimedRow(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("b1", 60))},
		Claimed:   map[string]bool{"b1": true},
		Progress:  []platform.Progress{{BenefitID: "b1", MinutesWatched: 60}},
	})
	eq(t, ids(res.Queue), []string{})
	eq(t, claimedIDs(res), []string{"b1"})
}

func TestBuild_LogClaimSurvivesAbsentRow(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("b1", 60))},
		Claimed:   map[string]bool{"b1": true},
	})
	eq(t, claimedIDs(res), []string{"b1"})
	if res.Claimed[0].Minutes != 60 {
		t.Fatalf("minutes %d want 60", res.Claimed[0].Minutes)
	}
}

func TestBuild_InventoryClaimedListed(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("b1", 60))},
		Progress:  []platform.Progress{{BenefitID: "b1", MinutesWatched: 60, Claimed: true}},
	})
	eq(t, claimedIDs(res), []string{"b1"})
	eq(t, ids(res.Queue), []string{})
}

func TestBuild_ClaimedOnlyChosenActive(t *testing.T) {
	up := camp("up", "kick", "Rust", ben("b2", 60))
	up.Status = "upcoming"
	res := Build(Input{
		Games: rust,
		Campaigns: []platform.Campaign{
			camp("ow", "kick", "Overwatch", ben("b1", 60)),
			up,
			camp("c", "kick", "Rust", ben("b3", 60)),
		},
		Claimed: map[string]bool{"b1": true, "b2": true, "b3": true},
	})
	eq(t, claimedIDs(res), []string{"b3"})
}

func TestBuild_PreconditionUsesOverride(t *testing.T) {
	b2 := ben("b2", 60)
	b2.Preconditions = []string{"b1"}
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c", "kick", "Rust", ben("b1", 60), b2)},
		Claimed:   map[string]bool{"b1": true},
		Progress:  []platform.Progress{{BenefitID: "b1", MinutesWatched: 10}},
	})
	eq(t, ids(res.Queue), []string{"b1"})
	if len(res.Blocked) != 1 || res.Blocked[0].Benefit.ID != "b2" || res.Blocked[0].Reason != ReasonPrecondition {
		t.Fatalf("blocked %+v", res.Blocked)
	}
}

func TestBuild_NotLinkedBenefitBlocked(t *testing.T) {
	res := Build(Input{
		Games:     rust,
		Campaigns: []platform.Campaign{camp("c1", "kick", "Rust", ben("d1", 30), ben("d2", 60))},
		NotLinked: map[string]bool{"d1": true},
	})
	eq(t, ids(res.Queue), []string{"d2"})
	if len(res.Blocked) != 1 || res.Blocked[0].Benefit.ID != "d1" || res.Blocked[0].Reason != ReasonNotLinked {
		t.Fatalf("blocked=%+v", res.Blocked)
	}
}

// A campaign the platform reports unlinked is still mined; it is listed in
// NeedsLink so the UI can ask the user to link before the claim.
func TestBuild_UnlinkedCampaignMinesAndNeedsLink(t *testing.T) {
	tw := camp("tw", "twitch", "Rust", ben("t1", 10))
	tw.AccountLinked = false
	tw.AccountLinkURL = "https://account.riotgames.com/#connected-accounts"
	k := camp("k", "kick", "Rust", ben("k1", 20))
	k.AccountLinked = false
	res := Build(Input{Games: rust, Campaigns: []platform.Campaign{tw, k}})
	eq(t, ids(res.Queue), []string{"t1", "k1"})
	eq(t, ids(res.NeedsLink), []string{"t1", "k1"})
	if len(res.Blocked) != 0 {
		t.Fatalf("blocked %+v", res.Blocked)
	}
}

func TestBuild_EndingSoonestIgnoresGameRank(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	at := func(c platform.Campaign, d time.Duration) platform.Campaign {
		c.EndsAt = now.Add(d)
		return c
	}
	games := []Game{{Name: "Rust"}, {Name: "Overwatch"}}
	camps := []platform.Campaign{
		camp("noend", "kick", "Rust", ben("n1", 10)),
		at(camp("late", "kick", "Rust", ben("l1", 10)), 10*24*time.Hour),
		at(camp("soon", "kick", "Overwatch", ben("s1", 10)), 24*time.Hour),
	}
	res := Build(Input{Games: games, Campaigns: camps})
	eq(t, ids(res.Queue), []string{"n1", "l1", "s1"})
	res = Build(Input{Games: games, Campaigns: camps, Priority: PriorityEndingSoonest})
	eq(t, ids(res.Queue), []string{"s1", "l1", "n1"})
	res = Build(Input{Games: games, Campaigns: camps, Priority: PriorityEndingSoonest, Order: []string{"n1"}})
	eq(t, ids(res.Queue), []string{"n1", "s1", "l1"})
}

func TestBuild_EndingSoonestTiesByRemainingThenRank(t *testing.T) {
	end := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	mk := func(id, game string, min int) platform.Campaign {
		c := camp(id, "kick", game, ben(id+"1", min))
		c.EndsAt = end
		return c
	}
	res := Build(Input{
		Games:     []Game{{Name: "Rust"}, {Name: "Overwatch"}},
		Campaigns: []platform.Campaign{mk("a", "Overwatch", 60), mk("b", "Rust", 60), mk("c", "Overwatch", 30)},
		Priority:  PriorityEndingSoonest,
	})
	eq(t, ids(res.Queue), []string{"c1", "b1", "a1"})
}
