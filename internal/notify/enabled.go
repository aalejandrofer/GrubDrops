package notify

import "context"

// Gate is a notifier that can tell before a send whether an event would
// reach anyone.
type Gate interface {
	Enabled(ctx context.Context, event Event) bool
}

// Enabled reports whether n would deliver event to at least one target. A
// notifier that is not a Gate is taken to deliver; a nil one never does.
func Enabled(ctx context.Context, n Notifier, event Event) bool {
	if n == nil {
		return false
	}
	if g, ok := n.(Gate); ok {
		return g.Enabled(ctx, event)
	}
	return true
}

// allows reports whether the filter lets event through; a nil filter lets
// every event through.
func (f *VerbosityFilter) allows(event Event) bool {
	return f == nil || f.Allow[event]
}

// Enabled is false: the no-op notifier has no target.
func (n *NoopNotifier) Enabled(context.Context, Event) bool { return false }

// Enabled reports whether the filter lets event through.
func (d *DiscordWebhook) Enabled(_ context.Context, event Event) bool {
	return d.Filter.allows(event)
}

// Enabled reports whether the filter lets event through.
func (t *TelegramBot) Enabled(_ context.Context, event Event) bool {
	return t.Filter.allows(event)
}

// Enabled reports whether any target, or the one set with WithTarget,
// would deliver event.
func (m *Multi) Enabled(ctx context.Context, event Event) bool {
	want := TargetFrom(ctx)
	for _, t := range m.Targets {
		if want != "" && t.Name != want {
			continue
		}
		if Enabled(ctx, t.Notifier, event) {
			return true
		}
	}
	return false
}
