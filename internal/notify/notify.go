package notify

import "context"

type Event = string

const (
	EventState    Event = "state"
	EventProgress Event = "progress"
	EventClaim    Event = "claim"
	EventError    Event = "error"
	EventAuth     Event = "auth"
	// EventTest is a manual "send test" from the settings page. The
	// verbosity filter always allows it so a test delivers regardless of
	// which real kinds are toggled on.
	EventTest Event = "test"
	// EventCanary fires when the accrual canary transitions to a failed
	// state (OK→fail or first-ever fail). fail→fail does NOT re-fire.
	EventCanary Event = "canary"
)

type Notifier interface {
	Notify(ctx context.Context, event Event, fields map[string]any) error
}
