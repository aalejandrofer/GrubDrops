package watcher

type State int

const (
	StateIdle State = iota
	StatePickCampaign
	StatePickStream
	StateWatching
	StateClaiming
	StateSleeping
	StateAuthRequired
	StatePaused
	// StateAwaitingConnect: the account has a whitelisted, active campaign
	// it would mine, but every such campaign is gated behind an unlinked
	// external account (e.g. Krafton/PUBG). Distinct from Sleeping so the
	// dashboard can prompt the user to connect instead of implying idle.
	StateAwaitingConnect
	// StateForceWatch: the watcher is running a manual "force-watch" task
	// (watch a specific channel for N minutes), bypassing campaign
	// selection. Takes priority over normal mining when a pending task
	// exists for the account.
	StateForceWatch
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StatePickCampaign:
		return "pick_campaign"
	case StatePickStream:
		return "pick_stream"
	case StateWatching:
		return "watching"
	case StateClaiming:
		return "claiming"
	case StateSleeping:
		return "sleeping"
	case StateAuthRequired:
		return "auth_required"
	case StatePaused:
		return "paused"
	case StateAwaitingConnect:
		return "awaiting_connect"
	case StateForceWatch:
		return "force_watch"
	default:
		return "unknown"
	}
}
