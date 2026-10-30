package twitch

import (
	"context"
	"fmt"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type claimCommunityPointsResult struct {
	ClaimCommunityPoints *struct{} `json:"claimCommunityPoints"`
}

type channelPointsContext struct {
	Community struct {
		Channel struct {
			ID   string `json:"id"`
			Self struct {
				CommunityPoints struct {
					Balance        *int `json:"balance"`
					AvailableClaim *struct {
						ID string `json:"id"`
					} `json:"availableClaim"`
				} `json:"communityPoints"`
			} `json:"self"`
		} `json:"channel"`
	} `json:"community"`
}

// ChannelPointsBalance reads the viewer's channel-specific balance without
// claiming any available bonus.
func (b *Backend) ChannelPointsBalance(ctx context.Context, s platform.Session, channel string) (int, error) {
	if channel == "" || s.AccessToken == "" {
		return 0, fmt.Errorf("channel and access token are required")
	}
	b.c.bind(s)
	var state channelPointsContext
	if err := b.c.gql(ctx, s.AccessToken, OpChannelPointsContext, map[string]any{"channelLogin": channel}, &state); err != nil {
		return 0, fmt.Errorf("get channel points balance: %w", err)
	}
	balance := state.Community.Channel.Self.CommunityPoints.Balance
	if balance == nil {
		return 0, fmt.Errorf("Twitch returned no channel points balance")
	}
	return *balance, nil
}

var _ platform.ChannelPointsBalanceReader = (*Backend)(nil)

// claimAvailableCommunityPoints claims the Twitch channel bonus currently
// available to the authenticated viewer for channel. It returns the channel
// ID even when no bonus is available so force-watch can subscribe to raids.
func (w *watch) claimAvailableCommunityPoints(ctx context.Context, token, channel string) (string, bool, error) {
	if channel == "" || token == "" {
		return "", false, nil
	}
	var state channelPointsContext
	if err := w.c.gql(ctx, token, OpChannelPointsContext, map[string]any{"channelLogin": channel}, &state); err != nil {
		return "", false, fmt.Errorf("get channel points context: %w", err)
	}
	channelID := state.Community.Channel.ID
	claim := state.Community.Channel.Self.CommunityPoints.AvailableClaim
	if claim == nil || claim.ID == "" || channelID == "" {
		return channelID, false, nil
	}
	var result claimCommunityPointsResult
	if err := w.c.gql(ctx, token, OpClaimCommunityPoints, map[string]any{
		"input": map[string]string{"claimID": claim.ID, "channelID": channelID},
	}, &result); err != nil {
		return channelID, false, fmt.Errorf("claim channel points bonus: %w", err)
	}
	if result.ClaimCommunityPoints == nil {
		return channelID, false, fmt.Errorf("claim channel points bonus: Twitch returned no claim result")
	}
	return channelID, true, nil
}
