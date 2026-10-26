package sidecar

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// watchNetObserver records whether a watch tab requested Kick's playback
// API and an HLS playlist, and the playback API's HTTP status. It keeps
// booleans and a status code only: never URLs, query strings, headers or
// cookies, because the playback and playlist URLs carry viewer tokens.
//
// Amazon IVS fetches the playlist from a web worker, which is a separate
// CDP target, so m3u8 can read false while playback still works. The
// playback API request is made by the page and is the reliable signal.
type watchNetObserver struct {
	mu             sync.Mutex
	playbackIDs    map[network.RequestID]bool
	playbackAPI    bool
	playbackStatus int64
	m3u8           bool
}

// classifyWatchURL reports whether rawURL is Kick's stream playback API
// (web.kick.com/api/v1/stream/<id>/playback) or an HLS playlist.
func classifyWatchURL(rawURL string) (playbackAPI, m3u8 bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, false
	}
	p := strings.ToLower(u.Path)
	host := strings.ToLower(u.Hostname())
	playbackAPI = (host == "kick.com" || strings.HasSuffix(host, ".kick.com")) &&
		strings.Contains(p, "/stream/") && strings.HasSuffix(p, "/playback")
	m3u8 = strings.HasSuffix(p, ".m3u8")
	return playbackAPI, m3u8
}

func (o *watchNetObserver) listen(ev interface{}) {
	switch e := ev.(type) {
	case *network.EventRequestWillBeSent:
		if e.Request == nil {
			return
		}
		pb, m3u8 := classifyWatchURL(e.Request.URL)
		o.mu.Lock()
		if pb {
			o.playbackAPI = true
			if o.playbackIDs == nil {
				o.playbackIDs = map[network.RequestID]bool{}
			}
			o.playbackIDs[e.RequestID] = true
		}
		if m3u8 {
			o.m3u8 = true
		}
		o.mu.Unlock()
	case *network.EventResponseReceived:
		if e.Response == nil {
			return
		}
		o.mu.Lock()
		if o.playbackIDs[e.RequestID] {
			o.playbackStatus = e.Response.Status
		}
		o.mu.Unlock()
	}
}

// watchFailureDiag is what a failed watch start logs. No cookie or token
// values: page URL without query, title, video and consent state, and
// whether the player's requests were made.
type watchFailureDiag struct {
	URL            string `json:"url"`
	Title          string `json:"title"`
	Videos         int    `json:"videos"`
	ReadyState     int    `json:"readyState"`
	NetworkState   int    `json:"networkState"`
	VideoError     string `json:"videoError,omitempty"`
	ConsentBanner  bool   `json:"consentBanner"`
	MediaSource    bool   `json:"mediaSource"`
	PlaybackAPI    bool   `json:"playbackApiRequested"`
	PlaybackStatus int64  `json:"playbackApiStatus,omitempty"`
	M3U8           bool   `json:"m3u8Requested"`
	ProbeErr       string `json:"probeErr,omitempty"`
}

// kickWatchDiagScript reads page state for watchFailureDiag. Read-only.
// It strips the query and fragment from location so nothing token-like
// reaches the log, and reports any page-level .m3u8 resource fetch.
const kickWatchDiagScript = `(() => {
  const out = {url: '', title: '', videos: 0, readyState: -1, networkState: -1, consentBanner: false, mediaSource: false, m3u8: false};
  try {
    out.url = location.origin + location.pathname;
    out.title = (document.title || '').slice(0, 120);
    const vs = document.querySelectorAll('video');
    out.videos = vs.length;
    const v = vs[0];
    if (v) {
      out.readyState = v.readyState;
      out.networkState = v.networkState;
      if (v.error) out.videoError = String(v.error.code) + ' ' + String(v.error.message || '').slice(0, 120);
    }
    const b = document.getElementById('onetrust-banner-sdk');
    out.consentBanner = !!(b && b.getClientRects().length > 0 && getComputedStyle(b).display !== 'none' && getComputedStyle(b).visibility !== 'hidden');
    out.mediaSource = typeof window.MediaSource === 'function';
    try {
      out.m3u8 = performance.getEntriesByType('resource').some(e => {
        try { return new URL(e.name).pathname.toLowerCase().endsWith('.m3u8'); } catch (x) { return false; }
      });
    } catch (e) {}
  } catch (e) {
    out.err = String(e).slice(0, 120);
  }
  return JSON.stringify(out);
})()`

// watchFailureDiag gathers the failed-start diagnostic for a watch tab.
func (k *Kick) watchFailureDiag(ctx context.Context, obs *watchNetObserver) string {
	var raw string
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := chromedp.Run(probeCtx, chromedp.Evaluate(kickWatchDiagScript, &raw))
	cancel()
	return buildWatchFailureDiag(raw, err, obs)
}

// buildWatchFailureDiag merges the page probe JSON with the network
// observer into one JSON line. A probe error or bad JSON still yields the
// network fields.
func buildWatchFailureDiag(raw string, probeErr error, obs *watchNetObserver) string {
	var page struct {
		URL           string `json:"url"`
		Title         string `json:"title"`
		Videos        int    `json:"videos"`
		ReadyState    int    `json:"readyState"`
		NetworkState  int    `json:"networkState"`
		VideoError    string `json:"videoError"`
		ConsentBanner bool   `json:"consentBanner"`
		MediaSource   bool   `json:"mediaSource"`
		M3U8          bool   `json:"m3u8"`
		Err           string `json:"err"`
	}
	d := watchFailureDiag{ReadyState: -1, NetworkState: -1}
	if probeErr != nil {
		d.ProbeErr = probeErr.Error()
	} else if err := json.Unmarshal([]byte(raw), &page); err != nil {
		d.ProbeErr = "bad probe json"
	} else {
		d.URL, d.Title, d.Videos = page.URL, page.Title, page.Videos
		d.ReadyState, d.NetworkState, d.VideoError = page.ReadyState, page.NetworkState, page.VideoError
		d.ConsentBanner, d.MediaSource, d.M3U8 = page.ConsentBanner, page.MediaSource, page.M3U8
		d.ProbeErr = page.Err
	}
	if obs != nil {
		obs.mu.Lock()
		d.PlaybackAPI, d.PlaybackStatus = obs.playbackAPI, obs.playbackStatus
		d.M3U8 = d.M3U8 || obs.m3u8
		obs.mu.Unlock()
	}
	out, _ := json.Marshal(d)
	return string(out)
}
