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
