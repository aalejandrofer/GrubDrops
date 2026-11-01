package notify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnabled(t *testing.T) {
	ctx := context.Background()
	on := &VerbosityFilter{Allow: map[string]bool{EventRewardCode: true}}
	off := &VerbosityFilter{Allow: map[string]bool{EventRewardCode: false}}

	assert.False(t, Enabled(ctx, &NoopNotifier{}, EventRewardCode), "no target")
	assert.False(t, Enabled(ctx, nil, EventRewardCode), "nil notifier")
	assert.True(t, Enabled(ctx, &recNotifier{}, EventRewardCode), "no gate: taken to deliver")

	assert.True(t, Enabled(ctx, NewDiscordWebhook("http://x", on), EventRewardCode))
	assert.False(t, Enabled(ctx, NewDiscordWebhook("http://x", off), EventRewardCode))
	assert.True(t, Enabled(ctx, NewDiscordWebhook("http://x", nil), EventRewardCode), "no filter sends all")
	assert.True(t, Enabled(ctx, NewTelegramBot("t", "c", on), EventRewardCode))
	assert.False(t, Enabled(ctx, NewTelegramBot("t", "c", off), EventRewardCode))

	m := &Multi{Targets: []Named{
		{TargetDiscord, NewDiscordWebhook("http://x", off)},
		{TargetTelegram, NewTelegramBot("t", "c", on)},
	}}
	assert.True(t, Enabled(ctx, m, EventRewardCode), "one target takes it")
	assert.False(t, Enabled(WithTarget(ctx, TargetDiscord), m, EventRewardCode), "the chosen target is off")
	assert.False(t, Enabled(ctx, &Multi{}, EventRewardCode), "no targets")
	assert.False(t, Enabled(ctx, &Multi{Targets: []Named{{TargetDiscord, NewDiscordWebhook("http://x", off)}}}, EventRewardCode))
}
