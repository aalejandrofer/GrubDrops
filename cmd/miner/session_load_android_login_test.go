package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Android paste-login sessions have no refresh token; an expired stamp runs
// RefreshSession (a re-validate) instead of idling the account.
func TestAcquireSession_AndroidExpiredNoRefreshTokenRevalidates(t *testing.T) {
	now := time.Now()
	deps := noBackoffDeps()
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return platform.Session{AccessToken: "a", ClientID: twitch.ClientAndroid, ExpiresAt: now.Add(-time.Minute)}, true, nil
	}
	var put platform.Session
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		s.ExpiresAt = now.Add(time.Hour)
		return s, nil
	}
	deps.put = func(ctx context.Context, id string, s platform.Session) error { put = s; return nil }
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	sess, idleReason, err := acquireSession(context.Background(), deps, a, now)

	require.NoError(t, err)
	assert.Empty(t, idleReason)
	assert.Equal(t, "a", sess.AccessToken)
	assert.True(t, put.ExpiresAt.After(now), "new expiry persisted")
}

// A dead Android token (validate 401) idles the account as needs-login.
func TestAcquireSession_AndroidRevalidateFailsIdles(t *testing.T) {
	now := time.Now()
	deps := noBackoffDeps()
	deps.get = func(ctx context.Context, id string) (platform.Session, bool, error) {
		return platform.Session{AccessToken: "a", ClientID: twitch.ClientAndroid, ExpiresAt: now.Add(-time.Minute)}, true, nil
	}
	deps.refresh = func(ctx context.Context, s platform.Session) (platform.Session, error) {
		return platform.Session{}, twitch.ErrTokenInvalid
	}
	deps.verify = func(ctx context.Context, s platform.Session) error { return errors.New("currentUser null") }
	a := gen.Account{ID: "acc-1", Platform: "twitch"}

	_, idleReason, err := acquireSession(context.Background(), deps, a, now)

	require.NoError(t, err)
	assert.Equal(t, idleReasonNeedsAuth, idleReason)
}
