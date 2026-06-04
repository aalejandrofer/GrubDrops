package notify

import "context"

type Event = string

const (
	EventState    Event = "state"
	EventProgress Event = "progress"
	EventClaim    Event = "claim"
	EventError    Event = "error"
	EventAuth     Event = "auth"
)

type Notifier interface {
	Notify(ctx context.Context, event Event, fields map[string]any) error
}
