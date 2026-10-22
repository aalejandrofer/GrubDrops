package watcher

import (
	"context"
	"errors"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/stretchr/testify/require"
)

// forceCandidatesBackend refuses to watch every channel in offline, as a real
// backend does for a channel that is not live.
type forceCandidatesBackend struct {
	*platformtest.MockBackend
	offline map[string]bool
	tried   []string
}

func (b *forceCandidatesBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	b.tried = append(b.tried, s.Channel)
	if b.offline[s.Channel] {
		return platform.WatchHandle{}, errors.New("offline")
	}
	return platform.WatchHandle{Channel: s.Channel}, nil
}

func TestForceWatchSkipsOfflineChannels(t *testing.T) {
	ctx := context.Background()
	backend := &forceCandidatesBackend{MockBackend: platformtest.New(), offline: map[string]bool{"top": true, "second": true}}
	w := New(Config{AccountID: "acct", Backend: backend, Session: platform.Session{AccessToken: "tok"}})
	w.mu.Lock()
	w.forceTask = &ForceTask{Channel: "top", Alternates: []string{"second", "third"}}
	w.mu.Unlock()
	w.setState(ctx, StateForceWatch)

	require.NoError(t, w.forceWatch(ctx))
	require.Equal(t, []string{"top", "second", "third"}, backend.tried)
	require.Equal(t, StateForceWatch, w.State())
	require.Equal(t, "third", w.Snapshot().Channel, "watches the first live channel in priority order")

	// Everything offline: give the account back to mining instead of stalling.
	w2 := New(Config{AccountID: "acct2", Backend: &forceCandidatesBackend{MockBackend: platformtest.New(),
		offline: map[string]bool{"a": true, "b": true}}, Session: platform.Session{AccessToken: "tok"}})
	w2.mu.Lock()
	w2.forceTask = &ForceTask{Channel: "a", Alternates: []string{"b"}}
	w2.mu.Unlock()
	w2.setState(ctx, StateForceWatch)
	require.NoError(t, w2.forceWatch(ctx))
	require.Equal(t, StatePickCampaign, w2.State())
}
