package canary_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// fakeProbe records the calls made to it and returns a pre-configured result.
type fakeProbe struct {
	calls   int
	result  canary.Result
}

func (f *fakeProbe) Run(_ context.Context, _ platform.Session, _ string) canary.Result {
	f.calls++
	return f.result
}

// fakeSessionSource is an alias for canary.SessionSource used in tests.
type fakeSessionSource = canary.SessionSource

func openTestDB(t *testing.T) *gen.Queries {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return gen.New(db)
}

// TestRunner_TwitchRuns_KickSkippedWhenNoChannel verifies that:
//   - when Twitch channel is set + session present → probe runs, result saved
//   - when Kick channel is empty → probe NOT run, no kick result saved
func TestRunner_TwitchRuns_KickSkippedWhenNoChannel(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)

	twitchProbe := &fakeProbe{result: canary.Result{OK: true, Detail: "twitch ok"}}
	kickProbe := &fakeProbe{result: canary.Result{OK: true, Detail: "kick ok"}}

	source := fakeSessionSource(func(_ context.Context, plat string) (platform.Session, bool, error) {
		switch plat {
		case "twitch":
			return platform.Session{AccessToken: "tok"}, true, nil
		case "kick":
			return platform.Session{}, true, nil // session exists but channel empty
		}
		return platform.Session{}, false, nil
	})

	r := canary.NewRunner(q, source, twitchProbe, kickProbe, canary.RunnerSettings{
		TwitchChannel: "some_streamer",
		KickChannel:   "", // empty → skip
	})

	require.NoError(t, r.RunOnce(ctx))

	// Twitch probe should have been called once.
	assert.Equal(t, 1, twitchProbe.calls, "twitch probe should run when channel is set")

	// Twitch result should be persisted.
	got, ok, err := canary.LoadResult(ctx, q, "twitch")
	require.NoError(t, err)
	assert.True(t, ok, "twitch result should be stored")
	assert.True(t, got.OK)
	assert.Equal(t, "twitch ok", got.Detail)

	// Kick probe should NOT have been called.
	assert.Equal(t, 0, kickProbe.calls, "kick probe should be skipped when channel is empty")

	// No kick result stored.
	_, kickOK, err := canary.LoadResult(ctx, q, "kick")
	require.NoError(t, err)
	assert.False(t, kickOK, "no kick result should be saved when channel is empty")
}

// TestRunner_SkipsWhenNoSession verifies that a platform with no available
// session is skipped (probe not called, nothing persisted).
func TestRunner_SkipsWhenNoSession(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)

	twitchProbe := &fakeProbe{result: canary.Result{OK: true, Detail: "twitch ok"}}
	kickProbe := &fakeProbe{result: canary.Result{OK: false, Detail: "should not run"}}

	source := fakeSessionSource(func(_ context.Context, plat string) (platform.Session, bool, error) {
		// No session for either platform.
		return platform.Session{}, false, nil
	})

	r := canary.NewRunner(q, source, twitchProbe, kickProbe, canary.RunnerSettings{
		TwitchChannel: "some_streamer",
		KickChannel:   "some_kick_channel",
	})

	require.NoError(t, r.RunOnce(ctx))

	assert.Equal(t, 0, twitchProbe.calls, "twitch probe should be skipped when no session")
	assert.Equal(t, 0, kickProbe.calls, "kick probe should be skipped when no session")

	_, twOK, _ := canary.LoadResult(ctx, q, "twitch")
	assert.False(t, twOK, "no twitch result when no session")

	_, kkOK, _ := canary.LoadResult(ctx, q, "kick")
	assert.False(t, kkOK, "no kick result when no session")
}

// TestRunner_BothRun verifies that when both channels and sessions are present,
// both probes are called and results are persisted.
func TestRunner_BothRun(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)

	twitchProbe := &fakeProbe{result: canary.Result{OK: true, Detail: "twitch healthy"}}
	kickProbe := &fakeProbe{result: canary.Result{OK: false, Detail: "kick unhealthy"}}

	source := fakeSessionSource(func(_ context.Context, plat string) (platform.Session, bool, error) {
		return platform.Session{AccessToken: "tok"}, true, nil
	})

	r := canary.NewRunner(q, source, twitchProbe, kickProbe, canary.RunnerSettings{
		TwitchChannel: "twitch_chan",
		KickChannel:   "kick_chan",
	})

	require.NoError(t, r.RunOnce(ctx))

	assert.Equal(t, 1, twitchProbe.calls)
	assert.Equal(t, 1, kickProbe.calls)

	tw, twOK, err := canary.LoadResult(ctx, q, "twitch")
	require.NoError(t, err)
	assert.True(t, twOK)
	assert.True(t, tw.OK)

	kk, kkOK, err := canary.LoadResult(ctx, q, "kick")
	require.NoError(t, err)
	assert.True(t, kkOK)
	assert.False(t, kk.OK)
	assert.Equal(t, "kick unhealthy", kk.Detail)
}

// TestRunner_Run_ZeroIntervalReturnsImmediately verifies that Run with
// interval=0 returns immediately without running any probe or blocking.
func TestRunner_Run_ZeroIntervalReturnsImmediately(t *testing.T) {
	q := openTestDB(t)
	twitchProbe := &fakeProbe{}
	kickProbe := &fakeProbe{}

	source := fakeSessionSource(func(_ context.Context, _ string) (platform.Session, bool, error) {
		return platform.Session{AccessToken: "tok"}, true, nil
	})

	r := canary.NewRunner(q, source, twitchProbe, kickProbe, canary.RunnerSettings{
		TwitchChannel: "chan",
		KickChannel:   "kkchan",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.Run(ctx, 0)
		close(done)
	}()

	select {
	case <-done:
		// expected: returned immediately
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run with interval=0 should return immediately")
	}
}
