package canary_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestTwitchProbe_OK verifies that Run returns Result{OK: true} when both
// beacons are accepted by the (faked) Twitch GQL endpoint.
// The test server dispatches on operationName so the CurrentUser resolve and
// SendEvents mutation both succeed independently.
func TestTwitchProbe_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Peek at the request body to route by operationName.
		// Both query types POST JSON with an "operationName" field.
		var req struct {
			OperationName string `json:"operationName"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.OperationName {
		case "CurrentUser":
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"12345"}}}`))
		default:
			// SendEvents and any future mutation.
			_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
		}
	}))
	defer srv.Close()

	probe := canary.NewTwitchProbeForTest(srv.URL)
	result := probe.Run(context.Background(), platform.Session{AccessToken: "tok"}, "testchannel")

	assert.True(t, result.OK, "expected OK=true when beacons are accepted")
	assert.Contains(t, result.Detail, "beacon", "detail should mention beacons")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")
}

// TestTwitchProbe_Error verifies that Run returns Result{OK: false} when the
// beacon call returns a server error.
func TestTwitchProbe_Error(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// First call is CurrentUser (resolveUserID).
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"99999"}}}`))
			return
		}
		// Subsequent calls (beacons) fail.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal error"}]}`))
	}))
	defer srv.Close()

	probe := canary.NewTwitchProbeForTest(srv.URL)
	result := probe.Run(context.Background(), platform.Session{AccessToken: "tok"}, "testchannel")

	assert.False(t, result.OK, "expected OK=false when beacon fails")
	assert.True(t, strings.Contains(result.Detail, "beacon") || result.Detail != "",
		"detail should describe the failure")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")
}
