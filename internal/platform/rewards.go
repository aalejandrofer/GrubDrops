package platform

import (
	"context"
	"errors"
	"time"
)

// RewardCode is one reward an account received outside the drops claim
// flow, such as a game code Twitch hands out with a redeem link. Code is a
// secret: never log it.
type RewardCode struct {
	// Key is the platform's own id for the reward; it dedupes reads.
	Key  string
	Name string
	// Game is the game's name; empty when unknown.
	Game string
	// Code is the redeem code; empty when the reward carries none.
	Code string
	// RedeemURL is where the user redeems the reward; empty when unknown.
	RedeemURL string
	// ExpiresAt is when the reward can no longer be redeemed; zero when unknown.
	ExpiresAt time.Time
	// ReceivedAt is when the platform issued the reward; zero when unknown.
	ReceivedAt time.Time
}

// ErrRewardCodesUnavailable means the session cannot read reward codes,
// for example a token kind the platform refuses for that read. Retrying
// with the same session does not help.
var ErrRewardCodesUnavailable = errors.New("reward codes unavailable for this login")

// RewardCodeReader is a backend that reads the reward codes an account
// received. Reads are side-effect free. known reports, per reward key,
// whether a code and an expiry are already stored, so the reader can skip
// reads it does not need; nil means nothing is stored. A reward listed
// without a code keeps its stored one.
type RewardCodeReader interface {
	RewardCodes(ctx context.Context, s Session, known func(key string) (code, expiry bool)) ([]RewardCode, error)
}
