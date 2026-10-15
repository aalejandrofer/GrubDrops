package planner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

var now = time.Unix(1_700_000_000, 0)

func camp(id, game string, ends time.Time, drops ...platform.DropBenefit) platform.Campaign {
	return platform.Campaign{ID: id, Game: game, Status: "active", EndsAt: ends, Benefits: drops}
}

func drop(id, campaign string, req int, pre ...string) platform.DropBenefit {
	return platform.DropBenefit{ID: id, CampaignID: campaign, RequiredMinutes: req, Preconditions: pre}
}

func rows(rs ...dropstate.Row) map[string]dropstate.Row {
	m := map[string]dropstate.Row{}
	for _, r := range rs {
		m[r.DropID] = r
	}
	return m
}

func st(id string, s dropstate.Status) dropstate.Row { return dropstate.Row{DropID: id, Status: s} }

func stream(ch string, viewers int) platform.Stream {
	return platform.Stream{Channel: ch, ViewerCount: viewers}
}

func TestPlan_PicksHighestViewerByDefault(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("small", 5), stream("big", 500)}},
	}
	d := Plan(in)
	assert.Equal(t, Mine, d.Kind)
	assert.Equal(t, "big", d.Channel.Channel)
	assert.Equal(t, []string{"d1"}, d.Serves)
}

func TestPlan_PriorityStreamerFirst(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("big", 500), stream("Fav2", 1), stream("fav1", 2)}},
		Settings:  Settings{StreamerPriority: []string{"fav1", "fav2"}},
	}
	assert.Equal(t, "fav1", Plan(in).Channel.Channel)
	in.Live["c1"] = []platform.Stream{stream("big", 500), stream("Fav2", 1)}
	assert.Equal(t, "Fav2", Plan(in).Channel.Channel, "case-insensitive, next priority")
	in.Live["c1"] = []platform.Stream{stream("big", 500)}
	assert.Equal(t, "big", Plan(in).Channel.Channel, "fallback to category")
}

func TestPlan_GameRankThenEndingSoonest(t *testing.T) {
	soon, late := now.Add(time.Hour), now.Add(48*time.Hour)
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("late", "A", late, drop("d1", "late", 60)), camp("soon", "B", soon, drop("d2", "soon", 60))},
		Rows:      rows(st("d1", dropstate.Eligible), st("d2", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"late": {stream("x", 1)}, "soon": {stream("y", 1)}},
		Settings:  Settings{GameRank: func(g string) int { return map[string]int{"A": 0, "B": 1}[g] }},
	}
	assert.Equal(t, "late", Plan(in).CampaignID, "ordered: game rank wins")
	in.Settings.PriorityMode = "ending_soonest"
	assert.Equal(t, "soon", Plan(in).CampaignID)
}

func TestPlan_SkipsNonMineableAndLockedPreconditions(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{},
			drop("claimed", "c1", 30), drop("locked", "c1", 60, "gate"), drop("gate", "c1", 10))},
		Rows: rows(st("claimed", dropstate.Claimed), st("locked", dropstate.Eligible), st("gate", dropstate.Blocked)),
		Live: map[string][]platform.Stream{"c1": {stream("x", 1)}},
	}
	assert.Equal(t, Idle, Plan(in).Kind, "gate is blocked, locked waits on it")

	in.Rows["gate"] = dropstate.Row{DropID: "gate", Status: dropstate.Claimed, Source: dropstate.FromUser}
	d := Plan(in)
	assert.Equal(t, Mine, d.Kind, "user-marked precondition unlocks (P9)")
	assert.Equal(t, []string{"locked"}, d.Serves)
}

func TestPlan_ServesDropsAcrossCampaignsOnSameChannel(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{
			camp("c1", "Rust", time.Time{}, drop("r1", "c1", 60), drop("r2", "c1", 30)),
			camp("c2", "Rust", time.Time{}, drop("r3", "c2", 120)),
		},
		Rows: rows(st("r1", dropstate.Eligible), st("r2", dropstate.Accruing), st("r3", dropstate.Eligible)),
		Live: map[string][]platform.Stream{"c1": {stream("oilrats", 9)}, "c2": {stream("OILRATS", 9)}},
	}
	d := Plan(in)
	assert.Equal(t, []string{"r2", "r1", "r3"}, d.Serves, "lowest tier first, then other campaigns")
}

func TestPlan_CooldownSkipsChannel(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("big", 500), stream("small", 5)}},
		Cooldowns: map[string]time.Time{"big": now.Add(time.Minute)},
	}
	assert.Equal(t, "small", Plan(in).Channel.Channel)
}

func TestPlan_HysteresisHoldsThenBypassesDeadChannel(t *testing.T) {
	cur := Decision{Kind: Mine, CampaignID: "c1", Channel: stream("big", 500), Serves: []string{"d1"}}
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("big", 500), stream("fav", 1)}},
		Settings:  Settings{StreamerPriority: []string{"fav"}},
		Current:   &cur, LastSwap: now.Add(-time.Minute),
	}
	assert.Equal(t, "big", Plan(in).Channel.Channel, "inside hold: keep current")
	in.LastSwap = now.Add(-SwapHold - time.Second)
	assert.Equal(t, "fav", Plan(in).Channel.Channel, "hold over: priority streamer wins")
	in.LastSwap = now.Add(-time.Minute)
	in.Live["c1"] = []platform.Stream{stream("fav", 1)}
	assert.Equal(t, "fav", Plan(in).Channel.Channel, "current went offline: bypass hold")
}

func TestPlan_ForceWatchOnlyWhenNothingToMine(t *testing.T) {
	in := Input{Now: now, Settings: Settings{ForceWatch: []platform.Stream{stream("pts", 1)}}}
	d := Plan(in)
	assert.Equal(t, ForceWatch, d.Kind)
	assert.Equal(t, "pts", d.Channel.Channel)

	in.Settings.ForceWatch = nil
	d = Plan(in)
	assert.Equal(t, Idle, d.Kind)
	assert.Equal(t, ReasonNothingToMine, d.Reason)
}

func TestPlan_NoLiveChannelsIdleReason(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
	}
	assert.Equal(t, ReasonNoLiveChannels, Plan(in).Reason)
}

func TestPlan_OutsideWindowIgnored(t *testing.T) {
	c := camp("c1", "G", now.Add(-time.Minute), drop("d1", "c1", 60))
	in := Input{Now: now, Campaigns: []platform.Campaign{c}, Rows: rows(st("d1", dropstate.Eligible)),
		Live: map[string][]platform.Stream{"c1": {stream("x", 1)}}}
	assert.Equal(t, Idle, Plan(in).Kind)
}

func TestDecision_Same(t *testing.T) {
	a := Decision{Kind: Mine, Channel: stream("Big", 1)}
	assert.True(t, a.Same(Decision{Kind: Mine, Channel: stream("big", 9)}))
	assert.False(t, a.Same(Decision{Kind: ForceWatch, Channel: stream("big", 9)}))
	assert.True(t, Decision{Kind: Idle}.Same(Decision{Kind: Idle, Reason: "x"}))
}
