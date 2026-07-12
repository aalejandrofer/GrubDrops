package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestProbeBeacon_OK verifies that ProbeBeacon returns nil when the
// Spade beacon transport accepts every heartbeat (HTTP 204). After the
// 2026-07-11 Twitch change, the probe exercises the Spade beacon POST
// path, so the test server must serve a channel page (with an inlined
// spade_url) plus the beacon endpoint itself.
func TestProbeBeacon_OK(t *testing.T) {
	const channel = "testchannel"
	var beaconCount int32
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	// Channel page: inline the spade_url so resolveSpadeURL finds it.
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>"spade_url": "` + srvBase() + `/spade"</html>`))
	})
	// Spade beacon endpoint: 204 No Content is the success signal.
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&beaconCount, 1)
		w.WriteHeader(http.StatusNoContent)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	b := newForTest(srv.URL)
	// Pre-populate cachedUserID to skip the CurrentUser fetch, so the
	// test server only needs to handle the channel page + beacon.
	b.watch.cachedUserID = 12345
	sess := platform.Session{AccessToken: "tok-probe"}
	// Use 0 interval so the test doesn't wait 60s between beacons.
	err := b.ProbeBeacon(context.Background(), sess, channel, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&beaconCount), "expected exactly 2 beacon calls")
}

// TestProbeBeacon_Error verifies that ProbeBeacon returns an error when
// the Spade beacon transport returns a non-204 response.
func TestProbeBeacon_Error(t *testing.T) {
	const channel = "testchannel"
	callCount := 0
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>"spade_url": "` + srvBase() + `/spade"</html>`))
	})
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	b := newForTest(srv.URL)
	b.watch.cachedUserID = 12345 // skip CurrentUser
	sess := platform.Session{AccessToken: "tok-probe"}
	err := b.ProbeBeacon(context.Background(), sess, channel, 0)
	require.Error(t, err, "expected error when beacon returns 5xx")
}

// TestProbeBeacon_Timeout verifies that ProbeBeacon respects context
// cancellation during the inter-beacon sleep.
func TestProbeBeacon_Timeout(t *testing.T) {
	const channel = "testchannel"
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>"spade_url": "` + srvBase() + `/spade"</html>`))
	})
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // first beacon succeeds
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	b := newForTest(srv.URL)
	b.watch.cachedUserID = 12345 // skip CurrentUser so timeout fires during the sleep
	sess := platform.Session{AccessToken: "tok-probe"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Very long interval — context should cancel during the sleep between beacons.
	err := b.ProbeBeacon(ctx, sess, channel, 10*time.Second)
	require.Error(t, err, "expected context cancellation error")
}
