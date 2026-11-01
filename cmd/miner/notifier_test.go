package main

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/notify"
)

func TestIndirectNotifierPassesGate(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var cur notify.Notifier = &notify.NoopNotifier{}
	n := &indirectNotifier{mu: &mu, ptr: &cur}
	assert.False(t, notify.Enabled(ctx, n, notify.EventRewardCode), "no target")

	cur = notify.NewDiscordWebhook("http://x", &notify.VerbosityFilter{Allow: map[string]bool{notify.EventRewardCode: true}})
	assert.True(t, notify.Enabled(ctx, n, notify.EventRewardCode), "follows a settings swap")
}
