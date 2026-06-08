package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestProgressBar(t *testing.T) {
	tests := []struct {
		cur, req, wantFilled int
	}{
		{0, 60, 0},
		{45, 60, 8},  // 0.75 → round to 8/10
		{60, 60, 10}, // full
		{90, 60, 10}, // clamp over-full
	}
	for _, tt := range tests {
		bar := progressBar(tt.cur, tt.req)
		assert.Equal(t, 10, len([]rune(bar)), "bar always 10 segments")
		assert.Equal(t, tt.wantFilled, strings.Count(bar, "▰"), "filled segments for %d/%d", tt.cur, tt.req)
	}
	assert.Equal(t, "", progressBar(10, 0), "no bar when req<=0")
}

func TestBuildEmbed_ProgressEvent(t *testing.T) {
	embed := buildEmbed(EventProgress, map[string]any{
		"account_label": "@TTik3r",
		"platform":      "twitch",
		"game":          "League of Legends",
		"campaign":      "Summer Drops 2026",
		"drop":          "Hextech Chest",
		"channel":       "riotgames",
		"image":         "https://img/x.png",
		"cur_min":       45,
		"req_min":       60,
	})

	assert.Equal(t, 0x9146FF, embed["color"], "twitch purple accent")
	assert.Equal(t, "Hextech Chest", embed["title"], "title is the drop name")

	author := embed["author"].(map[string]any)
	assert.Equal(t, "League of Legends · Twitch", author["name"])

	footer := embed["footer"].(map[string]any)
	assert.Equal(t, "GrubDrops • Summer Drops 2026", footer["text"])

	thumb := embed["thumbnail"].(map[string]any)
	assert.Equal(t, "https://img/x.png", thumb["url"])

	fields := embed["fields"].([]map[string]any)
	byName := map[string]string{}
	for _, f := range fields {
		byName[f["name"].(string)] = f["value"].(string)
	}
	assert.Equal(t, "@TTik3r", byName["Account"])
	assert.Contains(t, byName["Channel"], "riotgames")
	require.Contains(t, byName, "⏳ Mining")
	assert.Contains(t, byName["⏳ Mining"], "45/60")
	assert.Contains(t, byName["⏳ Mining"], "▰")
}

func TestBuildEmbed_ClaimEvent(t *testing.T) {
	embed := buildEmbed(EventClaim, map[string]any{
		"platform": "kick",
		"game":     "Rust",
		"drop":     "Crate",
		"cur_min":  60,
		"req_min":  60,
	})
	assert.Equal(t, 0x23A55A, embed["color"], "claim is green regardless of platform")
	fields := embed["fields"].([]map[string]any)
	var progLabel, progVal string
	for _, f := range fields {
		if n := f["name"].(string); n == "✅ Claimed" {
			progLabel, progVal = n, f["value"].(string)
		}
	}
	assert.Equal(t, "✅ Claimed", progLabel)
	assert.Equal(t, 10, strings.Count(progVal, "▰"), "claim bar is full")
}

func TestBuildEmbed_AuthFallback(t *testing.T) {
	// auth/error events carry no drop/game — keep the simple title+desc path.
	embed := buildEmbed(EventAuth, map[string]any{"msg": "token refreshed"})
	assert.Equal(t, "Auth event", embed["title"])
	_, hasFields := embed["fields"]
	_, hasDesc := embed["description"]
	assert.True(t, hasFields || hasDesc, "renders something")
}

func TestDiscordWebhook_SetsUsernameAndAvatar(t *testing.T) {
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

	n := NewDiscordWebhook(srv.URL, nil)
	n.Username = "GrubDrops"
	n.AvatarURL = "https://img/avatar.png"
	require.NoError(t, n.Notify(context.Background(), EventClaim, map[string]any{"account": "a"}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "GrubDrops", got["username"])
	assert.Equal(t, "https://img/avatar.png", got["avatar_url"])
}

func TestDiscordWebhook_OmitsBrandingWhenUnset(t *testing.T) {
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

	n := NewDiscordWebhook(srv.URL, nil)
	require.NoError(t, n.Notify(context.Background(), EventClaim, map[string]any{"account": "a"}))

	mu.Lock()
	defer mu.Unlock()
	_, hasUser := got["username"]
	_, hasAvatar := got["avatar_url"]
	assert.False(t, hasUser, "no username key when unset")
	assert.False(t, hasAvatar, "no avatar_url key when unset")
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
