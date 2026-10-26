package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// usherBaseURL is the HLS master playlist endpoint. The channel login and
// ".m3u8" are appended, plus the playback access token as sig/token params.
const usherBaseURL = "https://usher.ttvnw.net/api/channel/hls/"

// maxPlaylistBytes caps how much of a master/media playlist we read. Real
// playlists are a few KB; the cap stops a misbehaving edge from streaming
// an unbounded body into memory.
const maxPlaylistBytes = 1 << 20

// playbackAccessToken fetches the stream playback access token for a live
// channel, mirroring DevilXD/TwitchDropsMiner's PlaybackAccessToken
// persisted query (same variables). The token is what usher needs to hand
// back the master playlist.
func (c *client) playbackAccessToken(ctx context.Context, oauthToken, login string) (value, signature string, err error) {
	var resp struct {
		StreamPlaybackAccessToken *struct {
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"streamPlaybackAccessToken"`
	}
	vars := map[string]any{
		"isLive":     true,
		"isVod":      false,
		"login":      login,
		"platform":   "web",
		"playerType": "site",
		"vodID":      "",
	}
	if err := c.gql(ctx, oauthToken, OpPlaybackAccessToken, vars, &resp); err != nil {
		return "", "", err
	}
	t := resp.StreamPlaybackAccessToken
	if t == nil || t.Value == "" || t.Signature == "" {
		return "", "", fmt.Errorf("playback access token empty for %s", login)
	}
	return t.Value, t.Signature, nil
}

// resolvePlaylistURL returns the channel's lowest-quality variant (media)
// playlist URL: access token from GQL, master playlist from usher, then the
// last variant listed (usher orders best to worst, so the last is the
// cheapest stream; DevilXD picks the same one).
func (c *client) resolvePlaylistURL(ctx context.Context, oauthToken, login string) (string, error) {
	value, sig, err := c.playbackAccessToken(ctx, oauthToken, login)
	if err != nil {
		return "", fmt.Errorf("playback access token: %w", err)
	}
	base := c.usherURL
	if base == "" {
		base = usherBaseURL
	}
	q := url.Values{}
	q.Set("sig", sig)
	q.Set("token", value)
	masterURL := strings.TrimRight(base, "/") + "/" + url.PathEscape(login) + ".m3u8?" + q.Encode()

	body, err := c.fetchHLS(ctx, oauthToken, masterURL)
	if err != nil {
		return "", fmt.Errorf("master playlist: %w", err)
	}
	lines := playlistURLs(body)
	if len(lines) == 0 {
		return "", fmt.Errorf("master playlist for %s lists no variants", login)
	}
	variant := lines[len(lines)-1]
	if !c.hostAllowedForHLS(variant) {
		return "", fmt.Errorf("variant playlist host not allowed for %s: %s", login, variant)
	}
	return variant, nil
}

// sendPlaylistWatch is the DevilXD (f693b0c) watch method: GET the current
// media playlist, then HEAD every chunk URL in it. Twitch credits watch
// time from the CDN seeing chunk requests; HEAD is enough, no video data is
// downloaded. Success means every HEAD returned 200.
func (c *client) sendPlaylistWatch(ctx context.Context, oauthToken, playlistURL string) error {
	body, err := c.fetchHLS(ctx, oauthToken, playlistURL)
	if err != nil {
		return fmt.Errorf("media playlist: %w", err)
	}
	chunks := playlistURLs(body)
	if len(chunks) == 0 {
		return fmt.Errorf("media playlist has no chunks")
	}
	for _, chunk := range chunks {
		if !c.hostAllowedForHLS(chunk) {
			return fmt.Errorf("chunk host not allowed: %s", chunk)
		}
		if err := c.headHLS(ctx, oauthToken, chunk); err != nil {
			return err
		}
	}
	return nil
}

// playlistURLs returns the lines of an m3u8 body that are absolute http(s)
// URLs. Tags (#EXTINF, #EXT-X-ENDLIST, ...) and blank lines are skipped.
func playlistURLs(body []byte) []string {
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http") {
			out = append(out, line)
		}
	}
	return out
}

// fetchHLS GETs an usher/playlist URL. It sends only Accept + User-Agent:
// no Authorization, Client-Id or device headers ever go to HLS hosts
// (DevilXD sends none either; the signed URL is the credential).
// Connection: close mirrors DevilXD; the CDN drops the connection right
// after the list anyway.
func (c *client) fetchHLS(ctx context.Context, oauthToken, rawURL string) ([]byte, error) {
	if !c.hostAllowedForHLS(rawURL) {
		return nil, fmt.Errorf("hls host not allowed: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHLSHeaders(req, oauthToken)
	req.Close = true
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxPlaylistBytes))
	if resp.StatusCode >= 400 {
		// An offline stream returns 404 here.
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	// Usher/edge errors come back as JSON (sometimes a one-element list)
	// instead of an m3u8 body.
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return nil, fmt.Errorf("error response: %s", hlsErrorMessage(trimmed))
	}
	return body, nil
}

// hlsErrorMessage pulls the "error" field out of an usher JSON error body,
// falling back to a short prefix of the raw body.
func hlsErrorMessage(body []byte) string {
	var one map[string]any
	if json.Unmarshal(body, &one) != nil {
		var many []map[string]any
		if json.Unmarshal(body, &many) == nil && len(many) > 0 {
			one = many[0]
		}
	}
	if e, ok := one["error"]; ok {
		return fmt.Sprint(e)
	}
	if len(body) > 120 {
		body = body[:120]
	}
	return string(body)
}

func (c *client) headHLS(ctx context.Context, oauthToken, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err
	}
	c.setHLSHeaders(req, oauthToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chunk HEAD: status %d", resp.StatusCode)
	}
	return nil
}

// setHLSHeaders sets the User-Agent of the client profile that minted
// oauthToken. The token itself is only a lookup key here and is never put
// on the request.
func (c *client) setHLSHeaders(req *http.Request, oauthToken string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", c.profileForToken(oauthToken).UserAgent)
}

// hostAllowedForHLS reports whether rawURL is a Twitch video host we'll
// request playlists or chunks from. Usher, the playlist edges and the
// chunk edges live on ttvnw.net (usher.ttvnw.net,
// video-weaver.*.hls.ttvnw.net, video-edge-*.abs.hls.ttvnw.net) or on
// Twitch's IVS domain live-video.net (*.playlist.live-video.net,
// *.hls.live-video.net). Only https is allowed. No credentials go to these
// hosts, but pinning them keeps a tampered playlist from turning the miner
// into a request bouncer.
func (c *client) hostAllowedForHLS(rawURL string) bool {
	if c.hlsHostAllow != nil {
		return c.hlsHostAllow(rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, d := range []string{"ttvnw.net", "live-video.net"} {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
