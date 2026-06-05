package twitch

import (
	"context"
	"fmt"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// advisory wraps the optional pre-watch + post-claim verification gql
// queries (P4 + P6). The watcher consults these as "soft" signals — a
// parse miss or upstream error never blocks mining, it just leaves the
// verification log silent.
type advisory struct {
	c *client
}

// availableDropsData decodes DropsHighlightService_AvailableDrops. The
// persisted query returns currently-active campaigns the given channel
// is serving — the shape mirrors DevilXD's reading of the response:
//
//	data.channel.viewerDropCampaigns[].timeBasedDrops[].id
//
// We don't need all fields; only the drop IDs to verify our target.
type availableDropsData struct {
	Channel struct {
		ViewerDropCampaigns []struct {
			ID             string `json:"id"`
			TimeBasedDrops []struct {
				ID string `json:"id"`
			} `json:"timeBasedDrops"`
		} `json:"viewerDropCampaigns"`
	} `json:"channel"`
}

// availableDropIDs returns the set of drop template IDs the given
// channel is currently serving. Returns nil when the response is
// empty or doesn't include any drops — caller treats nil as "unknown"
// and skips the gate.
func (a *advisory) availableDropIDs(ctx context.Context, sess platform.Session, channelID string) (map[string]struct{}, error) {
	if channelID == "" {
		return nil, nil
	}
	var resp availableDropsData
	if err := a.c.gql(ctx, sess.AccessToken, OpAvailableDrops,
		map[string]any{"channelID": channelID}, &resp); err != nil {
		return nil, fmt.Errorf("available drops %s: %w", channelID, err)
	}
	if len(resp.Channel.ViewerDropCampaigns) == 0 {
		return nil, nil
	}
	out := map[string]struct{}{}
	for _, camp := range resp.Channel.ViewerDropCampaigns {
		for _, td := range camp.TimeBasedDrops {
			if td.ID != "" {
				out[td.ID] = struct{}{}
			}
		}
	}
	return out, nil
}

// currentDropData decodes DropCurrentSessionContext. The query reads
// the user's currently-watched drop session. We use it post-claim to
// confirm Twitch's view of the inventory matches our local claim
// state — drift here means our claim got rejected silently.
type currentDropData struct {
	CurrentUser struct {
		DropCurrentSession *struct {
			DropID                 string `json:"dropID"`
			CurrentMinutesWatched  int    `json:"currentMinutesWatched"`
			RequiredMinutesWatched int    `json:"requiredMinutesWatched"`
			ChannelID              string `json:"channelID"`
		} `json:"dropCurrentSession"`
	} `json:"currentUser"`
}

// currentSession returns the active drop session for the authenticated
// user, or zero-value when nothing is in flight. Nil error + zero
// value means the query returned but no session was active.
func (a *advisory) currentSession(ctx context.Context, sess platform.Session) (platform.CurrentSession, error) {
	var resp currentDropData
	if err := a.c.gql(ctx, sess.AccessToken, OpCurrentDrop, nil, &resp); err != nil {
		return platform.CurrentSession{}, fmt.Errorf("current drop session: %w", err)
	}
	if resp.CurrentUser.DropCurrentSession == nil {
		return platform.CurrentSession{}, nil
	}
	s := resp.CurrentUser.DropCurrentSession
	return platform.CurrentSession{
		DropID:         s.DropID,
		ChannelID:      s.ChannelID,
		CurrentMinute:  s.CurrentMinutesWatched,
		RequiredMinute: s.RequiredMinutesWatched,
	}, nil
}
