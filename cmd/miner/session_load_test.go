package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// noBackoffDeps returns a sessionDeps with zero backoff delays so retry
// tests run instantly. get/refresh/put are nil and must be set by the
// caller before use.
func noBackoffDeps() sessionDeps {
	return sessionDeps{logger: testLogger(), backoff: []time.Duration{0, 0}}
}

func TestAcquireSession_GetRetriesThenSucceeds(t *testing.T) {
	var calls int32
	wantSess := platform.Session{ExpiresAt: time.Now().Add(time.Hour)}
	deps := noBackoffDeps()
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return platform.Session{}, false, errors.New("transient db error")
		}
		return wantSess, true, nil
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.Empty(t, idleReason)
	assert.Equal(t, wantSess.ExpiresAt, sess.ExpiresAt)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls), "expected exactly 3 Get attempts")
}

func TestAcquireSession_GetFailsAllAttemptsReturnsError(t *testing.T) {
	wantErr := errors.New("db down")
	deps := noBackoffDeps()
	var calls int32
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		atomic.AddInt32(&calls, 1)
		return platform.Session{}, false, wantErr
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	_, _, err := acquireSession(context.Background(), deps, a, time.Now())

	require.Error(t, err)
	assert.True(t, errors.Is(err, wantErr))
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls), "expected exactly 3 Get attempts before giving up")
}

func TestAcquireSession_NoSessionIdles(t *testing.T) {
	deps := noBackoffDeps()
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return platform.Session{}, false, nil
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.NotEmpty(t, idleReason, "no session on record should idle")
	assert.Equal(t, platform.Session{}, sess)
}

func TestAcquireSession_KickExpiredWeekAgoReturnedNoRefreshCalled(t *testing.T) {
	deps := noBackoffDeps()
	expired := platform.Session{ExpiresAt: time.Now().Add(-7 * 24 * time.Hour)}
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return expired, true, nil
	}
	refreshCalled := false
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		refreshCalled = true
		return s, nil
	}
	a := gen.Account{ID: "acc-1", Platform: "kick"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.Empty(t, idleReason, "a week-old Kick expiry must not idle the account")
	assert.Equal(t, expired.ExpiresAt, sess.ExpiresAt)
	assert.False(t, refreshCalled, "kick sessions must never be refreshed off ExpiresAt")
}

func TestAcquireSession_TwitchExpiredRefreshRetriesThenSucceeds(t *testing.T) {
	deps := noBackoffDeps()
	expired := platform.Session{ExpiresAt: time.Now().Add(-time.Hour), RefreshToken: "rt"}
	refreshed := platform.Session{ExpiresAt: time.Now().Add(time.Hour), RefreshToken: "rt2"}
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return expired, true, nil
	}
	var refreshCalls int32
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		n := atomic.AddInt32(&refreshCalls, 1)
		if n < 3 {
			return platform.Session{}, errors.New("transient refresh error")
		}
		return refreshed, nil
	}
	var putCalls int32
	deps.put = func(ctx context.Context, id string, s platform.Session) error {
		atomic.AddInt32(&putCalls, 1)
		return nil
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.Empty(t, idleReason)
	assert.Equal(t, refreshed.RefreshToken, sess.RefreshToken)
	assert.Equal(t, int32(3), atomic.LoadInt32(&refreshCalls), "expected exactly 3 refresh attempts")
	assert.Equal(t, int32(1), atomic.LoadInt32(&putCalls))
}

func TestAcquireSession_TwitchExpiredRefreshFailsAllAttemptsIdles(t *testing.T) {
	deps := noBackoffDeps()
	expired := platform.Session{ExpiresAt: time.Now().Add(-time.Hour), RefreshToken: "rt"}
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return expired, true, nil
	}
	var refreshCalls int32
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		atomic.AddInt32(&refreshCalls, 1)
		return platform.Session{}, errors.New("refresh down")
	}
	deps.put = func(ctx context.Context, id string, s platform.Session) error {
		t.Fatal("put should never be called when refresh never succeeds")
		return nil
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.NotEmpty(t, idleReason)
	assert.Equal(t, platform.Session{}, sess)
	assert.Equal(t, int32(3), atomic.LoadInt32(&refreshCalls), "expected exactly 3 refresh attempts before idling")
}

func TestAcquireSession_PutFailsTwiceStillReturnsRefreshedSession(t *testing.T) {
	deps := noBackoffDeps()
	expired := platform.Session{ExpiresAt: time.Now().Add(-time.Hour), RefreshToken: "rt"}
	refreshed := platform.Session{ExpiresAt: time.Now().Add(time.Hour), RefreshToken: "rt2"}
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return expired, true, nil
	}
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		return refreshed, nil
	}
	var putCalls int32
	deps.put = func(ctx context.Context, id string, s platform.Session) error {
		atomic.AddInt32(&putCalls, 1)
		return errors.New("db write failed")
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.Empty(t, idleReason)
	assert.Equal(t, refreshed.RefreshToken, sess.RefreshToken, "refreshed session must survive a failed persist")
	assert.Equal(t, int32(2), atomic.LoadInt32(&putCalls), "expected the initial Put plus exactly one retry")
}

func TestAcquireSession_TwitchExpiredNoRefreshTokenIdles(t *testing.T) {
	deps := noBackoffDeps()
	expired := platform.Session{ExpiresAt: time.Now().Add(-time.Hour)}
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return expired, true, nil
	}
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		t.Fatal("refresh should not be called without a refresh token")
		return platform.Session{}, nil
	}
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, time.Now())

	require.NoError(t, err)
	assert.NotEmpty(t, idleReason)
	assert.Equal(t, platform.Session{}, sess)
}

func TestAcquireSession_CtxCancelledDuringBackoffReturnsPromptly(t *testing.T) {
	deps := sessionDeps{
		logger:  testLogger(),
		backoff: []time.Duration{time.Hour}, // would hang the test if ctx weren't respected
	}
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return platform.Session{}, false, errors.New("db down")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	done := make(chan struct{})
	var err error
	go func() {
		_, _, err = acquireSession(ctx, deps, a, time.Now())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acquireSession did not respect ctx cancellation during backoff")
	}
	require.Error(t, err)
}
