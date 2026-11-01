package twitch

import (
	"context"
	"errors"
	"fmt"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// channelPointsContext is the part of the ChannelPointsContext response the
// bonus claim and the balance read use.
type channelPointsContext struct {
	Community *struct {
		Channel *channelPointsChannel `json:"channel"`
	} `json:"community"`
}

// claimCommunityPointsResult is the ClaimCommunityPoints mutation payload.
type claimCommunityPointsResult struct {
	ClaimCommunityPoints *struct {
		CurrentPoints *int `json:"currentPoints"`
		Error         *struct {
			Code string `json:"code"`
		} `json:"error"`
	} `json:"claimCommunityPoints"`
}

// ClaimChannelPoints claims the channel-points bonus on channel (a login)
// when one is available. With none available it returns Claimed false and a
// nil error after one ChannelPointsContext query. Satisfies
// platform.PointsClaimer.
func (b *Backend) ClaimChannelPoints(ctx context.Context, s platform.Session, channel string) (platform.PointsClaim, error) {
	b.c.bind(s)
	return claimChannelPoints(ctx, b.c, s.AccessToken, channel)
}

// ChannelPointsBalance reads the account's balance on channel (a login)
// with one ChannelPointsContext query and claims nothing. A channel with no
// balance returns platform.ErrNoPointsBalance. Satisfies
// platform.PointsBalancer.
func (b *Backend) ChannelPointsBalance(ctx context.Context, s platform.Session, channel string) (int, error) {
	b.c.bind(s)
	ch, err := channelPointsState(ctx, b.c, s.AccessToken, channel)
	if err != nil {
		return 0, err
	}
	if ch.Self == nil || ch.Self.CommunityPoints == nil || ch.Self.CommunityPoints.Balance == nil {
		return 0, platform.ErrNoPointsBalance
	}
	return *ch.Self.CommunityPoints.Balance, nil
}

var (
	_ platform.PointsClaimer  = (*Backend)(nil)
	_ platform.PointsBalancer = (*Backend)(nil)
)

// channelPointsChannel is the channel part of a ChannelPointsContext reply.
type channelPointsChannel = struct {
	ID   string `json:"id"`
	Self *struct {
		CommunityPoints *struct {
			Balance        *int `json:"balance"`
			AvailableClaim *struct {
				ID string `json:"id"`
			} `json:"availableClaim"`
		} `json:"communityPoints"`
	} `json:"self"`
}

// channelPointsState runs one ChannelPointsContext query for channel.
func channelPointsState(ctx context.Context, c *client, token, channel string) (*channelPointsChannel, error) {
	if channel == "" || token == "" {
		return nil, errors.New("channel points: channel and access token are required")
	}
	var state channelPointsContext
	if err := c.gql(ctx, token, OpChannelPointsContext, map[string]any{"channelLogin": channel}, &state); err != nil {
		return nil, fmt.Errorf("channel points context: %w", err)
	}
	if state.Community == nil || state.Community.Channel == nil {
		return nil, fmt.Errorf("channel points context: no channel %q", channel)
	}
	return state.Community.Channel, nil
}

func claimChannelPoints(ctx context.Context, c *client, token, channel string) (platform.PointsClaim, error) {
	res := platform.PointsClaim{Balance: -1}
	ch, err := channelPointsState(ctx, c, token, channel)
	if err != nil {
		return res, err
	}
	var before *int
	var claimID string
	if ch.Self != nil && ch.Self.CommunityPoints != nil {
		before = ch.Self.CommunityPoints.Balance
		if a := ch.Self.CommunityPoints.AvailableClaim; a != nil {
			claimID = a.ID
		}
	}
	if before != nil {
		res.Balance = *before
	}
	if claimID == "" {
		return res, nil
	}
	if ch.ID == "" {
		return res, errors.New("channel points context: no channel id")
	}
	var out claimCommunityPointsResult
	if err := c.gql(ctx, token, OpClaimCommunityPoints, map[string]any{
		"input": map[string]string{"channelID": ch.ID, "claimID": claimID},
	}, &out); err != nil {
		return res, fmt.Errorf("claim channel points: %w", err)
	}
	r := out.ClaimCommunityPoints
	if r == nil {
		return res, errors.New("claim channel points: no claim result")
	}
	if r.Error != nil && r.Error.Code != "" {
		return res, fmt.Errorf("claim channel points: refused: %s", r.Error.Code)
	}
	res.Claimed = true
	if r.CurrentPoints != nil {
		res.Balance = *r.CurrentPoints
		if before != nil && *r.CurrentPoints > *before {
			res.Gained = *r.CurrentPoints - *before
		}
	}
	return res, nil
}
