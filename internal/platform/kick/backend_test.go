package kick

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/dropsminer/internal/platform"
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

func withFake(f *fakeDoer) *Backend {
	b := New(nil)
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
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://web.kick.com/api/v1/drops/progress": {200, `{"data":[
			{"reward_id":"d1","campaign_id":"c1","minutes_watched":45,"claimed":false}
		]}`},
	}}
	b := withFake(f)
	pr, err := b.InventoryProgress(context.Background(), sess("acc1"))
	require.NoError(t, err)
	require.Len(t, pr, 1)
	assert.Equal(t, "d1", pr[0].BenefitID)
	assert.Equal(t, 45, pr[0].MinutesWatched)
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

func TestKickBackend_WatchPingViaHeartbeat(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://kick.com/api/v1/video/views/12345": {200, `{}`},
	}}
	b := withFake(f)
	h, err := b.StartWatch(context.Background(), sess("acc1"), platform.Stream{Channel: "tippie", ChannelID: "12345"})
	require.NoError(t, err)
	require.NoError(t, b.Heartbeat(context.Background(), h))
	require.Len(t, f.calls, 1)
	assert.Equal(t, "POST", f.calls[0].method)
	assert.Equal(t, "https://kick.com/api/v1/video/views/12345", f.calls[0].path)
	// No livestream id -> heartbeat is a soft no-op (manual channel case).
	h2, _ := b.StartWatch(context.Background(), sess("acc1"), platform.Stream{Channel: "x"})
	require.NoError(t, b.Heartbeat(context.Background(), h2))
}

func TestKickBackend_ListEligibleChannels_LiveCampaignChannel(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		// tippie is live (data present), bob is offline (data null).
		"https://kick.com/api/v2/channels/tippie/livestream": {200, `{"data":{"id":111,"viewer_count":50}}`},
		"https://kick.com/api/v2/channels/bob/livestream":    {200, `{"data":null}`},
	}}
	b := withFake(f)
	out, err := b.ListEligibleChannels(context.Background(), sess("acc1"),
		platform.Campaign{ID: "c1", Game: "Rust", AllowedChannels: []string{"bob", "tippie"}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "tippie", out[0].Channel)
	assert.Equal(t, "111", out[0].ChannelID)
	assert.Equal(t, 50, out[0].ViewerCount)
}

func TestKickBackend_DeviceLoginRejected(t *testing.T) {
	b := New(nil)
	_, err := b.StartDeviceLogin(context.Background())
	require.Error(t, err)
}

// --- channel registration (manual fallback) — pure, no transport ----------

func TestKickBackend_RegisterChannelExposesInList(t *testing.T) {
	b := New(nil)
	b.RegisterChannel("acc1", "fakestreamer")
	// Empty campaign so the live-discovery paths skip and we hit the manual
	// fallback (no cookies in the session anyway).
	out, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fakestreamer", out[0].Channel)
}

func TestKickBackend_RegisterChannelsMulti(t *testing.T) {
	b := New(nil)
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
	b := New(nil)
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
	b := New(nil)
	b.RegisterChannels("acc1", []string{" alice ", "Alice", "bob", "", " bob ", "carol"})
	assert.Equal(t, []string{"alice", "bob", "carol"}, b.Channels("acc1"))
}

func TestKickBackend_AllowedChannelCountDistinct(t *testing.T) {
	b := New(nil)
	assert.Equal(t, 0, b.AllowedChannelCount("anything"))
	b.RegisterChannel("acc1", "alice")
	b.RegisterChannel("acc2", "bob")
	b.RegisterChannel("acc3", "alice")
	assert.Equal(t, 2, b.AllowedChannelCount("kick-inventory"))
}
