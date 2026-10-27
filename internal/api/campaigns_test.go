package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

func newCampaignsEnv(t *testing.T) *gamesEnv {
	t.Helper()
	g := newGamesEnv(t)
	g.s.d.Claims = store.NewClaimLog(g.q)
	g.s.routesCampaigns(g.s.protected)
	return g
}

func (g *gamesEnv) campaign(path string) (int, campaignDetailJSON) {
	g.t.Helper()
	resp, b := g.do("GET", path, "", nil)
	var got campaignDetailJSON
	if resp.StatusCode == 200 {
		require.NoError(g.t, json.Unmarshal(b, &got))
	}
	return resp.StatusCode, got
}

func TestCampaign_LiveMergesBoardAndClaims(t *testing.T) {
	g := newCampaignsEnv(t)
	seedClaimAccount(t, g.env, "a1", "Zed")
	seedClaimAccount(t, g.env, "a2", "Amy")
	starts := time.Date(2026, 10, 23, 20, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC)
	c := platform.Campaign{
		ID: "c1", Platform: "twitch", Game: "Rust", GameID: "263490", Name: "Rust Drops", Status: "active",
		StartsAt: starts, EndsAt: ends, AccountLinkURL: "https://link.example/rust",
		Benefits: []platform.DropBenefit{
			{ID: "b2", CampaignID: "c1", Name: "Door", RequiredMinutes: 120, ImageURL: "https://img/b2"},
			{ID: "b1", CampaignID: "c1", Name: "Hoodie", RequiredMinutes: 60, ImageURL: "https://img/b1"},
		},
	}
	g.board.Observe(runner.Scan{AccountID: "a1", Platform: "twitch", At: time.Now(), Campaigns: []platform.Campaign{c},
		Result: plan.Result{Queue: []plan.Target{
			{Campaign: c, Benefit: c.Benefits[1], Minutes: 60},
			{Campaign: c, Benefit: c.Benefits[0], Minutes: 34},
		}}})
	g.board.Observe(runner.Scan{AccountID: "a2", Platform: "twitch", At: time.Now(), Campaigns: []platform.Campaign{c},
		Result: plan.Result{Blocked: []plan.Blocked{{Target: plan.Target{Campaign: c, Benefit: c.Benefits[0]}, Reason: plan.ReasonNotLinked}}}})
	// Deleted account: on the board, not in the store.
	g.board.Observe(runner.Scan{AccountID: "gone", Platform: "twitch", At: time.Now(), Campaigns: []platform.Campaign{c},
		Result: plan.Result{Queue: []plan.Target{{Campaign: c, Benefit: c.Benefits[1], Minutes: 5}}}})
	claimed := time.Date(2026, 10, 25, 14, 2, 0, 0, time.UTC)
	require.NoError(t, g.s.d.Claims.Record(context.Background(), store.ClaimEntry{
		AccountID: "a1", Platform: "twitch", Game: "Rust", CampaignID: "c1", CampaignName: "Rust Drops",
		BenefitID: "b1", BenefitName: "Hoodie", Source: store.ClaimSourceGrubDrops, ClaimedAt: claimed,
	}))

	code, got := g.campaign("/api/campaigns/twitch/c1")
	require.Equal(t, 200, code)
	assert.True(t, got.Live)
	assert.Equal(t, "Rust Drops", got.Name)
	assert.Equal(t, "Rust", got.Game)
	assert.Equal(t, "https://link.example/rust", got.LinkURL)
	assert.NotEmpty(t, got.BoxArtURL)
	require.NotNil(t, got.StartsAt)
	require.NotNil(t, got.EndsAt)
	assert.Equal(t, "2026-10-23T20:00:00Z", *got.StartsAt)
	assert.Equal(t, "2026-10-30T20:00:00Z", *got.EndsAt)
	require.Len(t, got.Drops, 2)

	b1 := got.Drops[0]
	assert.Equal(t, "b1", b1.ID)
	assert.Equal(t, 60, b1.RequiredMinutes)
	assert.Equal(t, "https://img/b1", b1.ImageURL)
	require.Len(t, b1.Accounts, 1, "deleted account dropped")
	assert.Equal(t, "Zed", b1.Accounts[0].AccountName)
	assert.Equal(t, "claimed", b1.Accounts[0].State)
	require.NotNil(t, b1.Accounts[0].ClaimedAt)
	assert.Equal(t, "2026-10-25T14:02:00Z", *b1.Accounts[0].ClaimedAt)

	b2 := got.Drops[1]
	require.Len(t, b2.Accounts, 2)
	assert.Equal(t, "Amy", b2.Accounts[0].AccountName, "sorted by name")
	assert.Equal(t, "blocked", b2.Accounts[0].State)
	assert.Equal(t, "not_linked", b2.Accounts[0].Reason)
	assert.Nil(t, b2.Accounts[0].ClaimedAt)
	assert.Equal(t, "Zed", b2.Accounts[1].AccountName)
	assert.Equal(t, "accruing", b2.Accounts[1].State)
	assert.Equal(t, 34, b2.Accounts[1].Minutes)
}

func TestCampaign_ClaimLogFallback(t *testing.T) {
	g := newCampaignsEnv(t)
	seedClaimAccount(t, g.env, "a1", "Zed")
	for i, b := range []string{"b8", "b7"} {
		require.NoError(t, g.s.d.Claims.Record(context.Background(), store.ClaimEntry{
			AccountID: "a1", Platform: "twitch", Game: "Marvel Rivals", CampaignID: "old", CampaignName: "Season 4",
			BenefitID: b, BenefitName: "name-" + b, ImageURL: "https://img/" + b,
			Source: store.ClaimSourceGrubDrops, ClaimedAt: time.Unix(1_700_000_000+int64(i), 0),
		}))
	}
	// Same campaign ID on the other platform stays separate.
	require.NoError(t, g.s.d.Claims.Record(context.Background(), store.ClaimEntry{
		AccountID: "a1", Platform: "kick", CampaignID: "old", BenefitID: "k1", Source: store.ClaimSourceGrubDrops,
	}))

	code, got := g.campaign("/api/campaigns/twitch/old")
	require.Equal(t, 200, code)
	assert.False(t, got.Live)
	assert.Equal(t, "Season 4", got.Name)
	assert.Equal(t, "Marvel Rivals", got.Game)
	assert.Nil(t, got.StartsAt)
	assert.Nil(t, got.EndsAt)
	require.Len(t, got.Drops, 2)
	assert.Equal(t, "b8", got.Drops[0].ID, "oldest claim first")
	assert.Equal(t, "name-b8", got.Drops[0].Name)
	require.Len(t, got.Drops[0].Accounts, 1)
	assert.Equal(t, "claimed", got.Drops[0].Accounts[0].State)
	assert.NotNil(t, got.Drops[0].Accounts[0].ClaimedAt)
}

func TestCampaign_NotFoundAndBadPlatform(t *testing.T) {
	g := newCampaignsEnv(t)
	code, _ := g.campaign("/api/campaigns/twitch/missing")
	assert.Equal(t, 404, code)
	code, _ = g.campaign("/api/campaigns/youtube/c1")
	assert.Equal(t, 400, code)
}

func TestClaims_ItemCarriesCampaignID(t *testing.T) {
	e := claimsEnv(t)
	loginAdmin(t, e)
	seedClaimAccount(t, e, "a1", "Alice")
	require.NoError(t, e.s.d.Claims.Record(context.Background(), store.ClaimEntry{
		AccountID: "a1", Platform: "twitch", CampaignID: "c1", BenefitID: "b1", Source: store.ClaimSourceGrubDrops,
	}))
	resp, b := e.do("GET", "/api/claims", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	var got struct {
		Items []claimItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(b, &got))
	require.Len(t, got.Items, 1)
	assert.Equal(t, "c1", got.Items[0].CampaignID)
}

func TestCampaign_CellQueuedFollowsRunnerPlan(t *testing.T) {
	g := newCampaignsEnv(t)
	seedClaimAccount(t, g.env, "a1", "Amy")
	seedClaimAccount(t, g.env, "a2", "Bob")
	c := platform.Campaign{
		ID: "c1", Platform: "twitch", Game: "Rust", Name: "Rust Drops", Status: "active",
		Benefits: []platform.DropBenefit{
			{ID: "b1", CampaignID: "c1", Name: "Hoodie", RequiredMinutes: 60},
			{ID: "b2", CampaignID: "c1", Name: "Door", RequiredMinutes: 120},
		},
	}
	skipped := plan.Blocked{Target: plan.Target{Campaign: c, Benefit: c.Benefits[1]}, Reason: plan.ReasonSkipped}
	g.board.Observe(runner.Scan{AccountID: "a1", Platform: "twitch", At: time.Now(), Campaigns: []platform.Campaign{c},
		Result: plan.Result{Queue: []plan.Target{{Campaign: c, Benefit: c.Benefits[0]}}, Blocked: []plan.Blocked{skipped}}})
	g.board.Observe(runner.Scan{AccountID: "a2", Platform: "twitch", At: time.Now(), Campaigns: []platform.Campaign{c},
		Result: plan.Result{Queue: []plan.Target{{Campaign: c, Benefit: c.Benefits[0]}}}})
	cur := plan.Target{Campaign: c, Benefit: c.Benefits[0]}
	// a2 has no running runner: its cells are not in any plan.
	g.s.d.Pool = snapPool{snaps: []runner.Snapshot{{AccountID: "a1", Platform: "twitch", Current: &cur, Blocked: []plan.Blocked{skipped}}}}

	code, got := g.campaign("/api/campaigns/twitch/c1")
	require.Equal(t, 200, code)
	require.Len(t, got.Drops, 2)
	require.Len(t, got.Drops[0].Accounts, 2)
	assert.True(t, got.Drops[0].Accounts[0].Queued, "Amy mines b1")
	assert.False(t, got.Drops[0].Accounts[1].Queued, "Bob has no runner")
	require.Len(t, got.Drops[1].Accounts, 1)
	assert.Equal(t, "skipped", got.Drops[1].Accounts[0].Reason)
	assert.True(t, got.Drops[1].Accounts[0].Queued, "a skipped drop is Blocked in the plan")

	// A logged claim is final: never queued.
	require.NoError(t, g.s.d.Claims.Record(context.Background(), store.ClaimEntry{
		AccountID: "a1", Platform: "twitch", Game: "Rust", CampaignID: "c1", CampaignName: "Rust Drops",
		BenefitID: "b1", BenefitName: "Hoodie", Source: store.ClaimSourceGrubDrops, ClaimedAt: time.Now(),
	}))
	_, got = g.campaign("/api/campaigns/twitch/c1")
	assert.Equal(t, "claimed", got.Drops[0].Accounts[0].State)
	assert.False(t, got.Drops[0].Accounts[0].Queued)
}
