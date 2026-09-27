package kick

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

const progressURL = "https://web.kick.com/api/v1/drops/progress"

func TestKickDropProgress_RequiredFromPlatform(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{progressURL: {200, `{"data":[
		{"id":"c1","rewards":[{"id":"r1","claimed":false,"progress":0.5,"required_units":30}]}
	]}`}}}
	b := withFake(f)
	got, err := b.DropProgress(context.Background(), sess("acc1"), []platform.Campaign{{
		ID: "c1", Benefits: []platform.DropBenefit{{ID: "r1", CampaignID: "c1", RequiredMinutes: 120}},
	}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	// Platform says 30, adapter's invented 120 must not win (P6).
	assert.Equal(t, platform.DropProgress{DropID: "r1", CampaignID: "c1", Minutes: 15, Required: 30, Known: true}, got[0])
}

func TestKickDropProgress_AbsentRewardIsUnknown(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{progressURL: {200, `{"data":[]}`}}}
	b := withFake(f)
	got, err := b.DropProgress(context.Background(), sess("acc1"), []platform.Campaign{{
		ID: "c1", Benefits: []platform.DropBenefit{{ID: "r1", CampaignID: "c1", RequiredMinutes: 120}},
	}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Known, "absence proves nothing")
	assert.Equal(t, 120, got[0].Required)
}

func TestKickDropProgress_ErrorFailsWhole(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{progressURL: {403, `{}`}}}
	_, err := withFake(f).DropProgress(context.Background(), sess("acc1"), []platform.Campaign{{ID: "c1"}})
	require.Error(t, err)
}

func TestKickClaimDrop_Outcomes(t *testing.T) {
	claimURL := "https://web.kick.com/api/v1/drops/claim"
	ok := withFake(&fakeDoer{resp: map[string]fakeResp{claimURL: {200, `{}`}}})
	assert.Equal(t, platform.ClaimOK, ok.ClaimDrop(context.Background(), sess("a"), platform.DropProgress{DropID: "r1", CampaignID: "c1"}).Outcome)

	link := withFake(&fakeDoer{resp: map[string]fakeResp{claimURL: {400, `{"connect_url":"https://kick.com/connect/riot"}`}}})
	r := link.ClaimDrop(context.Background(), sess("a"), platform.DropProgress{DropID: "r1", CampaignID: "c1"})
	assert.Equal(t, platform.ClaimNeedsLink, r.Outcome)
	assert.Equal(t, "https://kick.com/connect/riot", r.LinkURL)

	fail := withFake(&fakeDoer{resp: map[string]fakeResp{claimURL: {500, `{}`}}})
	assert.Equal(t, platform.ClaimFailed, fail.ClaimDrop(context.Background(), sess("a"), platform.DropProgress{DropID: "r1", CampaignID: "c1"}).Outcome)
}

func TestKickProbeChannels(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://kick.com/api/v2/channels/fav/livestream":   {200, `{"data":{"id":7,"viewer_count":3,"categories":[{"name":"Rust","slug":"rust"}]}}`},
		"https://kick.com/api/v2/channels/off/livestream":   {200, `{"data":null}`},
		"https://kick.com/api/v2/channels/other/livestream": {200, `{"data":{"id":8,"viewer_count":9,"categories":[{"name":"Slots","slug":"slots"}]}}`},
	}}
	got, err := withFake(f).ProbeChannels(context.Background(), sess("a"), platform.Campaign{Game: "Rust"}, []string{"Fav", "off", "other"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "fav", got[0].Channel)
}

func TestKickBackend_SatisfiesPipelineInterfaces(t *testing.T) {
	var _ platform.DropProgressSource = (*Backend)(nil)
	var _ platform.DropClaimer = (*Backend)(nil)
	var _ platform.ChannelProber = (*Backend)(nil)
}
