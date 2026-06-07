package twitch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

const sendEventsMutation = `mutation SendEvents($input: SendSpadeEventsInput!) {
  sendSpadeEvents(input: $input) {
    statusCode
  }
}`

type watch struct {
	c *client

	userIDMu     sync.Mutex
	cachedUserID int64
}

func newWatch() *watch {
	return &watch{c: newClient()}
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
	// SendEvents tracks watch time against the broadcaster + game IDs
	// — propagate from stream metadata.
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
// lifetime of a session. SendEvents needs this number for every
// heartbeat — without it watch time is silently discarded.
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

func (w *watch) heartbeat(ctx context.Context, h platform.WatchHandle) error {
	internal, ok := h.Internal.(watchInternal)
	if !ok {
		return fmt.Errorf("invalid watch handle")
	}

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
	plain, err := json.Marshal([]any{event})
	if err != nil {
		return err
	}

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	if _, err := gw.Write(plain); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(gz.Bytes())

	variables := map[string]any{
		"input": map[string]any{
			"data":       encoded,
			"repository": "twilight",
			"encoding":   "GZIP_B64",
		},
	}

	var resp struct {
		SendSpadeEvents struct {
			StatusCode int `json:"statusCode"`
		} `json:"sendSpadeEvents"`
	}
	return w.c.gqlQuery(ctx, internal.Token, "SendEvents", sendEventsMutation, variables, &resp)
}

func (w *watch) stop(_ context.Context, _ platform.WatchHandle) error {
	return nil
}
