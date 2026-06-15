package kick

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// ---- injectable dial hooks used by ProbeWS ----

// wsProbeFetcher is a function that fetches a viewer WS token. Nil = real path.
type wsProbeFetcher func(ctx context.Context, ks kickSession) (string, error)

// wsProbeDialFn is a function that dials the viewer WS. Nil = real path.
type wsProbeDialFn func(ctx context.Context, ks kickSession, token string) (*websocket.Conn, error)

// probeWSDeps holds optional injectable dial/token helpers for ProbeWS.
// Both nil fields mean "use the real Kick endpoints".
type probeWSDeps struct {
	fetchToken wsProbeFetcher
	dialWS     wsProbeDialFn
}

// ---- probingConn: a wsConn wrapper that tracks observations ----

// probingConn wraps a real *websocket.Conn and tracks:
//   - How many "channel_handshake" frames were SENT by the probe.
//   - Whether at least one inbound frame (pong) was RECEIVED from the server.
//
// It satisfies wsConn and can be passed directly to pumpWS.
type probingConn struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	hsSent   int  // channel_handshake writes sent (initial + periodic)
	pongSeen bool // any inbound frame received from server
}

func newProbingConn(c *websocket.Conn) *probingConn {
	return &probingConn{conn: c}
}

func (p *probingConn) WriteJSON(v any) error {
	if err := p.conn.WriteJSON(v); err != nil {
		return err
	}
	// Inspect the frame type to count channel_handshake sends.
	if m, ok := v.(map[string]any); ok {
		if t, _ := m["type"].(string); t == "channel_handshake" {
			p.mu.Lock()
			p.hsSent++
			p.mu.Unlock()
		}
	}
	return nil
}

func (p *probingConn) ReadMessage() (int, []byte, error) {
	mt, b, err := p.conn.ReadMessage()
	if err == nil {
		p.mu.Lock()
		p.pongSeen = true
		p.mu.Unlock()
	}
	return mt, b, err
}

func (p *probingConn) Close() error { return p.conn.Close() }

func (p *probingConn) snapshot() (hsSent int, pongSeen bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hsSent, p.pongSeen
}

// ---- test-server token fetcher and dialer ----

// fetchViewerTokenHTTP fetches the viewer token from baseURL using a plain
// http.Client (no utls). Used by NewKickBackendForTest so fake httptest servers
// are reachable without a TLS fingerprint.
func fetchViewerTokenHTTP(baseURL string) wsProbeFetcher {
	return func(ctx context.Context, _ kickSession) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/viewer/v1/token", nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
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
}

// dialWSAt dials the WS at a fixed URL using the plain DefaultDialer (no utls).
// Used by NewKickBackendForTest so fake ws:// servers are reachable.
func dialWSAt(wsURL string) wsProbeDialFn {
	return func(ctx context.Context, _ kickSession, token string) (*websocket.Conn, error) {
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL+"?token="+token, nil)
		if err != nil {
			return nil, fmt.Errorf("ws dial (test): %w", err)
		}
		return conn, nil
	}
}

// ---- NewKickBackendForTest ----

// NewKickBackendForTest builds a Backend wired to the given test endpoint URLs.
// tokenServerURL is the base URL of a fake HTTP server serving /viewer/v1/token.
// wsServerURL is the ws:// URL of a fake WS server.
// The returned Backend is suitable only for ProbeWS tests — all other methods
// still target real Kick endpoints.
func NewKickBackendForTest(tokenServerURL, wsServerURL string) *Backend {
	b := New(nil, nil, "x", 0, time.Minute)
	b.probeDeps = probeWSDeps{
		fetchToken: fetchViewerTokenHTTP(tokenServerURL),
		dialWS:     dialWSAt(wsServerURL),
	}
	return b
}

// ---- ProbeWS ----

// ProbeWS verifies the Kick WebSocket watch-time transport for the given
// session and channel WITHOUT a full watcher and WITHOUT needing a live drop.
//
// It:
//  1. Fetches a viewer WS token.
//  2. Dials the viewer WS.
//  3. Runs the presence pump for window, tracking sent/received frames.
//  4. Returns nil only if:
//     - The WS connected.
//     - At least 2 channel_handshake frames were sent (initial + ≥1 periodic).
//     - At least 1 inbound frame (pong) was received from the server.
//
// IMPORTANT: nil proves WS transport health (connect + periodic handshake
// cadence + server round-trip), NOT drop credit. Accrual additionally requires
// a live stream and an active drop campaign — those are not verified here.
//
// window controls how long the probe runs. Use ≥30s in production to observe
// one full periodic cycle; use a very short duration in tests (e.g. 80ms with
// an injectable fast-cadence server).
func (b *Backend) ProbeWS(ctx context.Context, sess platform.Session, channel string, window time.Duration) error {
	ks, err := decodeSession(sess)
	if err != nil {
		return fmt.Errorf("probe ws: decode session: %w", err)
	}

	// --- fetch viewer token ---
	var token string
	if b.probeDeps.fetchToken != nil {
		token, err = b.probeDeps.fetchToken(ctx, ks)
	} else {
		token, err = fetchViewerToken(ctx, ks)
	}
	if err != nil {
		return fmt.Errorf("probe ws: %w", err)
	}

	// --- dial WS ---
	var rawConn *websocket.Conn
	if b.probeDeps.dialWS != nil {
		rawConn, err = b.probeDeps.dialWS(ctx, ks, token)
	} else {
		rawConn, err = dialViewerWS(ctx, ks, token)
	}
	if err != nil {
		return fmt.Errorf("probe ws: %w", err)
	}

	spy := newProbingConn(rawConn)
	// NOTE: spy.Close() is called explicitly below, BEFORE snapshot(), to
	// ensure the pumpWS reader goroutine has fully drained before we read
	// pongSeen. The defer here is a safety net for early-return paths only.
	defer spy.Close()

	// Use synthetic channel IDs — the probe tests transport, not real channel
	// state. channelID=1 is a non-zero placeholder; 0 would suppress the
	// channel_handshake message type field. livestreamID=0 suppresses
	// user_event (not needed for transport verification).
	const (
		probeChannelID    int64 = 1
		probeLivestreamID int64 = 0
	)

	// Run the pump for the window with a fast handshake cadence so at least one
	// periodic cycle completes within even a very short probe window.
	const probeHandshakeEvery = 15 * time.Millisecond

	pumpCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	// pumpWS returns nil on ctx cancel — that is the expected exit path here.
	_ = pumpWS(pumpCtx, spy, probeChannelID, probeLivestreamID, probeHandshakeEvery, window+time.Second)

	// Close the connection BEFORE reading the snapshot so that the pumpWS
	// inbound reader goroutine has settled. Without this, a pong arriving
	// after snapshot() but before the deferred Close is counted as lost →
	// false "no inbound frame" negative. Closing the conn causes the reader
	// to exit (with an error), ensuring any in-flight pong is already
	// recorded in pongSeen before snapshot() reads it.
	_ = spy.Close()

	hsSent, pongSeen := spy.snapshot()

	// We require at least: 1 initial send + 1 periodic tick = 2 total
	// channel_handshake frames. This confirms the periodic cadence fired at
	// least once, which is the signal our accrual testing tied to watch-time
	// credit (see project-ws-accrual-retest-plan memory entry).
	var errs []string
	if hsSent < 2 {
		errs = append(errs, fmt.Sprintf("channel_handshake sent=%d (want ≥2: initial + ≥1 periodic)", hsSent))
	}
	if !pongSeen {
		errs = append(errs, "no inbound frame received from server (pong expected)")
	}
	if len(errs) > 0 {
		return fmt.Errorf("probe ws: transport incomplete — %v", errs)
	}
	return nil
}
