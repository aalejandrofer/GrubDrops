package kick

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	utls "github.com/refraction-networking/utls"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Pure-WebSocket Kick watch path. A viewer presence over wss — NO browser, no
// IVS <video> — that accrues drop watch-time server-side. Verified 2026-06-13:
// a campaign at 0% climbed monotonically with only this loop alive (see
// project-ws-accrual-retest-plan memory). The KEY is a PERIODIC
// channel_handshake (~12s); a one-shot handshake does not accrue.
//
// This is the experimental alternative to the chromedp IVS sidecar, selected
// per-deployment via the kick_watch_mode setting. The server credits ONE active
// watch per account, so WS and the sidecar are mutually exclusive — the toggle
// is exclusive by design.
const (
	websocketsBase = "https://websockets.kick.com"
	wsConnectURL   = "wss://websockets.kick.com/viewer/v1/connect"
	// xClientToken is the static public client token Kick's web bundle sends
	// on the viewer-token endpoint; it 403s without it. Captured from kick.com.
	xClientToken = "e1393935a959b4020a4491574f6490129f678acdaa92760471263db43487f823"

	wsHandshakeEvery = 12 * time.Second // ping/handshake cadence (alternated)
	wsUserEventEvery = 60 * time.Second // tracking.user.watch.livestream cadence
	wsMaxReconnect   = 6                // redial attempts before giving up
)

// ---- WS message builders (pure; unit-tested) ----

func wsHandshake(channelID int64) map[string]any {
	return map[string]any{
		"type": "channel_handshake",
		"data": map[string]any{"message": map[string]any{"channelId": channelID}},
	}
}

func wsPing() map[string]any { return map[string]any{"type": "ping"} }

// wsUserEvent emits the watch event. livestream_id is a JSON NUMBER, not a
// string — the verified-accruing reference sends the raw numeric id and the
// server does not credit a stringified id (live-confirmed 2026-06-13).
func wsUserEvent(channelID, livestreamID int64) map[string]any {
	return map[string]any{
		"type": "user_event",
		"data": map[string]any{"message": map[string]any{
			"name":          "tracking.user.watch.livestream",
			"channel_id":    channelID,
			"livestream_id": livestreamID,
		}},
	}
}

// ---- utls transport (Chrome-120 fingerprint, HTTP/1.1 ALPN for the WS upgrade) ----

// newUTLSConn dials TCP then wraps it with a Chrome-120 utls fingerprint,
// forcing http/1.1 ALPN (the WebSocket upgrade is http/1.1, and the token host
// is happy on 1.1). Mirrors the verified kickautodrops client.
func newUTLSConn(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	raw, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("split host: %w", err)
	}
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_120)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("chrome spec: %w", err)
	}
	// Strip h2 from ALPN + drop the ApplicationSettings (h2-only) extension so
	// the server picks http/1.1 — Go's stack won't auto-upgrade a custom-dialed
	// conn to h2, which would otherwise produce malformed responses.
	var cleaned []utls.TLSExtension
	for _, ext := range spec.Extensions {
		switch ext.(type) {
		case *utls.ALPNExtension:
			cleaned = append(cleaned, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
		case *utls.ApplicationSettingsExtension:
			// h2-only; skip
		default:
			cleaned = append(cleaned, ext)
		}
	}
	spec.Extensions = cleaned
	uc := utls.UClient(raw, &utls.Config{ServerName: host}, utls.HelloCustom)
	if err := uc.ApplyPreset(&spec); err != nil {
		raw.Close()
		return nil, fmt.Errorf("apply preset: %w", err)
	}
	if err := uc.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("utls handshake: %w", err)
	}
	return uc, nil
}

var (
	wsHTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{DialTLSContext: newUTLSConn},
	}
	wsDialer = &websocket.Dialer{
		NetDialTLSContext: newUTLSConn,
		HandshakeTimeout:  30 * time.Second,
	}
)

// fetchViewerToken gets a short-lived viewer-WS token. AUTHED: Bearer = full
// session_token + X-Client-Token (cookie-only auth 403s).
func fetchViewerToken(ctx context.Context, ks kickSession) (string, error) {
	cookieHeader, _, bearer := cookieHeaderFor(ks)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, websocketsBase+"/viewer/v1/token", nil)
	if err != nil {
		return "", err
	}
	ua := ks.UserAgent
	if ua == "" {
		ua = chromeUA
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("X-Client-Token", xClientToken)
	req.Header.Set("Referer", "https://kick.com/")
	req.Header.Set("Origin", "https://kick.com")
	req.Header.Set("Sec-Fetch-Site", "same-site")

	resp, err := wsHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("viewer token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("viewer token: status %d body %s", resp.StatusCode, truncate(body, 200))
	}
	var r struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode viewer token: %w", err)
	}
	if r.Data.Token == "" {
		return "", fmt.Errorf("viewer token: empty")
	}
	return r.Data.Token, nil
}

func dialViewerWS(ctx context.Context, ks kickSession, token string) (*websocket.Conn, error) {
	hdr := http.Header{}
	ua := ks.UserAgent
	if ua == "" {
		ua = chromeUA
	}
	hdr.Set("User-Agent", ua)
	hdr.Set("Origin", "https://kick.com")
	conn, resp, err := wsDialer.DialContext(ctx, wsConnectURL+"?token="+token, hdr)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ws dial: %w (status %d)", err, resp.StatusCode)
		}
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	return conn, nil
}

// ---- watch handle + loop ----

// kickWSWatch is stored in WatchHandle.Internal for the pure-WS path. The loop
// runs in its own goroutine; cancel stops it, done signals exit, err records a
// fatal failure so Heartbeat can report the watch dead (watcher then rotates).
type kickWSWatch struct {
	cancel  context.CancelFunc
	done    chan struct{}
	channel string

	mu  sync.Mutex
	err error
}

func (w *kickWSWatch) setErr(e error) {
	w.mu.Lock()
	if w.err == nil {
		w.err = e
	}
	w.mu.Unlock()
}

func (w *kickWSWatch) getErr() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// startWSWatch resolves the channel/livestream ids, fetches a viewer token,
// dials the WS, and spawns the presence loop. Returns once the connection is
// established; accrual happens in the background loop.
func (b *Backend) startWSWatch(ctx context.Context, ks kickSession, sess platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	slug := stream.Channel
	channelID, livestreamID, live, err := b.api.channelAndLivestream(ctx, sess, slug)
	if err != nil {
		return platform.WatchHandle{}, fmt.Errorf("kick ws watch: resolve %s: %w", slug, err)
	}
	if !live {
		return platform.WatchHandle{}, fmt.Errorf("kick ws watch: %s offline", slug)
	}
	token, err := fetchViewerToken(ctx, ks)
	if err != nil {
		return platform.WatchHandle{}, fmt.Errorf("kick ws watch: %w", err)
	}
	conn, err := dialViewerWS(ctx, ks, token)
	if err != nil {
		return platform.WatchHandle{}, fmt.Errorf("kick ws watch: %w", err)
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	w := &kickWSWatch{cancel: cancel, done: make(chan struct{}), channel: slug}
	slog.Info("kick ws watch started", "kind", "watch", "channel", slug,
		"channel_id", channelID, "livestream_id", livestreamID, "account", sess.AccountID)
	go b.runWSLoop(loopCtx, w, ks, conn, channelID, livestreamID)

	return platform.WatchHandle{Channel: slug, AccountID: sess.AccountID, Internal: w}, nil
}

// runWSLoop pumps the presence loop, reconnecting (with a fresh token) on a
// transient connection error up to wsMaxReconnect. A normal stop (ctx cancel)
// exits without recording an error; exhausting reconnects records the last
// error so Heartbeat reports the watch dead.
func (b *Backend) runWSLoop(ctx context.Context, w *kickWSWatch, ks kickSession, conn *websocket.Conn, channelID, livestreamID int64) {
	defer close(w.done)

	for attempt := 0; ; attempt++ {
		err := pumpWS(ctx, conn, channelID, livestreamID, wsHandshakeEvery, wsUserEventEvery)
		conn.Close()
		if ctx.Err() != nil {
			return // normal stop
		}
		if attempt >= wsMaxReconnect {
			w.setErr(fmt.Errorf("kick ws watch %s: gave up after %d reconnects: %w", w.channel, attempt, err))
			return
		}
		slog.Warn("kick ws watch reconnecting", "kind", "watch", "channel", w.channel,
			"attempt", attempt+1, "err", err)
		wait := time.Duration(2+rand.Intn(4)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		token, terr := fetchViewerToken(ctx, ks)
		if terr != nil {
			w.setErr(fmt.Errorf("kick ws watch %s reconnect token: %w", w.channel, terr))
			return
		}
		c, derr := dialViewerWS(ctx, ks, token)
		if derr != nil {
			w.setErr(fmt.Errorf("kick ws watch %s reconnect dial: %w", w.channel, derr))
			return
		}
		conn = c
	}
}

// pumpWS runs one connection's send loop: an initial handshake, then alternating
// handshake/ping every ~12s and a user_event every ~60s, draining any inbound
// frames (Kick only sends pong) with a short read deadline. Returns nil on ctx
// cancel, or the first write error (dead conn → caller reconnects).
func pumpWS(ctx context.Context, conn *websocket.Conn, channelID, livestreamID int64, handshakeEvery, userEventEvery time.Duration) error {
	// One reader goroutine drains inbound frames (Kick sends pong only) and
	// reports a dead connection. Only this loop writes, so read+write run
	// concurrently without violating gorilla's one-writer rule.
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				readErr <- err
				return
			}
		}
	}()

	// Initial handshake: register presence before the cadence kicks in.
	if err := conn.WriteJSON(wsHandshake(channelID)); err != nil {
		return fmt.Errorf("initial handshake: %w", err)
	}

	hbTick := time.NewTicker(handshakeEvery)
	defer hbTick.Stop()
	evTick := time.NewTicker(userEventEvery)
	defer evTick.Stop()

	counter := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			return fmt.Errorf("read: %w", err)
		case <-hbTick.C:
			counter++
			msg := wsHandshake(channelID)
			if counter%2 == 0 {
				msg = wsPing()
			}
			if err := conn.WriteJSON(msg); err != nil {
				return fmt.Errorf("write: %w", err)
			}
		case <-evTick.C:
			if livestreamID == 0 {
				continue
			}
			if err := conn.WriteJSON(wsUserEvent(channelID, livestreamID)); err != nil {
				return fmt.Errorf("write user_event: %w", err)
			}
		}
	}
}
