package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GQLSendsClientIDAndHash(t *testing.T) {
	var got struct {
		clientID string
		body     []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.clientID = r.Header.Get("Client-Id")
		got.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out struct {
		Ok bool `json:"ok"`
	}
	require.NoError(t, c.gql(context.Background(), "", OpInventory, nil, &out))

	assert.Equal(t, clientID, got.clientID)
	assert.Contains(t, string(got.body), `"operationName":"Inventory"`)
	assert.Contains(t, string(got.body), `"sha256Hash":"`+OpInventory.Hash+`"`)
	assert.True(t, out.Ok)
}

func TestClient_GQLReturnsErrorOnGQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"PersistedQueryNotFound"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out json.RawMessage
	err := c.gql(context.Background(), "", OpInventory, nil, &out)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "PersistedQueryNotFound"))
}

func TestClient_GQLSendsBearerWhenTokenProvided(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out json.RawMessage
	require.NoError(t, c.gql(context.Background(), "abc123", OpInventory, nil, &out))
	assert.Equal(t, "OAuth abc123", gotAuth)
}

func TestClient_GQLQueryNonPersisted(t *testing.T) {
	var got struct {
		body []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out struct {
		Ok bool `json:"ok"`
	}
	require.NoError(t, c.gqlQuery(context.Background(), "tok", "FooOp", "mutation FooOp { foo }", map[string]any{"x": 1}, &out))
	body := string(got.body)
	assert.Contains(t, body, `"operationName":"FooOp"`)
	assert.Contains(t, body, `"query":"mutation FooOp { foo }"`)
	assert.Contains(t, body, `"x":1`)
}

// TestClient_ResolveSpadeURL_Inline verifies resolveSpadeURL extracts
// the beacon URL when Twitch inlines "spade_url" directly in the channel
// page HTML.
func TestClient_ResolveSpadeURL_Inline(t *testing.T) {
	const wantURL = "https://video-edge-abc.spade.twitch.tv/spade"
	mux := http.NewServeMux()
	mux.HandleFunc("/somechannel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html>"spade_url": "`+wantURL+`"</html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.homeURL = srv.URL
	got, err := c.resolveSpadeURL(context.Background(), "tok", "somechannel")
	require.NoError(t, err)
	assert.Equal(t, wantURL, got)
}

// TestClient_ResolveSpadeURL_SettingsFallback verifies the two-step
// path: when the channel page only carries a settings.<hex>.js bundle
// URL (no inline spade_url), resolveSpadeURL fetches that bundle and
// extracts the beacon URL from it.
func TestClient_ResolveSpadeURL_SettingsFallback(t *testing.T) {
	const wantURL = "https://video-edge-xyz.spade.twitch.tv/spade"
	var srv *httptest.Server
	srvBase := func() string { return srv.URL }
	mux := http.NewServeMux()
	mux.HandleFunc("/fallbackchannel", func(w http.ResponseWriter, r *http.Request) {
		// No inline spade_url — only a settings bundle reference.
		_, _ = io.WriteString(w, `<html><script src="`+srvBase()+`/config/settings.1234567890abcdef1234567890abcdef.js"></script></html>`)
	})
	mux.HandleFunc("/config/settings.1234567890abcdef1234567890abcdef.js", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `window.settings = {"beacon_url": "`+wantURL+`"};`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.homeURL = srv.URL
	got, err := c.resolveSpadeURL(context.Background(), "tok", "fallbackchannel")
	require.NoError(t, err)
	assert.Equal(t, wantURL, got)
}

// TestClient_ResolveSpadeURL_NotFound verifies resolveSpadeURL errors
// when neither the page nor a settings bundle carries a beacon URL.
func TestClient_ResolveSpadeURL_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/deadchannel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html>no analytics here</html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.homeURL = srv.URL
	_, err := c.resolveSpadeURL(context.Background(), "tok", "deadchannel")
	require.Error(t, err)
}
