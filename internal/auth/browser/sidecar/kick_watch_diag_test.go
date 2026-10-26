package sidecar

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Kick's OneTrust consent banner blocks the player from requesting its
// playback URL until a choice is made, so the <video> stays at readyState
// 0. The drive script must click through it on every run.
func TestDriveScriptDismissesConsentBanner(t *testing.T) {
	for _, id := range []string{"onetrust-banner-sdk", "onetrust-accept-btn-handler", "onetrust-reject-all-handler"} {
		assert.Contains(t, kickPlayerDriveScript, id)
	}
	// The consent click must run before the <video> lookup so a page with
	// no player yet still gets the banner dismissed.
	assert.Less(t, strings.Index(kickPlayerDriveScript, "onetrust-accept-btn-handler"),
		strings.Index(kickPlayerDriveScript, "document.querySelector('video')"))
	// The banner is position:fixed, so offsetParent is always null and must
	// not be the visibility test (it hid the banner from the first fix).
	assert.NotContains(t, kickPlayerDriveScript, "offsetParent")
	assert.NotContains(t, kickWatchDiagScript, "offsetParent")
}

func TestClassifyWatchURL(t *testing.T) {
	cases := []struct {
		url      string
		pb, m3u8 bool
	}{
		{"https://web.kick.com/api/v1/stream/01a17943-c170/playback", true, false},
		{"https://kick.com/api/v1/stream/abc/playback?x=1", true, false},
		{"https://evil.example/api/v1/stream/abc/playback", false, false},
		{"https://evilkick.com/api/v1/stream/abc/playback", false, false},
		{"https://fa723fc1b171.us-west-2.playback.live-video.net/api/video/v1/x.channel.y.m3u8?token=t", false, true},
		{"https://web.kick.com/api/v1/drops/campaigns/livestream?channel_id=1", false, false},
		{"::not a url", false, false},
	}
	for _, tc := range cases {
		pb, m := classifyWatchURL(tc.url)
		assert.Equal(t, tc.pb, pb, tc.url)
		assert.Equal(t, tc.m3u8, m, tc.url)
	}
}

func TestWatchNetObserverRecordsPlaybackStatus(t *testing.T) {
	o := &watchNetObserver{}
	o.listen(&network.EventRequestWillBeSent{RequestID: "1", Request: &network.Request{URL: "https://web.kick.com/api/v1/stream/abc/playback"}})
	o.listen(&network.EventRequestWillBeSent{RequestID: "2", Request: &network.Request{URL: "https://kick.com/other"}})
	o.listen(&network.EventResponseReceived{RequestID: "2", Response: &network.Response{Status: 500}})
	o.listen(&network.EventResponseReceived{RequestID: "1", Response: &network.Response{Status: 403}})
	assert.True(t, o.playbackAPI)
	assert.Equal(t, int64(403), o.playbackStatus)
	assert.False(t, o.m3u8)
}

func TestBuildWatchFailureDiag(t *testing.T) {
	obs := &watchNetObserver{m3u8: true}
	raw := `{"url":"https://kick.com/valorant","title":"Valorant","videos":2,"readyState":0,"networkState":0,"consentBanner":true,"mediaSource":true,"m3u8":false}`
	var d watchFailureDiag
	require.NoError(t, json.Unmarshal([]byte(buildWatchFailureDiag(raw, nil, obs)), &d))
	assert.Equal(t, "https://kick.com/valorant", d.URL)
	assert.Equal(t, 2, d.Videos)
	assert.True(t, d.ConsentBanner)
	assert.True(t, d.M3U8, "network observer m3u8 must be merged in")
	assert.False(t, d.PlaybackAPI)

	// A probe failure still reports the network side.
	require.NoError(t, json.Unmarshal([]byte(buildWatchFailureDiag("", errors.New("context deadline exceeded"), &watchNetObserver{playbackAPI: true, playbackStatus: 200})), &d))
	assert.Equal(t, "context deadline exceeded", d.ProbeErr)
	assert.True(t, d.PlaybackAPI)
	assert.Equal(t, int64(200), d.PlaybackStatus)
	assert.Equal(t, -1, d.ReadyState)
}

// The diagnostic must never carry a query string: Kick playback and
// playlist URLs hold viewer tokens.
func TestWatchDiagScriptStripsQuery(t *testing.T) {
	assert.Contains(t, kickWatchDiagScript, "location.origin + location.pathname")
	assert.NotContains(t, kickWatchDiagScript, "location.href")
	assert.NotContains(t, kickWatchDiagScript, "document.cookie")
}
