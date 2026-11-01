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
	// EventSummary is the daily summary sent at local midnight. The fields
	// carry day, minutes, claims plus a localized title and msg.
	EventSummary Event = "summary"
	// EventRewardCode is a reward code an account received, or a reminder
	// that one expires soon. The fields carry title, reward, reward_game,
	// account_label, code, redeem_url and expires. The code is a secret:
	// notifiers send it but never log it.
	EventRewardCode Event = "reward_code"
)

// SecretFields are field names whose values must never be written to logs.
var SecretFields = map[string]bool{"code": true}

// IsAlert reports whether an event is a problem the user should act on:
// auth trouble, a failure, or a failed health check. Only alerts mention the
// configured user; claims, progress and info stay silent.
func IsAlert(event Event) bool {
	switch event {
	case EventError, EventAuth, EventCanary:
		return true
	}
	return false
}

// ValidUserID reports whether id is a numeric chat user ID: a Discord
// snowflake or a Telegram user ID.
func ValidUserID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type Notifier interface {
	Notify(ctx context.Context, event Event, fields map[string]any) error
}
