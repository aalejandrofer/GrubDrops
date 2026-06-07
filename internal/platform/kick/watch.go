package kick

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Kick viewer-presence websocket — how drops watch-time accrues. Flow (RE'd from
// the Next.js bundle + live DevTools frames):
//  1. GET https://websockets.kick.com/viewer/v1/token (X-CLIENT-TOKEN header,
//     cookies, NO Authorization Bearer) -> {data:{token}}
//  2. wss://websockets.kick.com/viewer/v1/connect?token=<token> (standard TLS,
//     Cookie + Origin headers; CF allows the upgrade without the utls fingerprint)
//  3. repeatedly send {"type":"channel_handshake","data":{"message":
//     {"channelId":"<id>"}}} + {"type":"ping"} (server replies {"type":"pong"}).
const (
	kickWSClientToken  = "e1393935a959b4020a4491574f6490129f678acdaa92760471263db43487f823"
	kickViewerTokenURL = "https://websockets.kick.com/viewer/v1/token"
	kickViewerWSURL    = "wss://websockets.kick.com/viewer/v1/connect?token="
	kickHandshakeEvery = 8 * time.Second
	kickPingEvery      = 12 * time.Second
)

// watchConn holds an active viewer-WS presence for one channel.
type watchConn struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	alive  atomic.Bool
}

// openWatch establishes the viewer-WS presence for channelID and starts the
// handshake/ping loop in the background. Returns once connected.
func openWatch(ctx context.Context, cookieHeader, channelID string) (*watchConn, error) {
	if channelID == "" {
		return nil, fmt.Errorf("kick watch: empty channel id")
	}
	tok, err := fetchViewerToken(ctx, cookieHeader)
	if err != nil {
		return nil, fmt.Errorf("viewer token: %w", err)
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{NextProtos: []string{"http/1.1"}},
		HandshakeTimeout: 15 * time.Second,
	}
	hdr := http.Header{}
	hdr.Set("User-Agent", chromeUA)
	hdr.Set("Origin", "https://kick.com")
	hdr.Set("Cookie", cookieHeader)
	conn, resp, err := dialer.DialContext(ctx, kickViewerWSURL+tok, hdr)
	if err != nil {
		st := 0
		if resp != nil {
			st = resp.StatusCode
		}
		return nil, fmt.Errorf("viewer ws dial (status %d): %w", st, err)
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	w := &watchConn{conn: conn, cancel: cancel}
	w.alive.Store(true)

	hs, _ := json.Marshal(map[string]any{
		"type": "channel_handshake",
		"data": map[string]any{"message": map[string]any{"channelId": channelID}},
	})
	ping, _ := json.Marshal(map[string]any{"type": "ping"})

	// reader: drain server frames; mark dead on error.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				w.alive.Store(false)
				return
			}
		}
	}()
	// writer: initial handshake, then handshake + ping on intervals.
	go func() {
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, hs)
		hsT := time.NewTicker(kickHandshakeEvery)
		pingT := time.NewTicker(kickPingEvery)
		defer hsT.Stop()
		defer pingT.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-hsT.C:
				if err := conn.WriteMessage(websocket.TextMessage, hs); err != nil {
					w.alive.Store(false)
					return
				}
			case <-pingT.C:
				if err := conn.WriteMessage(websocket.TextMessage, ping); err != nil {
					w.alive.Store(false)
					return
				}
			}
		}
	}()
	slog.Info("kick watch presence opened", "channel_id", channelID)
	return w, nil
}

func (w *watchConn) Alive() bool { return w != nil && w.alive.Load() }

func (w *watchConn) Close() {
	if w == nil {
		return
	}
	w.alive.Store(false)
	w.cancel()
	_ = w.conn.Close()
}

// fetchViewerToken GETs the per-session viewer token over a Chrome-fingerprinted
// (utls) HTTP/2 conn. Crucially sends X-CLIENT-TOKEN + cookies but NO bearer
// (the bearer 403s this endpoint).
func fetchViewerToken(ctx context.Context, cookieHeader string) (string, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var d net.Dialer
	tcp, err := d.DialContext(dialCtx, "tcp", "websockets.kick.com:443")
	if err != nil {
		return "", err
	}
	uc := utls.UClient(tcp, &utls.Config{ServerName: "websockets.kick.com", NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
	if err := uc.HandshakeContext(dialCtx); err != nil {
		uc.Close()
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, kickViewerTokenURL, nil)
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Origin", "https://kick.com")
	req.Header.Set("Referer", "https://kick.com/")
	req.Header.Set("X-CLIENT-TOKEN", kickWSClientToken)
	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(uc)
	if err != nil {
		uc.Close()
		return "", err
	}
	resp, err := cc.RoundTrip(req)
	if err != nil {
		uc.Close()
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("viewer token status %d", resp.StatusCode)
	}
	var r struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Data.Token == "" {
		return "", fmt.Errorf("viewer token: bad body")
	}
	return r.Data.Token, nil
}

// cookieHeaderForSession is a small helper so backend code can get the raw
// Cookie header for a session (used by the viewer WS).
func cookieHeaderForSession(s platform.Session) string {
	ks, err := decodeSession(s)
	if err != nil {
		return ""
	}
	h, _, _ := cookieHeaderFor(ks)
	return h
}
