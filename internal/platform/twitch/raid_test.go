package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/stretchr/testify/require"
)

func TestBackendJoinRaidSendsRaidID(t *testing.T) {
	var request struct {
		OperationName string `json:"operationName"`
		Variables     struct {
			Input struct {
				RaidID string `json:"raidID"`
			} `json:"input"`
		} `json:"variables"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		_, _ = w.Write([]byte(`{"data":{"joinRaid":{}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	require.NoError(t, b.JoinRaid(context.Background(), platform.Session{AccessToken: "token"}, "raid-1"))
	require.Equal(t, "JoinRaid", request.OperationName)
	require.Equal(t, "raid-1", request.Variables.Input.RaidID)
}
