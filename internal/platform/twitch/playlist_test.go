package twitch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// hlsFake is an httptest server that plays GQL (PlaybackAccessToken), usher,
// the media playlist edge, the chunk edge and Spade, all on one host.
type hlsFake struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	masterHits  int
	usherQuery  string
	heads       []string
	hlsAuth     []string // Authorization/Client-Id seen on any HLS request
	spadeStatus int
	spadeHits   int
	// variant returns the variant path usher lists last, per master hit.
	variant func(hit int) string
	// chunkStatus returns the HEAD status for a chunk path.
	chunkStatus func(path string) int
}

func newHLSFake(t *testing.T, channel string) *hlsFake {
	t.Helper()
	f := &hlsFake{
		t:           t,
		spadeStatus: http.StatusNoContent,
		variant:     func(int) string { return "/v/low.m3u8" },
		chunkStatus: func(string) int { return http.StatusOK },
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		require.Contains(t, string(b), `"PlaybackAccessToken"`)
		require.Contains(t, string(b), `"login":"`+channel+`"`)
		_, _ = io.WriteString(w, `{"data":{"streamPlaybackAccessToken":{"value":"{\"channel\":\"x\"}","signature":"sig123"}}}`)
	})
	mux.HandleFunc("/hls/"+channel+".m3u8", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.masterHits++
		hit := f.masterHits
		f.usherQuery = r.URL.RawQuery
		f.recordAuth(r)
		f.mu.Unlock()
		_, _ = io.WriteString(w, "#EXTM3U\n"+
			"#EXT-X-STREAM-INF:BANDWIDTH=6000000\n"+f.srv.URL+"/v/high.m3u8\n"+
			"#EXT-X-STREAM-INF:BANDWIDTH=200000\n"+f.srv.URL+f.variant(hit)+"\n")
	})
	mux.HandleFunc("/v/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.recordAuth(r)
		f.mu.Unlock()
		if r.URL.Path == "/v/stale.m3u8" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n"+
			"#EXTINF:2.000,live\n"+f.srv.URL+"/c/1.ts\n"+
			"#EXTINF:2.000,live\n"+f.srv.URL+"/c/2.ts\n"+
			"#EXTINF:2.000,live\n"+f.srv.URL+"/c/3.ts\n"+
			"#EXT-X-ENDLIST\n")
	})
	mux.HandleFunc("/c/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.recordAuth(r)
		if r.Method == http.MethodHead {
			f.heads = append(f.heads, r.URL.Path)
		}
		f.mu.Unlock()
		w.WriteHeader(f.chunkStatus(r.URL.Path))
	})
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html>"spade_url": "`+f.srv.URL+`/spade"</html>`)
	})
	mux.HandleFunc("/spade", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.spadeHits++
		st := f.spadeStatus
		f.mu.Unlock()
		w.WriteHeader(st)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *hlsFake) recordAuth(r *http.Request) {
	if v := r.Header.Get("Authorization"); v != "" {
		f.hlsAuth = append(f.hlsAuth, "Authorization: "+v)
	}
	if v := r.Header.Get("Client-Id"); v != "" {
		f.hlsAuth = append(f.hlsAuth, "Client-Id: "+v)
	}
}

func (f *hlsFake) client() *client {
	c := newTestClient(f.srv.URL + "/gql")
	c.homeURL = f.srv.URL
	c.usherURL = f.srv.URL + "/hls/"
	c.hlsHostAllow = func(raw string) bool { return strings.HasPrefix(raw, f.srv.URL+"/") }
	return c
}

func startWatch(t *testing.T, wt *watch, channel, broadcastID string) platform.WatchHandle {
	t.Helper()
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tokhls"},
		platform.Stream{Channel: channel, ChannelID: "c1", BroadcastID: broadcastID, GameID: "g1", Game: "World of Warcraft"})
	require.NoError(t, err)
	return h
}

func TestPlaylistURLs_OnlyHTTPLines(t *testing.T) {
	body := []byte("#EXTM3U\r\n#EXT-X-TARGETDURATION:2\n#EXTINF:2.000,live\n" +
		"https://video-edge-a.abs.hls.ttvnw.net/v1/segment/1.ts\n\n" +
		"#EXTINF:2.000,live\r\nhttps://video-edge-a.abs.hls.ttvnw.net/v1/segment/2.ts\r\n" +
		"relative/3.ts\n#EXT-X-ENDLIST\n")
	assert.Equal(t, []string{
		"https://video-edge-a.abs.hls.ttvnw.net/v1/segment/1.ts",
		"https://video-edge-a.abs.hls.ttvnw.net/v1/segment/2.ts",
	}, playlistURLs(body))
	assert.Empty(t, playlistURLs([]byte("#EXTM3U\n#EXT-X-ENDLIST\n")))
}

// TestWatch_PlaylistHeartbeatHeadsAllChunks pins the DevilXD f693b0c flow:
// access token, usher with sig/token, last (lowest) variant, HEAD on every
// chunk, and no Twitch credentials on any HLS request.
func TestWatch_PlaylistHeartbeatHeadsAllChunks(t *testing.T) {
	const channel = "wowstreamer"
	f := newHLSFake(t, channel)
	wt := &watch{c: f.client(), cachedUserID: 1}
	h := startWatch(t, wt, channel, "b1")

	require.NoError(t, wt.heartbeat(context.Background(), h))

	assert.Equal(t, []string{"/c/1.ts", "/c/2.ts", "/c/3.ts"}, f.heads, "every chunk HEADed, ENDLIST ignored")
	assert.Contains(t, f.usherQuery, "sig=sig123")
	assert.Contains(t, f.usherQuery, "token=%7B%22channel%22")
	assert.Empty(t, f.hlsAuth, "no Authorization or Client-Id may reach HLS hosts")
	assert.Equal(t, f.srv.URL+"/v/low.m3u8", wt.playlistURLs[channel].url, "last variant is picked and cached")

	// Second beat reuses the cached playlist URL (no new usher fetch).
	require.NoError(t, wt.heartbeat(context.Background(), h))
	assert.Equal(t, 1, f.masterHits)
	assert.Len(t, f.heads, 6)
}

func TestSendPlaylistWatch_FailsWhenAnyChunkNot200(t *testing.T) {
	const channel = "flaky"
	f := newHLSFake(t, channel)
	f.chunkStatus = func(p string) int {
		if p == "/c/2.ts" {
			return http.StatusForbidden
		}
		return http.StatusOK
	}
	err := f.client().sendPlaylistWatch(context.Background(), "tokhls", f.srv.URL+"/v/low.m3u8")
	require.Error(t, err)
	assert.Equal(t, []string{"/c/1.ts", "/c/2.ts"}, f.heads, "stops at the first failed chunk")
}

func TestSendPlaylistWatch_JSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"error":"Can not find channel","error_code":"vod_manifest_restricted"}]`)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	c.hlsHostAllow = func(string) bool { return true }
	err := c.sendPlaylistWatch(context.Background(), "", srv.URL+"/v/x.m3u8")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Can not find channel")
}

// TestWatch_PlaylistReResolvesOnFailure: the cached variant URL goes stale
// (404), so the heartbeat evicts it, resolves a fresh one and succeeds.
func TestWatch_PlaylistReResolvesOnFailure(t *testing.T) {
	const channel = "rotating"
	f := newHLSFake(t, channel)
	f.variant = func(hit int) string {
		if hit == 1 {
			return "/v/stale.m3u8"
		}
		return "/v/low.m3u8"
	}
	f.spadeStatus = http.StatusBadRequest // only the playlist path can pass
	wt := &watch{c: f.client(), cachedUserID: 1}
	h := startWatch(t, wt, channel, "b1")

	require.NoError(t, wt.heartbeat(context.Background(), h))
	assert.Equal(t, 2, f.masterHits, "stale URL evicted and re-resolved once")
	assert.Len(t, f.heads, 3)
	assert.Equal(t, f.srv.URL+"/v/low.m3u8", wt.playlistURLs[channel].url)
}

func TestWatch_PlaylistReResolvesOnNewBroadcast(t *testing.T) {
	const channel = "restarted"
	f := newHLSFake(t, channel)
	wt := &watch{c: f.client(), cachedUserID: 1}

	require.NoError(t, wt.heartbeat(context.Background(), startWatch(t, wt, channel, "b1")))
	require.NoError(t, wt.heartbeat(context.Background(), startWatch(t, wt, channel, "b2")))
	assert.Equal(t, 2, f.masterHits, "a new broadcast id resolves a new playlist")
}

// TestWatch_SpadeFailureIgnoredWhenPlaylistOK: Spade is secondary, so its
// failure must not fail the heartbeat when the playlist path worked.
func TestWatch_SpadeFailureIgnoredWhenPlaylistOK(t *testing.T) {
	const channel = "nospade"
	f := newHLSFake(t, channel)
	f.spadeStatus = http.StatusInternalServerError
	wt := &watch{c: f.client(), cachedUserID: 1}

	require.NoError(t, wt.heartbeat(context.Background(), startWatch(t, wt, channel, "b1")))
	assert.Equal(t, 2, f.spadeHits, "spade still sent (and retried once)")
	assert.Len(t, f.heads, 3)
}

func TestWatch_HeartbeatFailsWhenBothPathsFail(t *testing.T) {
	const channel = "offline"
	f := newHLSFake(t, channel)
	f.chunkStatus = func(string) int { return http.StatusNotFound }
	f.spadeStatus = http.StatusBadRequest
	wt := &watch{c: f.client(), cachedUserID: 1}

	err := wt.heartbeat(context.Background(), startWatch(t, wt, channel, "b1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "playlist:")
	assert.Contains(t, err.Error(), "spade:")
}

func TestHostAllowedForHLS(t *testing.T) {
	c := newTestClient("")
	for raw, want := range map[string]bool{
		"https://usher.ttvnw.net/api/channel/hls/x.m3u8":                  true,
		"https://video-weaver.fra05.hls.ttvnw.net/v1/playlist/abc.m3u8":   true,
		"https://video-edge-1a2b.fra05.abs.hls.ttvnw.net/v1/segment/x.ts": true,
		"https://use14.playlist.live-video.net/v1/playlist/abc.m3u8":      true,
		"https://d2nvs31859zcd8.cloudfront.net/x.ts":                      false,
		"http://video-edge.abs.hls.ttvnw.net/x.ts":                        false, // https only
		"https://ttvnw.net.evil.example/x.ts":                             false,
		"https://evilttvnw.net/x.ts":                                      false,
		"https://www.twitch.tv/x":                                         false,
		"not a url":                                                       false,
	} {
		assert.Equal(t, want, c.hostAllowedForHLS(raw), raw)
	}
}

// TestSendPlaylistWatch_RefusesForeignChunkHost: a playlist pointing at a
// foreign host is rejected before any request goes there.
func TestSendPlaylistWatch_RefusesForeignChunkHost(t *testing.T) {
	var foreignHits int
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits++
	}))
	defer foreign.Close()
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:2.000,live\n"+foreign.URL+"/c/1.ts\n")
	}))
	defer edge.Close()

	c := newTestClient("")
	c.hlsHostAllow = func(raw string) bool { return strings.HasPrefix(raw, edge.URL+"/") }
	err := c.sendPlaylistWatch(context.Background(), "", edge.URL+"/v/low.m3u8")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk host not allowed")
	assert.Zero(t, foreignHits)

	// Default validator: a plain-http test host is refused outright.
	c.hlsHostAllow = nil
	require.Error(t, c.sendPlaylistWatch(context.Background(), "", edge.URL+"/v/low.m3u8"))
}
