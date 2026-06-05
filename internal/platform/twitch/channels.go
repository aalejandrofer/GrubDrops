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
