package notify

import (
	"context"
	"errors"
	"fmt"
)

// Target names a notifier inside a Multi.
const (
	TargetDiscord  = "discord"
	TargetTelegram = "telegram"
)

// Named pairs a notifier with its target name.
type Named struct {
	Name     string
	Notifier Notifier
}

// Multi fans one event out to every configured notifier. A target set on the
// context with WithTarget limits delivery to that one notifier.
type Multi struct {
	Targets []Named
}

type targetKey struct{}

// WithTarget limits a Multi send to the notifier with this name.
func WithTarget(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, targetKey{}, name)
}

// TargetFrom returns the target set by WithTarget, or "".
func TargetFrom(ctx context.Context) string {
	s, _ := ctx.Value(targetKey{}).(string)
	return s
}

// ErrNoTarget is returned when the context target matches no notifier.
var ErrNoTarget = errors.New("notify: target not configured")

func (m *Multi) Notify(ctx context.Context, event Event, fields map[string]any) error {
	want := TargetFrom(ctx)
	var errs []error
	sent := false
	for _, t := range m.Targets {
		if want != "" && t.Name != want {
			continue
		}
		sent = true
		if err := t.Notifier.Notify(ctx, event, fields); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Name, err))
		}
	}
	if want != "" && !sent {
		return ErrNoTarget
	}
	return errors.Join(errs...)
}
