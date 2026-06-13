package kick

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWSMessageShapes(t *testing.T) {
	// channel_id must serialize as a JSON number, livestream_id as a string —
	// Kick rejects the event otherwise.
	hb, _ := json.Marshal(wsHandshake(1727361))
	assert.JSONEq(t, `{"type":"channel_handshake","data":{"message":{"channelId":1727361}}}`, string(hb))

	pb, _ := json.Marshal(wsPing())
	assert.JSONEq(t, `{"type":"ping"}`, string(pb))

	ub, _ := json.Marshal(wsUserEvent(1727361, 112557430))
	// livestream_id is a JSON number, not a string — the server only credits
	// the numeric form (live-confirmed 2026-06-13).
	assert.JSONEq(t, `{"type":"user_event","data":{"message":{"name":"tracking.user.watch.livestream","channel_id":1727361,"livestream_id":112557430}}}`, string(ub))
}

// fakeWSServer upgrades incoming connections and records every JSON frame's
// "type" (plus the last user_event payload) so tests can assert the loop.
type fakeWSServer struct {
	srv *httptest.Server

	mu        sync.Mutex
	types     []string
	lastEvent map[string]any
}

func newFakeWSServer(t *testing.T) *fakeWSServer {
	t.Helper()
	f := &fakeWSServer{}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			var m map[string]any
			if err := c.ReadJSON(&m); err != nil {
				return
			}
			typ, _ := m["type"].(string)
			f.mu.Lock()
			f.types = append(f.types, typ)
			if typ == "user_event" {
				if d, ok := m["data"].(map[string]any); ok {
					if msg, ok := d["message"].(map[string]any); ok {
						f.lastEvent = msg
					}
				}
			}
			f.mu.Unlock()
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWSServer) wsURL() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

func (f *fakeWSServer) seen() (types []string, last map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.types...), f.lastEvent
}

func TestPumpWS_SendsHandshakePingAndUserEvent(t *testing.T) {
	f := newFakeWSServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(f.wsURL(), nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// Fast cadences: handshake/ping ~10ms, user_event ~25ms.
		done <- pumpWS(ctx, conn, 1727361, 112557430, 10*time.Millisecond, 25*time.Millisecond)
	}()

	// Let several cycles run, then stop.
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case perr := <-done:
		assert.NoError(t, perr, "pumpWS should return nil on ctx cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("pumpWS did not return after cancel")
	}

	types, last := f.seen()
	assert.Contains(t, types, "channel_handshake", "must send periodic handshakes")
	assert.Contains(t, types, "ping", "must alternate pings")
	assert.Contains(t, types, "user_event", "must emit watch user_event")
	require.NotNil(t, last, "user_event payload recorded")
	assert.Equal(t, "tracking.user.watch.livestream", last["name"])
	assert.EqualValues(t, 112557430, last["livestream_id"])
	assert.EqualValues(t, 1727361, last["channel_id"])

	// First frame is always a handshake (presence before anything else).
	assert.Equal(t, "channel_handshake", types[0])
}

func TestKickWSWatch_Lifecycle(t *testing.T) {
	w := &kickWSWatch{done: make(chan struct{})}
	assert.NoError(t, w.getErr())

	// First error sticks; later ones don't overwrite.
	w.setErr(assertErr("boom"))
	w.setErr(assertErr("second"))
	require.Error(t, w.getErr())
	assert.Equal(t, "boom", w.getErr().Error())
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
