package notify

import (
	"context"
	"log/slog"
)

type NoopNotifier struct {
	Logger *slog.Logger
}

func (n *NoopNotifier) Notify(_ context.Context, event Event, fields map[string]any) error {
	if n.Logger == nil {
		return nil
	}
	args := []any{"event", event}
	for k, v := range fields {
		args = append(args, k, v)
	}
	n.Logger.Info("notify", args...)
	return nil
}
