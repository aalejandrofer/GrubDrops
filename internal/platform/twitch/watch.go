package twitch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

const sendEventsMutation = `mutation SendEvents($input: SendSpadeEventsInput!) {
  sendSpadeEvents(input: $input) {
    statusCode
  }
}`

type watch struct {
	c *client
}

func newWatch() *watch {
	return &watch{c: newClient()}
}

type watchInternal struct {
	Channel     string
	ChannelID   string
	BroadcastID string
	UserID      int64
	Token       string
}

func (w *watch) start(_ context.Context, sess platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	return platform.WatchHandle{
		Channel: stream.Channel,
		Internal: watchInternal{
			Channel: stream.Channel,
			Token:   sess.AccessToken,
		},
	}, nil
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
			"game":           "",
			"game_id":        "",
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
