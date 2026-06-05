package twitch

import (
	"context"
	"fmt"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

type channels struct {
	c *client
}

// streamLiveData decodes the VideoPlayerStreamInfoOverlayChannel
// response. user.id = channel_id (broadcaster), stream.id = broadcast_id,
// broadcastSettings.game.{id,displayName} = the live category. These IDs
// feed the SendEvents heartbeat — Twitch tracks watch minutes against
// (user_id watching, channel_id broadcasting, game_id, broadcast_id).
type streamLiveData struct {
	User struct {
		ID                string `json:"id"`
		Login             string `json:"login"`
		BroadcastSettings struct {
			Title string `json:"title"`
			Game  *struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"broadcastSettings"`
		Stream *struct {
			ID           string `json:"id"`
			ViewersCount int    `json:"viewersCount"`
			Game         *struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"stream"`
	} `json:"user"`
}

// gameDirectoryData decodes the DirectoryPage_Game response — used
// when a campaign's allow.channels list is empty (i.e. all streams of
// the game qualify). Returns the top live drops-enabled streams.
type gameDirectoryData struct {
	Game struct {
		Streams struct {
			Edges []struct {
				Node struct {
					ID           string `json:"id"`
					ViewersCount int    `json:"viewersCount"`
					Game         *struct {
						ID          string `json:"id"`
						DisplayName string `json:"displayName"`
					} `json:"game"`
					Broadcaster struct {
						ID    string `json:"id"`
						Login string `json:"login"`
					} `json:"broadcaster"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"streams"`
	} `json:"game"`
}

// listForGameDirectory falls back to the DirectoryPage_Game query when
// a campaign's allow.channels is empty. Returns up to 30 live
// drops-enabled streams for the given game slug, fully populated with
// ChannelID + BroadcastID + GameID needed by the SendEvents heartbeat.
func (ch *channels) listForGameDirectory(ctx context.Context, sess platform.Session, gameSlug string) ([]platform.Stream, error) {
	if gameSlug == "" {
		return nil, nil
	}
	vars := map[string]any{
		"limit":              30,
		"slug":               gameSlug,
		"imageWidth":         50,
		"includeCostreaming": false,
		"options": map[string]any{
			"broadcasterLanguages": []string{},
			"freeformTags":         nil,
			"includeRestricted":    []string{"SUB_ONLY_LIVE"},
			"recommendationsContext": map[string]any{
				"platform": "web",
			},
			"sort":          "VIEWER_COUNT",
			"systemFilters": []string{"DROPS_ENABLED"},
			"tags":          []string{},
			"requestID":     "JIRA-VXP-2397",
		},
		"sortTypeIsRecency": false,
	}
	var resp gameDirectoryData
	if err := ch.c.gql(ctx, sess.AccessToken, OpGameDirectory, vars, &resp); err != nil {
		return nil, fmt.Errorf("game directory %s: %w", gameSlug, err)
	}
	out := make([]platform.Stream, 0, len(resp.Game.Streams.Edges))
	for _, e := range resp.Game.Streams.Edges {
		if e.Node.Broadcaster.Login == "" {
			continue
		}
		gameID, gameName := "", ""
		if e.Node.Game != nil {
			gameID, gameName = e.Node.Game.ID, e.Node.Game.DisplayName
		}
		out = append(out, platform.Stream{
			Channel:      e.Node.Broadcaster.Login,
			ViewerCount:  e.Node.ViewersCount,
			DropsEnabled: true,
			ChannelID:    e.Node.Broadcaster.ID,
			BroadcastID:  e.Node.ID,
			GameID:       gameID,
			Game:         gameName,
		})
	}
	return out, nil
}

func (ch *channels) listEligible(ctx context.Context, sess platform.Session, _ platform.Campaign, allowedLogins []string) ([]platform.Stream, error) {
	if len(allowedLogins) == 0 {
		return nil, nil
	}
	out := []platform.Stream{}
	for _, login := range allowedLogins {
		var sd streamLiveData
		err := ch.c.gql(ctx, sess.AccessToken, OpGetStreamInfo,
			map[string]any{"channel": login}, &sd)
		if err != nil {
			return nil, fmt.Errorf("stream live %s: %w", login, err)
		}
		if sd.User.Stream == nil {
			continue
		}
		gameID, gameName := "", ""
		if sd.User.Stream.Game != nil {
			gameID, gameName = sd.User.Stream.Game.ID, sd.User.Stream.Game.DisplayName
		} else if sd.User.BroadcastSettings.Game != nil {
			gameID, gameName = sd.User.BroadcastSettings.Game.ID, sd.User.BroadcastSettings.Game.DisplayName
		}
		out = append(out, platform.Stream{
			Channel:      sd.User.Login,
			ViewerCount:  sd.User.Stream.ViewersCount,
			DropsEnabled: true,
			ChannelID:    sd.User.ID,
			BroadcastID:  sd.User.Stream.ID,
			GameID:       gameID,
			Game:         gameName,
		})
	}
	return out, nil
}
