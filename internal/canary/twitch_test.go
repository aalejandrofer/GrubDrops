package canary_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// spadeTestServer builds an httptest server that serves both the channel
// page (with an inlined spade_url) and the Spade beacon endpoint, so the
// post-2026-07-11 watch transport can resolve the beacon URL and POST to
// it. The CurrentUser GQL query is still routed through the same server's
// root path (httptest dispatches by path).
//
// beaconResponder is invoked for each beacon POST; returning a non-204
// status lets callers simulate failures.
func spadeTestServer(t *testing.T, channel string, beaconResponder http.HandlerFunc) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	// CurrentUser GQL resolve (root /gql-style POST lands on "/").
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string `json:"operationName"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.OperationName == "CurrentUser" {
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"12345"}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	// Channel page: inline the spade_url.
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>"spade_url": "` + srvBase() + `/spade"</html>`))
	})
	// Spade beacon endpoint.
	mux.HandleFunc("/spade", beaconResponder)
	srv = httptest.NewServer(mux)
	return srv
}

// TestTwitchProbe_OK verifies that Run returns Result{OK: true} when both
// Spade beacons are accepted (HTTP 204) by the test server.
func TestTwitchProbe_OK(t *testing.T) {
	const channel = "testchannel"
	srv := spadeTestServer(t, channel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	probe := canary.NewTwitchProbeForTest(srv.URL)
	result := probe.Run(context.Background(), platform.Session{AccessToken: "tok"}, channel)

	assert.True(t, result.OK, "expected OK=true when beacons are accepted")
	assert.Contains(t, result.Detail, "beacon", "detail should mention beacons")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")
}

// TestTwitchProbe_Error verifies that Run returns Result{OK: false} when the
// Spade beacon call returns a server error.
func TestTwitchProbe_Error(t *testing.T) {
	const channel = "testchannel"
	srv := spadeTestServer(t, channel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	probe := canary.NewTwitchProbeForTest(srv.URL)
	result := probe.Run(context.Background(), platform.Session{AccessToken: "tok"}, channel)

	assert.False(t, result.OK, "expected OK=false when beacon fails")
	assert.Contains(t, result.Detail, "beacon", "detail should describe the beacon failure")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")
}
