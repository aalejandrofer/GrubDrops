package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscordWebhook_SendsExpectedPayload(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		mu.Lock()
		got = append(got, payload)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewDiscordWebhook(srv.URL, nil)
	require.NoError(t, n.Notify(context.Background(), EventClaim, map[string]any{
		"account": "acc1", "benefit": "ben_helmet",
	}))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 1)
	embeds, _ := got[0]["embeds"].([]any)
	require.Len(t, embeds, 1)
	embed := embeds[0].(map[string]any)
	assert.Equal(t, "Drop claimed", embed["title"])
}

func TestDiscordWebhook_DropsBelowVerbosity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not have been called")
	}))
	defer srv.Close()

	n := NewDiscordWebhook(srv.URL, &VerbosityFilter{Allow: map[string]bool{
		EventClaim: true,
	}})
	require.NoError(t, n.Notify(context.Background(), EventState, map[string]any{}))
}
