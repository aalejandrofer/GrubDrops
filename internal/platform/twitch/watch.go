package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

	// playlistMu guards playlistURLs, a per-channel cache of the HLS media
	// playlist URL used by the playlist heartbeat. Resolving it costs a GQL
	// call plus an usher fetch, so it's kept until a send fails or the
	// broadcast changes.
	playlistMu   sync.Mutex
	playlistURLs map[string]playlistEntry
}

type playlistEntry struct {
	broadcastID string
	url         string
}

// newWatch builds the watch leg with its OWN client, constructed exactly
// as before per-session profiles existed (default transport, separate
// cookie jar and device/session identity), so legacy Android sessions keep
// an unchanged request surface: the web cookie jar filled by the channel
// page scrape never leaks onto gql.twitch.tv, and beacons stay off the
// proxy. Only the token -> profile registry is shared with parent, so a
// bind() on the backend's client is visible to the Spade heartbeat.
func newWatch(parent *client) *watch {
	c := newClient()
	c.profiles = parent.profiles
	return &watch{c: c, spadeURLs: map[string]string{}}
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

// heartbeat simulates one tick of watching. Two paths run every tick:
//
//  1. Playlist (primary): GET the channel's current HLS media playlist and
//     HEAD every chunk in it, the method DevilXD/TwitchDropsMiner switched
//     to in f693b0c after the Spade beacon stopped crediting reliably.
//  2. Spade (secondary): POST the minute-watched beacon, kept so nothing is
//     lost if Spade still credits some campaigns.
//
// Each path evicts its cached URL on failure and re-resolves once. The
// heartbeat fails only when both paths fail; a single failing path is
// logged and ignored.
func (w *watch) heartbeat(ctx context.Context, h platform.WatchHandle) error {
	internal, ok := h.Internal.(watchInternal)
	if !ok {
		return fmt.Errorf("invalid watch handle")
	}

	playErr := w.playlistHeartbeat(ctx, internal)
	spadeErr := w.spadeHeartbeat(ctx, internal)

	switch {
	case playErr == nil && spadeErr == nil:
		return nil
	case playErr == nil:
		slog.Warn("twitch spade beacon failed (playlist watch ok)", "channel", internal.Channel, "err", spadeErr)
		return nil
	case spadeErr == nil:
		slog.Warn("twitch playlist watch failed (spade beacon ok)", "channel", internal.Channel, "err", playErr)
		return nil
	default:
		return errors.Join(fmt.Errorf("playlist: %w", playErr), fmt.Errorf("spade: %w", spadeErr))
	}
}

// playlistHeartbeat HEADs every chunk of the channel's media playlist. On
// failure the cached playlist URL is evicted and re-resolved once: the
// variant URL is signed and expires, and an ended broadcast 404s.
func (w *watch) playlistHeartbeat(ctx context.Context, internal watchInternal) error {
	playlistURL, err := w.cachedPlaylistURL(ctx, internal)
	if err != nil {
		return fmt.Errorf("resolve playlist url: %w", err)
	}
	if err := w.c.sendPlaylistWatch(ctx, internal.Token, playlistURL); err == nil {
		return nil
	}
	w.evictPlaylistURL(internal.Channel)
	playlistURL, err = w.c.resolvePlaylistURL(ctx, internal.Token, internal.Channel)
	if err != nil {
		return fmt.Errorf("resolve playlist url (retry): %w", err)
	}
	w.setPlaylistURL(internal.Channel, internal.BroadcastID, playlistURL)
	return w.c.sendPlaylistWatch(ctx, internal.Token, playlistURL)
}

// cachedPlaylistURL returns the channel's media playlist URL from the
// cache, resolving + caching it on first use or when the broadcast changed
// (a new broadcast has a new playlist).
func (w *watch) cachedPlaylistURL(ctx context.Context, internal watchInternal) (string, error) {
	w.playlistMu.Lock()
	if e, ok := w.playlistURLs[internal.Channel]; ok && e.broadcastID == internal.BroadcastID {
		w.playlistMu.Unlock()
		return e.url, nil
	}
	w.playlistMu.Unlock()

	u, err := w.c.resolvePlaylistURL(ctx, internal.Token, internal.Channel)
	if err != nil {
		return "", err
	}
	w.setPlaylistURL(internal.Channel, internal.BroadcastID, u)
	return u, nil
}

func (w *watch) setPlaylistURL(channel, broadcastID, playlistURL string) {
	w.playlistMu.Lock()
	// Lazy-init for the same reason as setSpadeURL.
	if w.playlistURLs == nil {
		w.playlistURLs = map[string]playlistEntry{}
	}
	w.playlistURLs[channel] = playlistEntry{broadcastID: broadcastID, url: playlistURL}
	w.playlistMu.Unlock()
}

func (w *watch) evictPlaylistURL(channel string) {
	w.playlistMu.Lock()
	delete(w.playlistURLs, channel)
	w.playlistMu.Unlock()
}

// spadeHeartbeat sends one minute-watched event to Twitch's Spade
// analytics beacon. The beacon URL is resolved per channel (cached), and
// on a failed send the cache entry is evicted and re-resolved once before
// giving up, mirroring twitch-gql-rs' send_watch retry.
func (w *watch) spadeHeartbeat(ctx context.Context, internal watchInternal) error {
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
