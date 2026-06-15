package canary_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// wsUpgrader shared across helpers.
var wsUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// kickFakeServer is a WS server for KickProbe tests. It performs the expected
// Kick frame exchange: receives auth/handshake frames from the probe and replies
// with pong messages so the probe can confirm both directions of the transport.
//
// The server records every frame type it receives so the test can assert the
// probe actually sent the expected cadence.
type kickFakeServer struct {
	srv       *httptest.Server
	typesRecv []string // frame types received FROM the probe
	pongsRecv int      // "pong" messages the probe receives (sent by this server)
}

// newKickFakeWS starts a fake Kick WS server. It:
//   - Upgrades the incoming HTTP connection to WS
//   - Reads frames from the probe (recording their "type")
//   - After seeing an initial "channel_handshake", sends a pong reply, then
//     continues sending pong replies on a short cadence so the probe can observe
//     them during its window.
//
// The server deliberately keeps running until the connection closes (probe
// cancels the context and closes the connection). t.Cleanup shuts it down.
func newKickFakeWS(t *testing.T) *kickFakeServer {
	t.Helper()
	f := &kickFakeServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Goroutine: pump pong frames every 5ms so the probe's reader sees them.
		pongDone := make(chan struct{})
		go func() {
			defer close(pongDone)
			tick := time.NewTicker(5 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-tick.C:
					if err := c.WriteJSON(map[string]any{"type": "pong"}); err != nil {
						return
					}
				}
			}
		}()

		// Main: read frames from the probe, record types.
		for {
			var m map[string]any
			if err := c.ReadJSON(&m); err != nil {
				return
			}
			typ, _ := m["type"].(string)
			f.typesRecv = append(f.typesRecv, typ)
		}
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *kickFakeServer) wsURL() string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http")
}

// newKickFakeHTTP builds a fake HTTP server for the viewer-token endpoint.
// It returns a fixed token so dialViewerWS can proceed.
func newKickFakeHTTP(t *testing.T, token string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Viewer token endpoint: /viewer/v1/token
		if strings.HasSuffix(r.URL.Path, "/token") {
			resp := map[string]any{
				"data": map[string]any{"token": token},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestKickProbe_OK verifies that KickProbe.Run returns Result{OK: true} when the
// fake WS server correctly performs the Kick viewer presence exchange:
//   - Probe connects and sends the initial channel_handshake
//   - Server replies with pong frames
//   - Probe sends at least one periodic channel_handshake during the window
//   - Probe observes the pong
func TestKickProbe_OK(t *testing.T) {
	fakeWS := newKickFakeWS(t)
	fakeHTTP := newKickFakeHTTP(t, "test-viewer-token")

	probe := canary.NewKickProbeForTest(fakeHTTP.URL, fakeWS.wsURL())
	sess := platform.Session{
		AccountID: "test-acc",
		Cookies:   map[string]string{"kick": `{"cookies":[{"name":"session_token","value":"tok","domain":".kick.com","path":"/"}]}`},
	}
	result := probe.Run(context.Background(), sess, "testchannel")

	assert.True(t, result.OK, "expected OK=true; detail: %s", result.Detail)
	assert.Contains(t, result.Detail, "channel_handshake", "detail should mention channel_handshake")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")

	// The server should have seen at least one channel_handshake from the probe.
	require.NotEmpty(t, fakeWS.typesRecv, "server should have received frames from the probe")
	assert.Contains(t, fakeWS.typesRecv, "channel_handshake",
		"probe must send at least one channel_handshake; received: %v", fakeWS.typesRecv)
}

// TestKickProbe_ConnFail verifies that KickProbe.Run returns Result{OK: false}
// when the WS endpoint is unreachable (bad address → dial error).
func TestKickProbe_ConnFail(t *testing.T) {
	fakeHTTP := newKickFakeHTTP(t, "test-viewer-token")

	// Point the WS URL at a port that's not listening.
	probe := canary.NewKickProbeForTest(fakeHTTP.URL, "ws://127.0.0.1:1")
	sess := platform.Session{
		AccountID: "test-acc",
		Cookies:   map[string]string{"kick": `{"cookies":[{"name":"session_token","value":"tok","domain":".kick.com","path":"/"}]}`},
	}
	result := probe.Run(context.Background(), sess, "testchannel")

	assert.False(t, result.OK, "expected OK=false when WS endpoint is unreachable")
	assert.NotEmpty(t, result.Detail, "detail should describe the failure")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")
}

// TestKickProbe_NoPong verifies that KickProbe.Run returns Result{OK: false}
// when the server never sends a pong (probe observes no inbound frames).
func TestKickProbe_NoPong(t *testing.T) {
	// Server that accepts connections but never sends anything back (read-only).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Read frames but never reply.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	fakeHTTP := newKickFakeHTTP(t, "test-viewer-token")
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	probe := canary.NewKickProbeForTest(fakeHTTP.URL, wsURL)
	sess := platform.Session{
		AccountID: "test-acc",
		Cookies:   map[string]string{"kick": `{"cookies":[{"name":"session_token","value":"tok","domain":".kick.com","path":"/"}]}`},
	}
	result := probe.Run(context.Background(), sess, "testchannel")

	assert.False(t, result.OK, "expected OK=false when server sends no pong")
	assert.False(t, result.CheckedAt.IsZero(), "CheckedAt should be set")
}
