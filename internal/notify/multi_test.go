package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recNotifier struct {
	events []Event
	err    error
}

func (r *recNotifier) Notify(_ context.Context, e Event, _ map[string]any) error {
	r.events = append(r.events, e)
	return r.err
}

func TestMulti_FansOutToAll(t *testing.T) {
	d, tg := &recNotifier{}, &recNotifier{}
	m := &Multi{Targets: []Named{{TargetDiscord, d}, {TargetTelegram, tg}}}
	ctx := context.Background()
	for _, e := range []Event{EventClaim, EventSummary, EventCanary} {
		require.NoError(t, m.Notify(ctx, e, nil))
	}
	want := []Event{EventClaim, EventSummary, EventCanary}
	assert.Equal(t, want, d.events)
	assert.Equal(t, want, tg.events)
}

func TestMulti_OneFailureStillDeliversOthers(t *testing.T) {
	d, tg := &recNotifier{err: errors.New("boom")}, &recNotifier{}
	m := &Multi{Targets: []Named{{TargetDiscord, d}, {TargetTelegram, tg}}}
	err := m.Notify(context.Background(), EventClaim, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discord: boom")
	assert.Len(t, tg.events, 1)
}

func TestMulti_Target(t *testing.T) {
	d, tg := &recNotifier{}, &recNotifier{}
	m := &Multi{Targets: []Named{{TargetDiscord, d}, {TargetTelegram, tg}}}
	require.NoError(t, m.Notify(WithTarget(context.Background(), TargetTelegram), EventTest, nil))
	assert.Empty(t, d.events)
	assert.Len(t, tg.events, 1)

	only := &Multi{Targets: []Named{{TargetDiscord, d}}}
	assert.ErrorIs(t, only.Notify(WithTarget(context.Background(), TargetTelegram), EventTest, nil), ErrNoTarget)
}
