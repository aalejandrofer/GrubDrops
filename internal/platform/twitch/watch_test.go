package twitch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// spadeTestClient builds a *client whose homeURL + http client point at
// the test server, so both the channel-page GET (resolveSpadeURL) and
// the beacon POST (sendSpadeBeacon) hit the same httptest server.
func spadeTestClient(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	c := newTestClient(srv.URL)
	c.homeURL = srv.URL
	return c
}

// TestWatch_HeartbeatPostsSpadeBeacon is the core regression test for
// the 2026-07-11 Twitch API change: drop progress now requires a Spade
// beacon POST, not the GQL SendEvents mutation. It pins the request
// shape against the proven twitch-gql-rs send_watch:
//   - method POST, Content-Type application/x-www-form-urlencoded
//   - body form shape data=<urlencoded-base64-of-json> (NO gzip, NO
//     twilight/GZIP_B64 envelope)
//   - success on HTTP 204
func TestWatch_HeartbeatPostsSpadeBeacon(t *testing.T) {
	const (
		wantChannel     = "goldenstreamer"
		wantChannelID   = "chan99"
		wantBroadcastID = "bcast42"
		wantUserID      = int64(12345)
		wantGame        = "Some Game"
		wantGameID      = "game7"
	)

	var beacon struct {
		method      string
		contentType string
		auth        string
		body        string
	}
	// srv is assigned below; the handler closures capture this var (not
	// its value), so they see the final URL once httptest has bound a port.
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	// Channel page: serve HTML that inlines the spade_url.
	mux.HandleFunc("/"+wantChannel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html>"spade_url": "`+srvBase()+`/spade"</html>`)
	})
	// Spade beacon endpoint.
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		beacon.method = r.Method
		beacon.contentType = r.Header.Get("Content-Type")
		beacon.auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		beacon.body = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := spadeTestClient(t, srv)
	wt := &watch{c: c, cachedUserID: wantUserID, spadeURLs: map[string]string{}}
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tokgolden"},
		platform.Stream{
			Channel:     wantChannel,
			ChannelID:   wantChannelID,
			BroadcastID: wantBroadcastID,
			Game:        wantGame,
			GameID:      wantGameID,
		})
	require.NoError(t, err)
	require.NoError(t, wt.heartbeat(context.Background(), h))

	// ── Transport shape (the thing that broke) ───────────────────────────────
	assert.Equal(t, http.MethodPost, beacon.method, "beacon must be POST")
	assert.Equal(t, "application/x-www-form-urlencoded", beacon.contentType,
		"Content-Type must be application/x-www-form-urlencoded (the 2026-07-11 fix)")
	assert.Equal(t, "OAuth tokgolden", beacon.auth, "beacon must carry the OAuth token")

	// Body is a single data= form field.
	form, err := url.ParseQuery(beacon.body)
	require.NoError(t, err, "body must be a valid form-encoded string")
	require.Len(t, form, 1, "body must have exactly one field: data")
	b64 := form.Get("data")
	require.NotEmpty(t, b64, "data field must be present")

	// Decoded payload is plain base64 of a JSON array — NOT gzip.
	raw, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err, "data must be valid base64")

	var events []map[string]any
	require.NoError(t, json.Unmarshal(raw, &events), "decoded data must be plain JSON (no gzip)")
	require.Len(t, events, 1)
	assert.Equal(t, "minute-watched", events[0]["event"])

	props, ok := events[0]["properties"].(map[string]any)
	require.True(t, ok, "event must have a properties object")

	// Required accrual fields — Twitch silently discards heartbeats without these.
	assert.EqualValues(t, wantUserID, props["user_id"], "user_id must be the numeric Twitch ID")
	assert.Equal(t, wantChannelID, props["channel_id"], "channel_id from stream metadata")
	assert.Equal(t, wantBroadcastID, props["broadcast_id"], "broadcast_id from stream metadata")
	assert.Equal(t, wantGame, props["game"], "game from stream metadata")
	assert.Equal(t, wantGameID, props["game_id"], "game_id from stream metadata")
	assert.EqualValues(t, 1, props["minutes_logged"], "minutes_logged must be 1")
	assert.Equal(t, true, props["is_live"], "is_live must be true")
	assert.Equal(t, true, props["logged_in"], "logged_in must be true")

	// The legacy envelope is gone — body is form-encoded, not a GQL JSON envelope.
	assert.NotContains(t, beacon.body, "sendSpadeEvents",
		"heartbeat must NOT use the GQL SendEvents mutation (Twitch stopped crediting it)")
	assert.NotContains(t, beacon.body, "GZIP_B64",
		"heartbeat must NOT use the gzip/twilight envelope")
}

// TestWatch_HeartbeatRetriesAfterFailedBeacon verifies the cache-evict
// + re-resolve + retry path: a first beacon failure triggers a fresh
// resolveSpadeURL, and the second beacon attempt succeeds.
func TestWatch_HeartbeatRetriesAfterFailedBeacon(t *testing.T) {
	const channel = "retrystreamer"
	// srv is assigned below; the handler closures capture this var (not
	// its value), so they see the final URL once httptest has bound a port.
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	beaconHits := 0
	pageHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		pageHits++
		endpoint := "/spade"
		if pageHits == 1 {
			endpoint = "/spade-stale"
		}
		_, _ = io.WriteString(w, `<html>"spade_url": "`+srvBase()+endpoint+`"</html>`)
	})
	mux.HandleFunc("/spade-stale", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // first beacon fails
	})
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		beaconHits++
		w.WriteHeader(http.StatusNoContent)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := spadeTestClient(t, srv)
	wt := &watch{c: c, cachedUserID: 12345, spadeURLs: map[string]string{}}
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tok"},
		platform.Stream{Channel: channel, ChannelID: "c1", BroadcastID: "b1"})
	require.NoError(t, err)
	require.NoError(t, wt.heartbeat(context.Background(), h))

	assert.Equal(t, 1, beaconHits, "the second (fresh) beacon should succeed")
	// The stale URL was cached then evicted; the cache now holds the fresh one.
	wt.spadeMu.Lock()
	cached := wt.spadeURLs[channel]
	wt.spadeMu.Unlock()
	assert.True(t, strings.HasSuffix(cached, "/spade"), "cache should hold the fresh /spade URL")
}

// TestWatch_HeartbeatFailsOnNon204 confirms a persistently-failing
// beacon (both attempts) surfaces an error rather than silently
// succeeding.
func TestWatch_HeartbeatFailsOnNon204(t *testing.T) {
	const channel = "deadstreamer"
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html>"spade_url": "`+srvBase()+`/spade"</html>`)
	})
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := spadeTestClient(t, srv)
	wt := &watch{c: c, cachedUserID: 12345, spadeURLs: map[string]string{}}
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tok"},
		platform.Stream{Channel: channel})
	require.NoError(t, err)
	err = wt.heartbeat(context.Background(), h)
	require.Error(t, err, "both beacon attempts failed — heartbeat must error")
}
