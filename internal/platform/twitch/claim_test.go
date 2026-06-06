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

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

func TestClaim_SendsCorrectVariables(t *testing.T) {
	var got struct {
		body []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
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
		Variables struct {
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
	require.Error(t, err)
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
