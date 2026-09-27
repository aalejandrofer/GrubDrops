package platform

import "context"

// DropProgress is one drop's per-account state as the platform reports it.
// Pipeline v2's reconciler folds these into drop_state.
type DropProgress struct {
	DropID     string
	CampaignID string
	Minutes    int
	Required   int
	Claimed    bool
	InstanceID string
	// Known is true when the platform returned a definite record for this
	// drop. False means the platform said nothing about it (Kick lists a
	// reward only after it accrues), which is not evidence of anything.
	Known bool
	// Unmineable marks drops whose progress the platform cannot track,
	// such as Twitch scrape placeholders. The reconciler blocks them.
	Unmineable bool
}

type ClaimOutcome int

const (
	ClaimOK ClaimOutcome = iota
	ClaimAlready
	ClaimNeedsLink
	ClaimFailed
)

func (o ClaimOutcome) String() string {
	switch o {
	case ClaimOK:
		return "ok"
	case ClaimAlready:
		return "already_claimed"
	case ClaimNeedsLink:
		return "needs_link"
	default:
		return "failed"
	}
}

// ClaimResult is a typed claim outcome, replacing string-matched errors.
type ClaimResult struct {
	Outcome ClaimOutcome
	LinkURL string
	Detail  string
}

// DropProgressSource reports authoritative per-drop progress for the given
// campaigns. It returns an error rather than a partial result, so a failed
// sync never looks like "the platform forgot these drops".
type DropProgressSource interface {
	DropProgress(ctx context.Context, s Session, camps []Campaign) ([]DropProgress, error)
}

// DropClaimer claims one drop and classifies the outcome.
type DropClaimer interface {
	ClaimDrop(ctx context.Context, s Session, d DropProgress) ClaimResult
}

// ChannelProber checks specific channels for liveness and, when c.Game is
// set, that they stream the campaign's game. Used for priority streamers
// that platform directories may not list.
type ChannelProber interface {
	ProbeChannels(ctx context.Context, s Session, c Campaign, logins []string) ([]Stream, error)
}
