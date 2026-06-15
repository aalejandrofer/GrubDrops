package kick

// ws_frame_replay_test.go — CI regression guard for inbound WS frame handling.
//
// Loads recorded inbound frames from testdata/ws_frames.json and replays them
// through the real inbound-recognition logic (probingConn.ReadMessage) using a
// loopback httptest WS server — no external network.
//
// OUTBOUND frame shapes (wsHandshake / wsPing / wsUserEvent JSON, livestream_id
// as a JSON number) are already fully covered by TestWSMessageShapes in
// wswatch_test.go — this file does NOT duplicate them.
//
// What this adds:
//  1. A versioned fixture file (testdata/ws_frames.json) that documents the
//     real server's inbound frame shapes — any format change forces a fixture
//     update, making regressions visible.
//  2. Replay of every fixture frame through probingConn.ReadMessage to confirm
//     the pong-detection contract holds for all known frame types (pong,
//     connected, channel_handshake_ack, unknown future events).
//  3. A guard for the ReadMessage error-branch: an injected read error must NOT
//     set pongSeen (the detection must only fire on a successful read).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsFixtureFrame mirrors one entry in testdata/ws_frames.json.
type wsFixtureFrame struct {
	Frame    json.RawMessage `json:"frame"`
	Desc     string          `json:"desc"`
	WantPong bool            `json:"wantPong"`
}

// loadWSFrameFixture reads and parses testdata/ws_frames.json.
func loadWSFrameFixture(t *testing.T) []wsFixtureFrame {
	t.Helper()
	path := filepath.Join("testdata", "ws_frames.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "fixture file must be readable: %s", path)
	var frames []wsFixtureFrame
	require.NoError(t, json.Unmarshal(data, &frames), "fixture must parse as []wsFixtureFrame")
	require.NotEmpty(t, frames, "fixture must have at least one frame")
	return frames
}

// frameReplayServer upgrades the first incoming WS connection, sends each
// provided raw JSON frame as a text message, then closes the connection.
func frameReplayServer(t *testing.T, rawFrames []json.RawMessage) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for _, raw := range rawFrames {
			if werr := c.WriteMessage(websocket.TextMessage, raw); werr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsURL converts an http://… test server URL to ws://….
func wsURLFrom(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

// TestWSInboundFrameReplay is the core regression guard:
//
//   - Loads all fixture frames.
//   - Starts a loopback WS server that sends them all.
//   - Connects a probingConn to the server.
//   - Reads all frames via probingConn.ReadMessage.
//   - Asserts pongSeen matches wantPong for each fixture entry (they all expect
//     true — any inbound frame counts as "server acknowledged presence").
func TestWSInboundFrameReplay(t *testing.T) {
	fixtures := loadWSFrameFixture(t)

	// Collect raw frames to send.
	rawFrames := make([]json.RawMessage, len(fixtures))
	for i, f := range fixtures {
		rawFrames[i] = f.Frame
	}

	srv := frameReplayServer(t, rawFrames)
	conn, _, err := websocket.DefaultDialer.Dial(wsURLFrom(srv), nil)
	require.NoError(t, err)
	defer conn.Close()

	spy := newProbingConn(conn)

	// Read each frame through the real recognition path.
	for i, fix := range fixtures {
		t.Run(fix.Desc, func(t *testing.T) {
			mt, b, readErr := spy.ReadMessage()
			require.NoError(t, readErr, "frame %d (%s): unexpected read error", i, fix.Desc)
			assert.Equal(t, websocket.TextMessage, mt, "frame %d: expected text message", i)

			// The frame bytes must round-trip as valid JSON.
			var decoded map[string]any
			assert.NoError(t, json.Unmarshal(b, &decoded), "frame %d: payload must be valid JSON", i)

			// After each successful read the pongSeen flag must be set.
			_, pongSeen := spy.snapshot()
			assert.Equal(t, fix.WantPong, pongSeen,
				"frame %d (%s): pongSeen mismatch", i, fix.Desc)
		})
	}

	// Sanity: after all frames pongSeen must be true.
	_, pongSeen := spy.snapshot()
	assert.True(t, pongSeen, "pongSeen must be true after all fixture frames replayed")
}

// TestWSInboundReadError_NoPongSeen guards the error branch:
// a read error must NOT set pongSeen (the flag only fires on a clean read).
// This test uses a server that immediately closes the connection after upgrade,
// forcing a read error on the first ReadMessage call.
func TestWSInboundReadError_NoPongSeen(t *testing.T) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately — next ReadMessage on the client side will error.
		c.Close()
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial(wsURLFrom(srv), nil)
	require.NoError(t, err)
	defer conn.Close()

	spy := newProbingConn(conn)

	_, _, readErr := spy.ReadMessage()
	assert.Error(t, readErr, "expected a read error from closed server connection")

	_, pongSeen := spy.snapshot()
	assert.False(t, pongSeen, "pongSeen must remain false when ReadMessage returns an error")
}
