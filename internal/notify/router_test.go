package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureNotifier struct {
	mu     sync.Mutex
	events []map[string]any
}

func (c *captureNotifier) Notify(_ context.Context, event string, fields map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	fields["__event"] = event
	c.events = append(c.events, fields)
	return nil
}

func TestRouter_RoutesToPerAccountURL(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	fallback := &captureNotifier{}
	r := NewAccountRouted(fallback, func(accountID string) string {
		if accountID == "acc1" {
			return srv.URL
		}
		return ""
	}, nil)

	require.NoError(t, r.Notify(context.Background(), EventClaim, map[string]any{
		"account": "acc1", "benefit": "b1",
	}))
	assert.EqualValues(t, 1, atomic.LoadInt32(&hits))
	assert.Empty(t, fallback.events)
}

func TestRouter_PropagatesBrandingToPerAccountClient(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(body, &got)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := NewAccountRouted(&captureNotifier{}, func(string) string { return srv.URL }, nil)
	r.Username = "GrubDrops"
	r.AvatarURL = "https://img/a.png"

	require.NoError(t, r.Notify(context.Background(), EventClaim, map[string]any{"account": "acc1"}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "GrubDrops", got["username"])
	assert.Equal(t, "https://img/a.png", got["avatar_url"])
}

func TestRouter_FallsBackWhenNoAccountURL(t *testing.T) {
	fallback := &captureNotifier{}
	r := NewAccountRouted(fallback, func(_ string) string { return "" }, nil)

	require.NoError(t, r.Notify(context.Background(), EventState, map[string]any{
		"account": "acc1", "state": "watching",
	}))
	assert.Len(t, fallback.events, 1)
}

func TestRouter_FallsBackWhenAccountFieldMissing(t *testing.T) {
	fallback := &captureNotifier{}
	r := NewAccountRouted(fallback, func(_ string) string { return "http://should-not-be-used" }, nil)

	require.NoError(t, r.Notify(context.Background(), EventError, map[string]any{
		"panic": "oh no",
	}))
	assert.Len(t, fallback.events, 1)
}
