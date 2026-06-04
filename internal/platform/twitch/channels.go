package twitch

import (
	"context"
	"fmt"

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
)

type channels struct {
	c *client
}

type streamLiveData struct {
	User struct {
		Login  string `json:"login"`
		Stream *struct {
			ID           string `json:"id"`
			ViewersCount int    `json:"viewersCount"`
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
		out = append(out, platform.Stream{
			Channel:      sd.User.Login,
			ViewerCount:  sd.User.Stream.ViewersCount,
			DropsEnabled: true,
		})
	}
	return out, nil
}
