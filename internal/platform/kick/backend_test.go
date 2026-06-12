package kick

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeDoer returns canned (status, body) per request path and records calls,
// standing in for the live utls transport so the backend's mapping/claim/watch
// logic is unit-testable offline.
type fakeDoer struct {
	resp  map[string]fakeResp
	calls []fakeCall
}

type fakeResp struct {
	status int
	body   string
}

type fakeCall struct {
	method string
	path   string
	body   string
}

func (f *fakeDoer) do(_ context.Context, _ platform.Session, method, path string, body []byte) ([]byte, int, error) {
	f.calls = append(f.calls, fakeCall{method: method, path: path, body: string(body)})
	if r, ok := f.resp[path]; ok {
		return []byte(r.body), r.status, nil
	}
	return []byte("{}"), 404, nil
}

func (f *fakeDoer) getRaw(_ context.Context, rawURL string) ([]byte, string, int, error) {
	if r, ok := f.resp[rawURL]; ok {
		return []byte(r.body), "image/png", r.status, nil
	}
	return nil, "", 404, nil
}

func withFake(f *fakeDoer) *Backend {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	b.api = &api{d: f}
	return b
}

func sess(id string) platform.Session { return platform.Session{AccountID: id} }

func TestKickBackend_ListActiveCampaigns_MapsAndFilters(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": {200, `{"data":[
			{"id":"c-rust","game":"Rust","name":"Rust Drops","status":"active",
			 "rewards":[{"id":"ben1","name":"Crate","required_minutes":60}]},
			{"id":"c-cs","game":"Counter-Strike","name":"CS Drops","status":"active",
			 "rewards":[{"id":"ben2","name":"Sticker","required_minutes":30}]}
		]}`},
	}}
	b := withFake(f)

	all, err := b.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, all, 2)
	for _, c := range all {
		assert.Equal(t, "kick", c.Platform)
		require.NotEmpty(t, c.Benefits)
		assert.Equal(t, c.ID, c.Benefits[0].CampaignID)
	}

	// GameFilter prunes to Rust only.
	s := sess("acc1")
	s.GameFilter = func(g string) bool { return g == "Rust" }
	only, err := b.ListActiveCampaigns(context.Background(), s)
	require.NoError(t, err)
	require.Len(t, only, 1)
	assert.Equal(t, "Rust", only[0].Game)
}

// Kick exposes no per-campaign "is the external account linked" signal (unlike
// Twitch). Deriving link state from /drops/progress deadlocks: progress only
// appears after watch time, watch time is blocked until "linked", "linked" is
// read from progress. So a connect_url campaign the account has no progress for
// must be treated as OPTIMISTICALLY linked (mine anyway) rather than blocked.
func TestKickBackend_ListActiveCampaigns_ConnectURLOptimisticallyLinked(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": {200, `{"data":[
			{"id":"c-rust","game":"Rust","name":"Rust Drops","status":"active",
			 "connect_url":"https://kick.com/connect/steam",
			 "rewards":[{"id":"ben1","name":"Crate","required_minutes":60}]}
		]}`},
		// No progress yet (freshly linked, never watched) — the old code read
		// this as "not linked" and blocked mining.
		"https://web.kick.com/api/v1/drops/progress": {200, `{"data":[]}`},
	}}
	b := withFake(f)

	camps, err := b.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.True(t, camps[0].AccountLinked, "connect_url campaign with no progress should be optimistically linked (mineable)")
	assert.Equal(t, "https://kick.com/connect/steam", camps[0].AccountLinkURL, "connect URL still surfaced for the user")
}

func TestKickBackend_ListActiveCampaigns_DefaultRequiredMinutes(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": {200, `{"data":[
			{"id":"c1","game":"Rust","name":"Drops","status":"active",
			 "rewards":[{"id":"b1","name":"Reward"}]}
		]}`},
	}}
	b := withFake(f)
	camps, err := b.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, camps, 1)
	require.Len(t, camps[0].Benefits, 1)
	assert.Equal(t, 120, camps[0].Benefits[0].RequiredMinutes)
}

func TestKickBackend_InventoryProgress(t *testing.T) {
	// Live-verified shape: progress nests rewards under each campaign, and
	// reward.progress is the FRACTION watched (minutes = progress*required_units).
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/progress": {200, `{"data":[
			{"id":"c1","name":"Rust Drops","progress_units":45,"rewards":[
				{"id":"d1","claimed":false,"progress":0.75,"required_units":60},
				{"id":"d2","claimed":true,"progress":1,"required_units":30}
			]}
		],"message":"Success"}`},
	}}
	b := withFake(f)
	pr, err := b.InventoryProgress(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, pr, 2)
	assert.Equal(t, "d1", pr[0].BenefitID)
	assert.Equal(t, 45, pr[0].MinutesWatched) // round(0.75 * 60)
	assert.False(t, pr[0].Claimed)
	assert.Equal(t, "d2", pr[1].BenefitID)
	assert.Equal(t, 30, pr[1].MinutesWatched) // round(1 * 30)
	assert.True(t, pr[1].Claimed)
}

func TestKickBackend_Claim_PostsRewardAndCampaign(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/claim": {200, `{}`},
	}}
	b := withFake(f)
	err := b.Claim(context.Background(), sess("acc1"), platform.DropBenefit{ID: "rew1", CampaignID: "camp1"})
	require.NoError(t, err)
	require.Len(t, f.calls, 1)
	assert.Equal(t, "POST", f.calls[0].method)
	assert.Equal(t, "https://web.kick.com/api/v1/drops/claim", f.calls[0].path)
	assert.Contains(t, f.calls[0].body, `"reward_id":"rew1"`)
	assert.Contains(t, f.calls[0].body, `"campaign_id":"camp1"`)
}

func TestKickBackend_ListEligibleChannels_LiveCampaignChannel(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		// tippie is live (data present), bob is offline (data null).
		"https://kick.com/api/v2/channels/tippie/livestream": {200, `{"data":{"id":999,"viewer_count":50}}`},
		"https://kick.com/api/v2/channels/bob/livestream":    {200, `{"data":null}`},
	}}
	b := withFake(f)
	// Channels (slug+channelId) come from the campaigns payload, cached here.
	b.campaignChannels["c1"] = []kickChannel{{Slug: "bob", ID: "5"}, {Slug: "tippie", ID: "27589"}}
	out, err := b.ListEligibleChannels(context.Background(), sess("acc1"),
		platform.Campaign{ID: "c1", Game: "Rust"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "tippie", out[0].Channel)
	// ChannelID is the CHANNEL id (for the viewer-WS handshake), not livestream id.
	assert.Equal(t, "27589", out[0].ChannelID)
	assert.Equal(t, 50, out[0].ViewerCount)
}

// An OPEN campaign (no channels of its own) must borrow the category-wide pool
// of participating channels gathered from sibling campaigns — that's how the
// daemon finds a live Rust channel for "Kick Off 2 - General Drops" et al.
// Verifies liveness + category before committing (no junk picks).
func TestKickBackend_OpenCampaignBorrowsCategoryPool(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/campaigns": {200, `{"data":[
			{"id":"c-A","game":"Rust","name":"A","status":"active",
			 "channels":[{"slug":"tippie","id":"27589"}],
			 "rewards":[{"id":"b1","required_units":60}]},
			{"id":"c-B","game":"Rust","name":"B (open)","status":"active",
			 "rewards":[{"id":"b2","required_units":60}]}
		]}`},
		"https://kick.com/api/v2/channels/tippie/livestream": {200, `{"data":{"id":999,"viewer_count":50,"categories":[{"name":"Rust","slug":"rust"}]}}`},
	}}
	b := withFake(f)
	_, err := b.ListActiveCampaigns(context.Background(), sess("acc1"))
	require.NoError(t, err)

	// c-B has no channels of its own; it should borrow tippie from the Rust pool.
	out, err := b.ListEligibleChannels(context.Background(), sess("acc1"),
		platform.Campaign{ID: "c-B", Game: "Rust"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "tippie", out[0].Channel)
	assert.Equal(t, "27589", out[0].ChannelID)
}

// A live channel streaming a DIFFERENT game than the campaign must be rejected
// (watching it accrues nothing) — guards against the old generic-feed junk.
func TestKickBackend_RejectsWrongCategoryChannel(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://kick.com/api/v2/channels/wrong/livestream": {200, `{"data":{"id":1,"viewer_count":9,"categories":[{"name":"Left 4 Dead 2","slug":"left-4-dead-2"}]}}`},
	}}
	b := withFake(f)
	b.campaignChannels["c1"] = []kickChannel{{Slug: "wrong", ID: "1"}}
	out, err := b.ListEligibleChannels(context.Background(), sess("acc1"),
		platform.Campaign{ID: "c1", Game: "Rust"})
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestKickBackend_DeviceLoginRejected(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	_, err := b.StartDeviceLogin(context.Background())
	require.Error(t, err)
}

// --- channel registration (manual fallback) — pure, no transport ----------

func TestKickBackend_RegisterChannelExposesInList(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	b.RegisterChannel("acc1", "fakestreamer")
	// Empty campaign so the live-discovery paths skip and we hit the manual
	// fallback (no cookies in the session anyway).
	out, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fakestreamer", out[0].Channel)
}

func TestKickBackend_RegisterChannelsMulti(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	b.RegisterChannels("acc1", []string{"alice", "bob", "carol"})
	out, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out, 3)
	names := map[string]bool{}
	for _, s := range out {
		names[s.Channel] = true
	}
	assert.True(t, names["alice"] && names["bob"] && names["carol"])
}

func TestKickBackend_ListEligibleChannelsScopedToAccount(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	b.RegisterChannels("acc1", []string{"alice", "bob"})
	b.RegisterChannels("acc2", []string{"carol"})

	out1, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out1, 2)

	out2, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc2"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out2, 1)
	assert.Equal(t, "carol", out2[0].Channel)

	outNone, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc3"}, platform.Campaign{})
	require.NoError(t, err)
	assert.Empty(t, outNone)
}

func TestKickBackend_RegisterChannels_DedupesAndTrims(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	b.RegisterChannels("acc1", []string{" alice ", "Alice", "bob", "", " bob ", "carol"})
	assert.Equal(t, []string{"alice", "bob", "carol"}, b.Channels("acc1"))
}

func TestKickBackend_AllowedChannelCountDistinct(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	assert.Equal(t, 0, b.AllowedChannelCount("anything"))
	b.RegisterChannel("acc1", "alice")
	b.RegisterChannel("acc2", "bob")
	b.RegisterChannel("acc3", "alice")
	assert.Equal(t, 2, b.AllowedChannelCount("kick-inventory"))
}

// EnableBrowserWatch is a no-op (and must NOT flip browserWatch) when the
// backend has no sidecar client — otherwise StartWatch would dereference a
// nil client. With no sidecar, Kick watch is unavailable (StartWatch errors;
// there is no non-accruing fallback).
func TestKickBackend_EnableBrowserWatch_NilClientNoOp(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	b.EnableBrowserWatch()
	assert.False(t, b.browserWatch, "browser-watch must stay off without a sidecar client")
}

// Heartbeat/StopWatch dispatch on the watch-handle's concrete Internal
// type. An unknown/zero handle is an error for Heartbeat (so the watcher
// re-picks) and a benign no-op for StopWatch (idempotent teardown).
func TestKickBackend_WatchHandleDispatch(t *testing.T) {
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)

	// Unknown handle type -> Heartbeat errors, StopWatch is a no-op.
	bad := platform.WatchHandle{Channel: "x", Internal: struct{}{}}
	assert.Error(t, b.Heartbeat(context.Background(), bad))
	assert.NoError(t, b.StopWatch(context.Background(), bad))

	// Zero handle (Internal == nil) behaves the same.
	zero := platform.WatchHandle{Channel: "x"}
	assert.Error(t, b.Heartbeat(context.Background(), zero))
	assert.NoError(t, b.StopWatch(context.Background(), zero))
}

// preferReliableChannels moves a known always-live broadcaster (oilrats) to
// the front of an OPEN campaign's category pool so the watcher lands on it,
// without adding/dropping channels or disturbing the rest's order.
func TestPreferReliableChannels(t *testing.T) {
	pool := []kickChannel{
		{Slug: "welyn", ID: "1"},
		{Slug: "oilrats", ID: "2"},
		{Slug: "trausi", ID: "3"},
	}
	got := preferReliableChannels(pool)
	require.Len(t, got, 3)
	assert.Equal(t, "oilrats", got[0].Slug, "oilrats should sort first")
	assert.Equal(t, "welyn", got[1].Slug, "rest keep original order")
	assert.Equal(t, "trausi", got[2].Slug)

	// No reliable channel present -> unchanged.
	none := []kickChannel{{Slug: "a"}, {Slug: "b"}}
	assert.Equal(t, none, preferReliableChannels(none))
}

// SweepCompletedClaims claims every reward at 100% that isn't already granted,
// skips in-progress and already-claimed rewards, and posts reward_id +
// campaign_id per claim.
func TestKickBackend_SweepCompletedClaims(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/progress": {200, `{"data":[
			{"id":"camp1","rewards":[
				{"id":"done-unclaimed","name":"Box","progress":1,"claimed":false,"required_units":120},
				{"id":"already-claimed","name":"Crossbow","progress":1,"claimed":true,"required_units":120},
				{"id":"in-progress","name":"Door","progress":0.5,"claimed":false,"required_units":120}
			]}
		],"message":"Success"}`},
		"https://web.kick.com/api/v1/drops/claim": {200, `{}`},
	}}
	b := withFake(f)
	claimed, err := b.SweepCompletedClaims(context.Background(), sess("acc1"))
	require.NoError(t, err)
	// Only the completed-but-unclaimed reward is claimed.
	require.Len(t, claimed, 1)
	assert.Equal(t, "Box", claimed[0].Title)
	// Exactly one claim POST fired, for the right reward+campaign.
	var claimCalls int
	for _, c := range f.calls {
		if c.path == "https://web.kick.com/api/v1/drops/claim" {
			claimCalls++
			assert.Contains(t, c.body, `"reward_id":"done-unclaimed"`)
			assert.Contains(t, c.body, `"campaign_id":"camp1"`)
		}
	}
	assert.Equal(t, 1, claimCalls, "exactly one completed reward should be claimed")
}
