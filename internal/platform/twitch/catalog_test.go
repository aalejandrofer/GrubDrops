package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestCatalog_ActiveOnlyWithRewards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case OpDropCampaignDetails.Name:
			assert.Equal(t, "42", req.Variables["channelLogin"])
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	camps, err := b.Catalog(context.Background(), platform.Session{AccessToken: "tok", UserID: "42"})
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.Equal(t, "camp1", camps[0].ID)
	assert.Equal(t, "Rust", camps[0].Game)
	assert.Equal(t, "https://static-cdn.jtvnw.net/ttv-boxart/263490-144x192.jpg", camps[0].BoxArtURL, "gql game.boxArtURL, size filled")
	require.Len(t, camps[0].Benefits, 1)
	assert.Equal(t, "Wolf Helmet", camps[0].Benefits[0].Name)
	assert.Equal(t, 60, camps[0].Benefits[0].RequiredMinutes)
	assert.Empty(t, b.disc.drainAllowed(), "catalog must not capture allow-lists")
}

func TestCatalog_NullCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"currentUser":null}}`))
	}))
	defer srv.Close()

	_, err := newForTest(srv.URL).Catalog(context.Background(), platform.Session{AccessToken: "tok", UserID: "1"})
	assert.True(t, errors.Is(err, ErrCatalogUnavailable))
}

func TestCatalog_TVTokenRefused(t *testing.T) {
	_, err := newForTest("http://127.0.0.1:1").Catalog(context.Background(), platform.Session{AccessToken: "tok", ClientID: ClientTV})
	assert.True(t, errors.Is(err, ErrCatalogUnavailable))
}

func TestCatalog_DetailFailureKeepsRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		if req.OperationName == OpCampaigns.Name {
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	camps, err := newForTest(srv.URL).Catalog(context.Background(), platform.Session{AccessToken: "tok", UserID: "1"})
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.Empty(t, camps[0].Benefits)
}
