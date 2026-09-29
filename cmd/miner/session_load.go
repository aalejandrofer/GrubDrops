package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// idleReasonNeedsAuth is the idleReason acquireSession returns when the
// caller should park the account on an idle runner pending re-auth. It's
// surfaced through nopRunner{reason: idleReasonNeedsAuth}; scheduler.idleState
// already defaults a bare/empty reason to "needs_auth" (see
// internal/scheduler/state.go), so this is behaviourally identical to the
// old bare nopRunner{} — just explicit.
const idleReasonNeedsAuth = "needs_auth"

// sessionRetryBackoff is the delay between attempts for both the
// sessions.Get and RefreshSession retry loops in acquireSession. A brief
// DB hiccup or network blip at startup resolves well inside this window;
// tests substitute a zero/near-zero backoff so they run instantly.
var sessionRetryBackoff = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// sessionAttempts is the total number of tries (the first attempt plus
// retries) for both the Get and RefreshSession loops.
const sessionAttempts = 3

// sessionDeps bundles acquireSession's collaborators so tests can substitute
// fakes without touching the real session store, backend, or scheduler
// wiring. get/refresh/put mirror store.SessionStore.Get/Put and
// platform.Backend.RefreshSession's signatures, so callers can pass the
// real methods directly (e.g. sessions.Get, b.RefreshSession, sessions.Put).
type sessionDeps struct {
	get     func(ctx context.Context, accountID string) (platform.Session, bool, error)
	refresh func(ctx context.Context, s platform.Session) (platform.Session, error)
	put     func(ctx context.Context, accountID string, s platform.Session) error
	logger  *slog.Logger
	backoff []time.Duration
	// verify is an optional last-resort check consulted only when every
	// refresh attempt has failed: it probes whether the existing (expired
	// by ExpiresAt stamp) session still works against the live API. Twitch
	// now rejects refreshes for the legacy Android client (#48-adjacent)
	// even when the access token itself is still good, so a refresh
	// rejection alone is not proof the session is dead. nil when the
	// backend doesn't implement platform.AuthChecker (e.g. wired from a
	// non-AuthChecker backend); Kick never reaches this branch at all
	// (see the platform=="kick" short-circuit above).
	verify func(ctx context.Context, s platform.Session) error
}

// acquireSession loads (and refreshes, if needed) an account's session. It
// is the extracted, testable core of build()'s former inline session block
// in main.go; see cmd/miner/main.go's build() for how the three return
// shapes are turned into a scheduler.Entry:
//
//   - sess, "", nil        — usable session; caller proceeds to build the entry.
//   - Session{}, reason, nil — caller should idle the account (nopRunner{reason: reason}).
//   - Session{}, "", err     — the session store itself is unavailable after
//     retries; caller currently surfaces this the same way it always has
//     (logs "account skipped" and idles too).
//
// now is passed in (instead of acquireSession calling time.Now itself) so
// tests can pin the expired/valid boundary deterministically.
func acquireSession(ctx context.Context, deps sessionDeps, a gen.Account, now time.Time) (platform.Session, string, error) {
	var sess platform.Session
	var ok bool
	var err error

	for attempt := 0; attempt < sessionAttempts; attempt++ {
		sess, ok, err = deps.get(ctx, a.ID)
		if err == nil {
			break
		}
		if attempt == sessionAttempts-1 {
			return platform.Session{}, "", err
		}
		if !sleepBackoff(ctx, deps.backoff, attempt) {
			return platform.Session{}, "", ctx.Err()
		}
	}
	if !ok {
		deps.logger.Warn("account has no session, will idle until re-auth",
			"account", a.ID, "platform", a.Platform)
		return platform.Session{}, idleReasonNeedsAuth, nil
	}

	if !sess.ExpiresAt.Before(now) {
		return sess, "", nil
	}

	// Kick's ExpiresAt is a synthetic now+7d stamp set at login (Kick
	// cookies don't carry a real expiry, and RefreshSession is a no-op for
	// Kick) — not a real signal the cookies died. Let the periodic
	// auth-health sweep (internal/authcheck, which calls VerifyAuth over
	// the live API) decide instead of idling a still-working account every
	// week.
	if a.Platform == "kick" {
		return sess, "", nil
	}

	if sess.RefreshToken == "" {
		deps.logger.Warn("session expired and no refresh token, will idle",
			"account", a.ID, "platform", a.Platform)
		return platform.Session{}, idleReasonNeedsAuth, nil
	}

	var refreshed platform.Session
	var refreshErr error
	for attempt := 0; attempt < sessionAttempts; attempt++ {
		refreshed, refreshErr = deps.refresh(ctx, sess)
		if refreshErr == nil {
			break
		}
		if attempt == sessionAttempts-1 {
			// Every refresh attempt failed. That's not automatically proof
			// the session is dead: Twitch rejects refreshes outright for
			// the legacy Android client even while the access token itself
			// still works. If the backend gave us a cheap way to check
			// (deps.verify) and the existing session passes it, keep
			// mining with it instead of idling a perfectly good account.
			if deps.verify != nil {
				if ctx.Err() != nil {
					return platform.Session{}, "", ctx.Err()
				}
				if verifyErr := deps.verify(ctx, sess); verifyErr == nil {
					deps.logger.Warn("session refresh failed but the current token still verifies; continuing with it",
						"account", a.ID, "platform", a.Platform, "err", refreshErr)
					return sess, "", nil
				}
			}
			deps.logger.Warn("session refresh failed, will idle",
				"account", a.ID, "platform", a.Platform, "err", refreshErr)
			return platform.Session{}, idleReasonNeedsAuth, nil
		}
		if !sleepBackoff(ctx, deps.backoff, attempt) {
			return platform.Session{}, "", ctx.Err()
		}
	}

	// The refresh itself succeeded — never discard it over a storage
	// hiccup (that would also strand the rotated refresh token). Retry the
	// persist once, log if it still fails, and continue with the refreshed
	// session either way.
	if err := deps.put(ctx, a.ID, refreshed); err != nil {
		deps.logger.Warn("persist refreshed session failed, retrying once",
			"account", a.ID, "err", err)
		if err := deps.put(ctx, a.ID, refreshed); err != nil {
			deps.logger.Error("persist refreshed session failed twice; continuing with in-memory session",
				"account", a.ID, "err", err)
		}
	}

	deps.logger.Info("session refreshed", "account", a.ID, "platform", a.Platform)
	return refreshed, "", nil
}

// sleepBackoff waits out backoff[attempt] (or returns immediately if attempt
// is beyond the given backoff slice), honoring ctx cancellation. It reports
// false if ctx was cancelled before the wait completed.
func sleepBackoff(ctx context.Context, backoff []time.Duration, attempt int) bool {
	var d time.Duration
	if attempt < len(backoff) {
		d = backoff[attempt]
	}
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
