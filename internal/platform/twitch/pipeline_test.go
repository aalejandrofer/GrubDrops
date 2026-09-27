package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// pipelineTestBackend serves DropCampaignDetails + Inventory from fixtures.
func pipelineTestBackend(t *testing.T, details map[string]string, inventory string, detailsStatus int) *Backend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpDropCampaignDetails.Name:
			if detailsStatus != 0 {
				w.WriteHeader(detailsStatus)
				return
			}
			id, _ := req.Variables["dropID"].(string)
			_, _ = w.Write([]byte(details[id]))
		case OpInventory.Name:
			_, _ = w.Write([]byte(inventory))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	t.Cleanup(srv.Close)
	return &Backend{disc: &discovery{c: newTestClient(srv.URL), userLogin: "testuser"}}
}

const emptyInventory = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[]}}}}`

func TestDropProgress_ReadsSelfForCompletedCampaign(t *testing.T) {
	// Campaign left dropCampaignsInProgress (fully claimed), but details self
	// still reports isClaimed. This is the P1 fix.
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":{"currentMinutesWatched":60,"isClaimed":true,"dropInstanceID":"i1"}}
	]}}}}`}, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, platform.DropProgress{DropID: "d1", CampaignID: "c1", Minutes: 60, Required: 60, Claimed: true, InstanceID: "i1", Known: true}, got[0])
}

func TestDropProgress_NullSelfIsUnclaimed(t *testing.T) {
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":null}
	]}}}}`}, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Claimed)
	assert.Equal(t, 0, got[0].Minutes)
	assert.True(t, got[0].Known)
}

func TestDropProgress_DedupesMultiItemDrop(t *testing.T) {
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[{"benefit":{"id":"b1","name":"A"}},{"benefit":{"id":"b2","name":"B"}}],"self":null},
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":null}
	]}}}}`}, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestDropProgress_InventoryFillsInstanceAndMinutes(t *testing.T) {
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":{"currentMinutesWatched":10,"isClaimed":false,"dropInstanceID":""}}
	]}}}}`}, `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
		{"id":"c1","timeBasedDrops":[{"id":"d1","self":{"currentMinutesWatched":25,"isClaimed":false,"dropInstanceID":"inst"}}]}
	]}}}}`, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 25, got[0].Minutes)
	assert.Equal(t, "inst", got[0].InstanceID)
}

func TestDropProgress_InventoryClaimedWins(t *testing.T) {
	// Details self says isClaimed:false but inventory reports isClaimed:true
	// for the same drop — claimed=true from either source wins because
	// isClaimed never reverts on Twitch.
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":{"currentMinutesWatched":60,"isClaimed":false,"dropInstanceID":"i1"}}
	]}}}}`}, `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
		{"id":"c1","timeBasedDrops":[{"id":"d1","self":{"currentMinutesWatched":60,"isClaimed":true,"dropInstanceID":"i1"}}]}
	]}}}}`, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Claimed)
}

func TestDropProgress_SyntheticCampaignIsUnmineable(t *testing.T) {
	b := pipelineTestBackend(t, nil, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "Game|Name", Platform: "twitch", Benefits: []platform.DropBenefit{{ID: "Game|Name_default"}}}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Unmineable)
	assert.True(t, got[0].Known)
}

func TestDropProgress_DetailsErrorFailsWhole(t *testing.T) {
	b := pipelineTestBackend(t, nil, emptyInventory, http.StatusInternalServerError)
	_, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.Error(t, err)
}

func TestClassifyClaimStatus(t *testing.T) {
	assert.Equal(t, platform.ClaimOK, classifyClaimStatus("ELIGIBLE_FOR_ALL").Outcome)
	assert.Equal(t, platform.ClaimOK, classifyClaimStatus("").Outcome)
	assert.Equal(t, platform.ClaimAlready, classifyClaimStatus("DROP_INSTANCE_ALREADY_CLAIMED").Outcome)
	r := classifyClaimStatus("DROP_INSTANCE_ALREADY_EXPIRED")
	assert.Equal(t, platform.ClaimFailed, r.Outcome)
	assert.Contains(t, r.Detail, "DROP_INSTANCE_ALREADY_EXPIRED")
}

func TestBackend_SatisfiesPipelineInterfaces(t *testing.T) {
	var _ platform.DropProgressSource = (*Backend)(nil)
	var _ platform.DropClaimer = (*Backend)(nil)
	var _ platform.ChannelProber = (*Backend)(nil)
}
