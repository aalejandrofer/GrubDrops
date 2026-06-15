package canary_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/notify"
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

// dynamicProbe returns a sequence of results (wraps around when exhausted).
type dynamicProbe struct {
	results []canary.Result
	calls   int
}

func (d *dynamicProbe) Run(_ context.Context, _ platform.Session, _ string) canary.Result {
	r := d.results[d.calls%len(d.results)]
	d.calls++
	return r
}

// spyNotifier records every Notify call.
type spyNotifier struct {
	mu     sync.Mutex
	calls  []spyCall
}

type spyCall struct {
	event  string
	fields map[string]any
}

func (s *spyNotifier) Notify(_ context.Context, event notify.Event, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, spyCall{event: event, fields: fields})
	return nil
}

func (s *spyNotifier) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
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

// alwaysSession is a session source that returns a valid session for all platforms.
func alwaysSession(_ context.Context, _ string) (platform.Session, bool, error) {
	return platform.Session{AccessToken: "tok"}, true, nil
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
	}, nil)

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
	}, nil)

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

	r := canary.NewRunner(q, alwaysSession, twitchProbe, kickProbe, canary.RunnerSettings{
		TwitchChannel: "twitch_chan",
		KickChannel:   "kick_chan",
	}, nil)

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

	r := canary.NewRunner(q, alwaysSession, twitchProbe, kickProbe, canary.RunnerSettings{
		TwitchChannel: "chan",
		KickChannel:   "kkchan",
	}, nil)

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

// ---------------------------------------------------------------------------
// Notification / transition tests
// ---------------------------------------------------------------------------

// TestNotify_FirstFail_Fires verifies that a first-ever fail (no previous
// result) fires the canary notify exactly once.
func TestNotify_FirstFail_Fires(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)
	spy := &spyNotifier{}

	probe := &fakeProbe{result: canary.Result{OK: false, Detail: "beacon timeout"}}
	r := canary.NewRunner(q, alwaysSession, probe, &fakeProbe{}, canary.RunnerSettings{
		TwitchChannel: "chan",
	}, spy)

	require.NoError(t, r.RunOnce(ctx))

	assert.Equal(t, 1, spy.count(), "first fail should fire notify once")
	if spy.count() > 0 {
		call := spy.calls[0]
		assert.Equal(t, notify.EventCanary, call.event, "event must be 'canary'")
		assert.Equal(t, "twitch", call.fields["platform"], "platform field must be set")
		assert.Equal(t, "beacon timeout", call.fields["detail"], "detail field must be set")
	}
}

// TestNotify_FailFail_NoRefire verifies that a second consecutive fail does
// NOT re-fire the notification (fail→fail must be suppressed).
func TestNotify_FailFail_NoRefire(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)
	spy := &spyNotifier{}

	probe := &fakeProbe{result: canary.Result{OK: false, Detail: "still broken"}}
	r := canary.NewRunner(q, alwaysSession, probe, &fakeProbe{}, canary.RunnerSettings{
		TwitchChannel: "chan",
	}, spy)

	// First run → notify fires (entering fail state).
	require.NoError(t, r.RunOnce(ctx))
	assert.Equal(t, 1, spy.count(), "first fail should fire once")

	// Second run → same fail state, notify must NOT fire again.
	require.NoError(t, r.RunOnce(ctx))
	assert.Equal(t, 1, spy.count(), "fail→fail must not re-fire")
}

// TestNotify_OKThenFail_Fires verifies the canonical OK→fail transition:
// first run OK (no notify), second run fail (notify fires once).
func TestNotify_OKThenFail_Fires(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)
	spy := &spyNotifier{}

	probe := &dynamicProbe{
		results: []canary.Result{
			{OK: true, Detail: "healthy"},   // run 1
			{OK: false, Detail: "degraded"}, // run 2
		},
	}
	r := canary.NewRunner(q, alwaysSession, probe, &fakeProbe{}, canary.RunnerSettings{
		TwitchChannel: "chan",
	}, spy)

	// First run: OK → no notification.
	require.NoError(t, r.RunOnce(ctx))
	assert.Equal(t, 0, spy.count(), "OK result must not notify")

	// Second run: fail → one notification.
	require.NoError(t, r.RunOnce(ctx))
	assert.Equal(t, 1, spy.count(), "OK→fail transition must fire once")
}

// TestNotify_NilNotifier_NoPanic verifies that a nil notifier is handled safely
// (fail does not panic, just skips notification).
func TestNotify_NilNotifier_NoPanic(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)

	probe := &fakeProbe{result: canary.Result{OK: false, Detail: "broken"}}
	r := canary.NewRunner(q, alwaysSession, probe, &fakeProbe{}, canary.RunnerSettings{
		TwitchChannel: "chan",
	}, nil) // nil notifier

	assert.NotPanics(t, func() {
		require.NoError(t, r.RunOnce(ctx))
	})
}

// TestNotify_Recovery_NoSpuriousFail verifies that after fail→OK→fail
// the second fail does fire (re-entering fail state), but the OK in between
// does NOT fire a fail notification.
func TestNotify_Recovery_NoSpuriousFail(t *testing.T) {
	ctx := context.Background()
	q := openTestDB(t)
	spy := &spyNotifier{}

	probe := &dynamicProbe{
		results: []canary.Result{
			{OK: false, Detail: "fail 1"}, // run 1: first fail → notify
			{OK: true, Detail: "ok"},      // run 2: recovery → no notify
			{OK: false, Detail: "fail 2"}, // run 3: re-enter fail → notify again
		},
	}
	r := canary.NewRunner(q, alwaysSession, probe, &fakeProbe{}, canary.RunnerSettings{
		TwitchChannel: "chan",
	}, spy)

	require.NoError(t, r.RunOnce(ctx)) // fail → notify (count=1)
	assert.Equal(t, 1, spy.count(), "first fail: 1 notification")

	require.NoError(t, r.RunOnce(ctx)) // ok → no notify (count=1)
	assert.Equal(t, 1, spy.count(), "recovery: no new notification")

	require.NoError(t, r.RunOnce(ctx)) // fail again → notify (count=2)
	assert.Equal(t, 2, spy.count(), "re-enter fail: another notification")
}

// TestNotify_FiresBeforeSave verifies that the Discord alert fires on an
// OK→fail transition even when SaveResult fails (closed DB). This guards the
// reorder fix: notify must run BEFORE persist so a DB error on the transition
// tick never permanently suppresses the alert.
func TestNotify_FiresBeforeSave(t *testing.T) {
	ctx := context.Background()
	spy := &spyNotifier{}

	// Open a real DB, do a first OK run so prev.OK=true is stored, then close
	// the DB so the second run's SaveResult fails.
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	q := gen.New(db)

	probe := &dynamicProbe{
		results: []canary.Result{
			{OK: true, Detail: "ok"},    // run 1: persisted normally
			{OK: false, Detail: "boom"}, // run 2: save will fail (DB closed)
		},
	}
	r := canary.NewRunner(q, alwaysSession, probe, &fakeProbe{}, canary.RunnerSettings{
		TwitchChannel: "chan",
	}, spy)

	// First run: OK — should persist fine and not notify.
	require.NoError(t, r.RunOnce(ctx))
	assert.Equal(t, 0, spy.count(), "OK result must not notify")

	// Close the DB to force SaveResult to fail on the next run.
	require.NoError(t, db.Close())

	// Second run: fail — SaveResult will error (closed DB), but notify must
	// still fire because the alert is sent before the persist attempt.
	_ = r.RunOnce(ctx) // error return is expected/ignored (save failed)
	assert.Equal(t, 1, spy.count(), "notify must fire on OK→fail even when SaveResult fails")

	// Verify the correct event and fields were sent.
	if spy.count() > 0 {
		call := spy.calls[0]
		assert.Equal(t, notify.EventCanary, call.event)
		assert.Equal(t, "twitch", call.fields["platform"])
		assert.Equal(t, "boom", call.fields["detail"])
	}
}

