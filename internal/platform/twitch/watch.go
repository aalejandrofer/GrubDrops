package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type watch struct {
	c *client

	userIDMu     sync.Mutex
	cachedUserID int64

	// spadeMu guards spadeURLs, a per-channel cache of the Spade
	// minute-watched beacon URL. The beacon URL is scraped from each
	// channel's page (see client.resolveSpadeURL) and is stable for the
	// channel, so it's worth caching to avoid an extra HTTP GET per
	// heartbeat. An entry is evicted on a failed beacon so the next
	// heartbeat re-resolves from a fresh page fetch.
	spadeMu   sync.Mutex
	spadeURLs map[string]string
}

func newWatch() *watch {
	return &watch{c: newClient(), spadeURLs: map[string]string{}}
}

type watchInternal struct {
	Channel     string
	ChannelID   string
	BroadcastID string
	GameID      string
	Game        string
	UserID      int64
	Token       string
}

func (w *watch) start(ctx context.Context, sess platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	// The minute-watched heartbeat is tracked against the broadcaster +
	// game IDs — propagate them from stream metadata.
	userID, err := w.resolveUserID(ctx, sess)
	if err != nil {
		return platform.WatchHandle{}, fmt.Errorf("resolve user id: %w", err)
	}
	return platform.WatchHandle{
		Channel: stream.Channel,
		Internal: watchInternal{
			Channel:     stream.Channel,
			ChannelID:   stream.ChannelID,
			BroadcastID: stream.BroadcastID,
			GameID:      stream.GameID,
			Game:        stream.Game,
			UserID:      userID,
			Token:       sess.AccessToken,
		},
	}, nil
}

// resolveUserID returns the authenticated user's Twitch numeric id.
// Cached on the watch struct because it doesn't change for the
// lifetime of a session. The Spade minute-watched heartbeat needs this
// number in its properties — without it watch time is silently
// discarded.
func (w *watch) resolveUserID(ctx context.Context, sess platform.Session) (int64, error) {
	w.userIDMu.Lock()
	defer w.userIDMu.Unlock()
	if w.cachedUserID > 0 {
		return w.cachedUserID, nil
	}
	const q = `query CurrentUser { currentUser { id } }`
	var resp struct {
		CurrentUser struct {
			ID string `json:"id"`
		} `json:"currentUser"`
	}
	if err := w.c.gqlQuery(ctx, sess.AccessToken, "CurrentUser", q, nil, &resp); err != nil {
		return 0, err
	}
	if resp.CurrentUser.ID == "" {
		return 0, fmt.Errorf("currentUser.id empty")
	}
	id, err := strconv.ParseInt(resp.CurrentUser.ID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("currentUser.id non-numeric: %q", resp.CurrentUser.ID)
	}
	w.cachedUserID = id
	return id, nil
}

// heartbeat sends one minute-watched event to Twitch's Spade analytics
// beacon. As of the 2026-07-11 Twitch change, the GQL SendEvents mutation
// is no longer credited for drop progress — only this Spade POST path
// accrues watch time. The beacon URL is resolved per channel (cached),
// and on a failed send the cache entry is evicted and re-resolved once
// before giving up, mirroring twitch-gql-rs' send_watch retry.
func (w *watch) heartbeat(ctx context.Context, h platform.WatchHandle) error {
	internal, ok := h.Internal.(watchInternal)
	if !ok {
		return fmt.Errorf("invalid watch handle")
	}

	events, err := minuteWatchedEvents(internal)
	if err != nil {
		return err
	}

	spadeURL, err := w.cachedSpadeURL(ctx, internal.Token, internal.Channel)
	if err != nil {
		return fmt.Errorf("resolve spade url: %w", err)
	}

	if err := w.c.sendSpadeBeacon(ctx, internal.Token, spadeURL, events); err == nil {
		return nil
	}

	// Evict and re-resolve once: the cached URL can go stale (Twitch
	// rotates the analytics edge) and a fresh page fetch is the fix.
	w.evictSpadeURL(internal.Channel)
	spadeURL, err = w.c.resolveSpadeURL(ctx, internal.Token, internal.Channel)
	if err != nil {
		return fmt.Errorf("resolve spade url (retry): %w", err)
	}
	w.setSpadeURL(internal.Channel, spadeURL)
	return w.c.sendSpadeBeacon(ctx, internal.Token, spadeURL, events)
}

func (w *watch) stop(_ context.Context, _ platform.WatchHandle) error {
	return nil
}

// cachedSpadeURL returns the channel's beacon URL from the cache,
// resolving + caching it on first use.
func (w *watch) cachedSpadeURL(ctx context.Context, token, channel string) (string, error) {
	w.spadeMu.Lock()
	if u, ok := w.spadeURLs[channel]; ok {
		w.spadeMu.Unlock()
		return u, nil
	}
	w.spadeMu.Unlock()

	u, err := w.c.resolveSpadeURL(ctx, token, channel)
	if err != nil {
		return "", err
	}
	w.setSpadeURL(channel, u)
	return u, nil
}

func (w *watch) setSpadeURL(channel, spadeURL string) {
	w.spadeMu.Lock()
	// Lazy-init: Backend/BrowserBackend construct *watch via struct
	// literal (&watch{c: c}) without initializing spadeURLs, so the map
	// can be nil here. Writing to a nil map panics, so allocate on first use.
	if w.spadeURLs == nil {
		w.spadeURLs = map[string]string{}
	}
	w.spadeURLs[channel] = spadeURL
	w.spadeMu.Unlock()
}

func (w *watch) evictSpadeURL(channel string) {
	w.spadeMu.Lock()
	delete(w.spadeURLs, channel)
	w.spadeMu.Unlock()
}

// minuteWatchedEvents builds the Spade minute-watched event array for
// the given watch handle and returns its JSON encoding. The properties
// schema matches the post-2026-07-11 Twitch edge: game/game_id/
// is_live/minutes_logged are required (Twitch silently discards
// heartbeats missing them); the legacy location/player fields are gone.
func minuteWatchedEvents(internal watchInternal) ([]byte, error) {
	event := map[string]any{
		"event": "minute-watched",
		"properties": map[string]any{
			"broadcast_id":   internal.BroadcastID,
			"channel_id":     internal.ChannelID,
			"channel":        internal.Channel,
			"client_time":    time.Now().UTC().Format(time.RFC3339),
			"game":           internal.Game,
			"game_id":        internal.GameID,
			"hidden":         false,
			"is_live":        true,
			"live":           true,
			"logged_in":      true,
			"minutes_logged": 1,
			"muted":          false,
			"user_id":        internal.UserID,
		},
	}
	return json.Marshal([]any{event})
}
