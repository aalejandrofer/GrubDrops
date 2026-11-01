package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestClaim_SendsCorrectVariables(t *testing.T) {
	var got struct {
		body []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if bytes.Contains(b, []byte(OpClaimDrop.Name)) {
			got.body = b // the post-claim inventory read is not the claim
		}
		_, _ = w.Write(loadFixture(t, "claim_ok.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	cl := &claimer{c: c}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "drop1", CampaignID: "camp1"}, 0)
	require.NoError(t, err)

	var req struct {
		OperationName string `json:"operationName"`
		Variables     struct {
			Input struct {
				DropInstanceID string `json:"dropInstanceID"`
			} `json:"input"`
		} `json:"variables"`
		Extensions struct {
			PersistedQuery struct {
				Sha256Hash string `json:"sha256Hash"`
			} `json:"persistedQuery"`
		} `json:"extensions"`
	}
	require.NoError(t, json.Unmarshal(got.body, &req))
	assert.Equal(t, "DropsPage_ClaimDropRewards", req.OperationName)
	assert.Equal(t, "drop1", req.Variables.Input.DropInstanceID)
	assert.Equal(t, OpClaimDrop.Hash, req.Extensions.PersistedQuery.Sha256Hash)
}

func TestClaim_AcceptsAlreadyClaimedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "claim_ok.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	cl := &claimer{c: c}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "drop1"}, 0)
	require.NoError(t, err)
}

func TestClaim_RejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"claimDropRewards":{"status":"WHO_KNOWS"}}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	cl := &claimer{c: c}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "drop1"}, 0)
	require.ErrorIs(t, err, platform.ErrClaimRefused)
}

func TestClaim_ServerErrorIsNotRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cl := &claimer{c: newTestClient(srv.URL)}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "drop1"}, 0)
	require.Error(t, err)
	assert.NotErrorIs(t, err, platform.ErrClaimRefused)
}

// When InstanceID is empty, the claimer builds DevilXD's synthetic
// dropInstanceID `userID#campaignID#dropID` rather than the bare drop id.
func TestClaim_SyntheticInstanceIDFallback(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("491#camp1#drop1")) {
			gotID = "491#camp1#drop1"
		}
		_, _ = w.Write([]byte(`{"data":{"claimDropRewards":{"status":"ELIGIBLE_FOR_ALL"}}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	cl := &claimer{c: c}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "drop1", CampaignID: "camp1"}, 491)
	require.NoError(t, err)
	require.Equal(t, "491#camp1#drop1", gotID)
}

// Review Focus #5: Twitch answers a bad/missing instance id with
// claimDropRewards:null. That must not count as claimed.
func TestClaim_NullResultIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"claimDropRewards":null}}`))
	}))
	defer srv.Close()
	b := newForTest(srv.URL)
	err := b.Claim(context.Background(), platform.Session{AccessToken: "t"}, platform.DropBenefit{ID: "d1", InstanceID: "i1"})
	require.Error(t, err)
}

// claimServer answers DropsPage_ClaimDropRewards with claimBody and the
// Inventory read with invBody, counting each op.
func claimServer(t *testing.T, claimBody, invBody string) (*httptest.Server, map[string]int) {
	t.Helper()
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		calls[req.OperationName]++
		switch req.OperationName {
		case OpClaimDrop.Name:
			_, _ = w.Write([]byte(claimBody))
		case OpInventory.Name:
			_, _ = w.Write([]byte(invBody))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// invOneDrop is an Inventory body holding drop d1 of campaign camp1, fully
// watched, with the given claimed flag.
func invOneDrop(claimed bool) string {
	c := "false"
	if claimed {
		c = "true"
	}
	return `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
	  {"id":"camp1","name":"WTCS","game":{"name":"War Thunder"},"timeBasedDrops":[
	    {"id":"d1","name":"50% SL Booster","requiredMinutesWatched":30,"self":{"currentMinutesWatched":30,"isClaimed":` + c + `,"dropInstanceID":"u#camp1#d1"}}
	  ]}]}}}}`
}

func noConfirmWait(t *testing.T) {
	t.Helper()
	prev := confirmGap
	confirmGap = 0
	t.Cleanup(func() { confirmGap = prev })
}

// GRUB-58: Twitch can answer ELIGIBLE_FOR_ALL (or ALREADY_CLAIMED) and still
// keep the drop unclaimed. The inventory decides: still unclaimed is a
// refused claim, never a success.
func TestClaim_SuccessStatusButInventoryUnclaimedIsRefused(t *testing.T) {
	noConfirmWait(t)
	for _, status := range []string{"ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED"} {
		t.Run(status, func(t *testing.T) {
			srv, calls := claimServer(t, `{"data":{"claimDropRewards":{"status":"`+status+`"}}}`, invOneDrop(false))
			cl := &claimer{c: newTestClient(srv.URL)}
			err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
				platform.DropBenefit{ID: "d1", CampaignID: "camp1", InstanceID: "u#camp1#d1"}, 0)
			require.ErrorIs(t, err, platform.ErrClaimRefused)
			assert.Equal(t, 1, calls[OpClaimDrop.Name], "one claim attempt")
			assert.Equal(t, confirmReads, calls[OpInventory.Name], "every confirm read used")
		})
	}
}

func TestClaim_SuccessStatusConfirmedByInventory(t *testing.T) {
	noConfirmWait(t)
	srv, calls := claimServer(t, `{"data":{"claimDropRewards":{"status":"ELIGIBLE_FOR_ALL"}}}`, invOneDrop(true))
	cl := &claimer{c: newTestClient(srv.URL)}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "d1", CampaignID: "camp1", InstanceID: "u#camp1#d1"}, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, calls[OpInventory.Name])
}

// A campaign Twitch evicted from the inventory after its last claim counts
// as granted.
func TestClaim_DropGoneFromInventoryIsConfirmed(t *testing.T) {
	noConfirmWait(t)
	srv, _ := claimServer(t, `{"data":{"claimDropRewards":{"status":"ELIGIBLE_FOR_ALL"}}}`,
		`{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[]}}}}`)
	cl := &claimer{c: newTestClient(srv.URL)}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "d1", CampaignID: "camp1"}, 0)
	require.NoError(t, err)
}

// An empty status is no grant.
func TestClaim_EmptyStatusIsRefused(t *testing.T) {
	noConfirmWait(t)
	srv, _ := claimServer(t, `{"data":{"claimDropRewards":{"status":""}}}`, invOneDrop(false))
	cl := &claimer{c: newTestClient(srv.URL)}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "d1", CampaignID: "camp1"}, 0)
	require.ErrorIs(t, err, platform.ErrClaimRefused)
}

// A failed confirm read trusts Twitch's status: the 24h inventory sweep
// catches a claim that did not land.
func TestClaim_ConfirmReadFailureTrustsStatus(t *testing.T) {
	noConfirmWait(t)
	srv, _ := claimServer(t, `{"data":{"claimDropRewards":{"status":"ELIGIBLE_FOR_ALL"}}}`, `{"errors":[{"message":"service error"}],"data":null}`)
	cl := &claimer{c: newTestClient(srv.URL)}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "d1", CampaignID: "camp1"}, 0)
	require.NoError(t, err)
}
