package twitch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestWatch_HeartbeatSendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	wt := &watch{c: c, cachedUserID: 12345} // skip CurrentUser fetch
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tok123"},
		platform.Stream{Channel: "fakestreamer"})
	require.NoError(t, err)
	require.NoError(t, wt.heartbeat(context.Background(), h))

	require.Equal(t, "OAuth tok123", gotAuth)
}

// TestWatch_HeartbeatBeaconShapeGolden is a regression guard for the full
// beacon request shape. It pins everything the two focused tests above do NOT
// already assert:
//
//   - HTTP method (POST) and Content-Type (text/plain;charset=UTF-8)
//   - Required identity headers: Client-Id (Android app ID), User-Agent
//   - Non-persisted body: query field present (mutation text), no extensions.persistedQuery
//   - Decoded event properties: user_id, is_live, logged_in, minutes_logged,
//     channel_id, broadcast_id
//
// The test passes immediately because it guards existing behaviour — that is
// intentional for a regression guard. A future change that accidentally alters
// the mutation text, removes an identity header, or drops a required event
// property will cause this test to fail.
func TestWatch_HeartbeatBeaconShapeGolden(t *testing.T) {
	var captured struct {
		method      string
		contentType string
		clientID    string
		userAgent   string
		body        []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.contentType = r.Header.Get("Content-Type")
		captured.clientID = r.Header.Get("Client-Id")
		captured.userAgent = r.Header.Get("User-Agent")
		captured.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
	}))
	defer srv.Close()

	const (
		wantChannelID   = "chan99"
		wantBroadcastID = "bcast42"
		wantUserID      = int64(12345)
		wantChannel     = "goldenstreamer"
	)

	c := newTestClient(srv.URL)
	wt := &watch{c: c, cachedUserID: wantUserID}
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tokgolden"},
		platform.Stream{
			Channel:     wantChannel,
			ChannelID:   wantChannelID,
			BroadcastID: wantBroadcastID,
		})
	require.NoError(t, err)
	require.NoError(t, wt.heartbeat(context.Background(), h))

	// ── HTTP transport ────────────────────────────────────────────────────────
	assert.Equal(t, http.MethodPost, captured.method, "beacon must be POST")
	assert.Equal(t, "text/plain;charset=UTF-8", captured.contentType,
		"Content-Type must be text/plain;charset=UTF-8 (matches Android client profile)")

	// ── Identity headers ─────────────────────────────────────────────────────
	assert.Equal(t, clientID, captured.clientID,
		"Client-Id must be the Android app client ID")
	assert.Equal(t, userAgent, captured.userAgent,
		"User-Agent must be the Android Dalvik user-agent")

	// ── Envelope: non-persisted shape ────────────────────────────────────────
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(captured.body, &envelope), "body must be valid JSON")

	// Must carry the full mutation query text, NOT a persisted-query hash.
	var queryStr string
	require.NoError(t, json.Unmarshal(envelope["query"], &queryStr), "query field must be a string")
	assert.True(t, strings.Contains(queryStr, "sendSpadeEvents"),
		"query must contain the sendSpadeEvents mutation")
	assert.True(t, strings.Contains(queryStr, "SendEvents"),
		"query must declare mutation SendEvents")
	_, hasExtensions := envelope["extensions"]
	assert.False(t, hasExtensions,
		"non-persisted gqlQuery must NOT include extensions.persistedQuery")

	// ── Decoded event properties ──────────────────────────────────────────────
	var req struct {
		Variables struct {
			Input struct {
				Data string `json:"data"`
			} `json:"input"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(captured.body, &req))

	raw, err := base64.StdEncoding.DecodeString(req.Variables.Input.Data)
	require.NoError(t, err)
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	plain, err := io.ReadAll(gr)
	require.NoError(t, err)

	var events []map[string]any
	require.NoError(t, json.Unmarshal(plain, &events))
	require.Len(t, events, 1)

	props, ok := events[0]["properties"].(map[string]any)
	require.True(t, ok, "event must have a properties object")

	// Required accrual fields — Twitch silently discards heartbeats without these.
	assert.EqualValues(t, wantUserID, props["user_id"],
		"user_id must match the authenticated user's numeric Twitch ID")
	assert.Equal(t, wantChannelID, props["channel_id"],
		"channel_id must be propagated from stream metadata")
	assert.Equal(t, wantBroadcastID, props["broadcast_id"],
		"broadcast_id must be propagated from stream metadata")
	assert.EqualValues(t, 1, props["minutes_logged"],
		"minutes_logged must be 1 per heartbeat")
	assert.Equal(t, true, props["is_live"],
		"is_live must be true for live-stream heartbeats")
	assert.Equal(t, true, props["logged_in"],
		"logged_in must be true for authenticated sessions")
}

func TestWatch_HeartbeatSendsGzippedBase64Mutation(t *testing.T) {
	var got struct {
		body []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	wt := &watch{c: c, cachedUserID: 12345}
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tok"},
		platform.Stream{Channel: "fakestreamer"})
	require.NoError(t, err)

	require.NoError(t, wt.heartbeat(context.Background(), h))

	var req struct {
		OperationName string `json:"operationName"`
		Variables     struct {
			Input struct {
				Data       string `json:"data"`
				Repository string `json:"repository"`
				Encoding   string `json:"encoding"`
			} `json:"input"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(got.body, &req))
	assert.Equal(t, "SendEvents", req.OperationName)
	assert.Equal(t, "twilight", req.Variables.Input.Repository)
	assert.Equal(t, "GZIP_B64", req.Variables.Input.Encoding)

	raw, err := base64.StdEncoding.DecodeString(req.Variables.Input.Data)
	require.NoError(t, err)
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	plain, err := io.ReadAll(gr)
	require.NoError(t, err)

	var events []map[string]any
	require.NoError(t, json.Unmarshal(plain, &events))
	require.Len(t, events, 1)
	assert.Equal(t, "minute-watched", events[0]["event"])
	props := events[0]["properties"].(map[string]any)
	assert.Equal(t, "fakestreamer", props["channel"])
}
