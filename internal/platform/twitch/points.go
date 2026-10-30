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

func channelPointsBalance(ctx context.Context, c *client, s platform.Session, channel string) (int, error) {
	if channel == "" || s.AccessToken == "" {
		return 0, fmt.Errorf("channel and access token are required")
	}
	c.bind(s)
	var state channelPointsContext
	if err := c.gql(ctx, s.AccessToken, OpChannelPointsContext, map[string]any{"channelLogin": channel}, &state); err != nil {
		return 0, fmt.Errorf("get channel points balance: %w", err)
	}
	balance := state.Community.Channel.Self.CommunityPoints.Balance
	if balance == nil {
		return 0, fmt.Errorf("Twitch returned no channel points balance")
	}
	return *balance, nil
}

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

func joinRaid(ctx context.Context, c *client, s platform.Session, raidID string) error {
	var result any
	if err := c.gql(ctx, s.AccessToken, OpJoinRaid, map[string]any{"input": map[string]string{"raidID": raidID}}, &result); err != nil {
		return fmt.Errorf("join raid: %w", err)
	}
	return nil
}

func (b *BrowserBackend) ChannelPointsBalance(ctx context.Context, s platform.Session, channel string) (int, error) {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return 0, err
	}
	a := b.accountFor(s.AccountID)
	a.c.bind(s)
	return channelPointsBalance(ctx, a.c, s, channel)
}

func (b *BrowserBackend) JoinRaid(ctx context.Context, s platform.Session, raidID string) error {
	if err := b.ensureAuthenticated(ctx, s); err != nil {
		return err
	}
	a := b.accountFor(s.AccountID)
	a.c.bind(s)
	return joinRaid(ctx, a.c, s, raidID)
}
