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
	default:
		return "unknown"
	}
}
