package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestProbeBeacon_OK verifies that ProbeBeacon returns nil when the fake
// transport accepts every beacon (HTTP 200 with statusCode 204 in data).
func TestProbeBeacon_OK(t *testing.T) {
	var beaconCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beaconCount++
		_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	// Pre-populate cachedUserID to skip the CurrentUser fetch (same pattern
	// used by TestWatch_HeartbeatSendsAuthHeader), so the test server only
	// needs to handle the SendEvents mutation.
	b.watch.cachedUserID = 12345
	sess := platform.Session{AccessToken: "tok-probe"}
	// Use 0 interval so the test doesn't wait 60s between beacons.
	err := b.ProbeBeacon(context.Background(), sess, "testchannel", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, beaconCount, "expected exactly 2 beacon calls")
}

// TestProbeBeacon_Error verifies that ProbeBeacon returns an error when the
// fake transport returns a non-2xx response for the beacon call.
func TestProbeBeacon_Error(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// First call is CurrentUser (resolveUserID); return a valid user.
		// Subsequent calls are beacon mutations — return 500.
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"99999"}}}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"internal server error"}]}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tok-probe"}
	err := b.ProbeBeacon(context.Background(), sess, "testchannel", 0)
	require.Error(t, err, "expected error when beacon returns 5xx")
}

// TestProbeBeacon_Timeout verifies that ProbeBeacon respects context cancellation
// during the inter-beacon sleep.
func TestProbeBeacon_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All calls return a valid beacon response; timeout fires during the sleep.
		_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	// Pre-populate cachedUserID to skip CurrentUser; we want the timeout to
	// fire during the inter-beacon sleep, not during a CurrentUser call.
	b.watch.cachedUserID = 12345
	sess := platform.Session{AccessToken: "tok-probe"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Very long interval — context should cancel during the sleep between beacons.
	err := b.ProbeBeacon(ctx, sess, "testchannel", 10*time.Second)
	require.Error(t, err, "expected context cancellation error")
}
