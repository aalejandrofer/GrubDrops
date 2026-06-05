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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/dropsminer/internal/platform"
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
