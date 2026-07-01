package authcheck

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeChecker is a platform.AuthChecker that fails its first failN calls
// then succeeds, counting how many times VerifyAuth was invoked.
type fakeChecker struct {
	failN int
	calls int
}

func (f *fakeChecker) VerifyAuth(ctx context.Context, s platform.Session) error {
	f.calls++
	if f.calls <= f.failN {
		return errors.New("transient blip")
	}
	return nil
}

// A single transient failure must NOT be reported as expired: the checker
// retries once and the second attempt succeeds. This is Abu's bug — a valid
// Kick session was marked needs_auth after one idle-timeout/403 blip.
func TestVerifyWithRetry_TransientBlipRecovers(t *testing.T) {
	c := &Checker{retryDelay: time.Millisecond}
	fc := &fakeChecker{failN: 1}
	if err := c.verifyWithRetry(context.Background(), fc, platform.Session{}); err != nil {
		t.Fatalf("expected nil error after retry, got %v", err)
	}
	if fc.calls != 2 {
		t.Fatalf("expected 2 attempts (1 fail + 1 retry), got %d", fc.calls)
	}
}

// A genuinely expired session fails both the initial attempt and the retry,
// so it is still correctly reported as expired.
func TestVerifyWithRetry_PersistentFailureStillFails(t *testing.T) {
	c := &Checker{retryDelay: time.Millisecond}
	fc := &fakeChecker{failN: 5}
	if err := c.verifyWithRetry(context.Background(), fc, platform.Session{}); err == nil {
		t.Fatal("expected error for persistently-failing session, got nil")
	}
	if fc.calls != 2 {
		t.Fatalf("expected 2 attempts (initial + 1 retry), got %d", fc.calls)
	}
}

// A first-try success does not trigger a wasteful retry.
func TestVerifyWithRetry_SuccessNoRetry(t *testing.T) {
	c := &Checker{retryDelay: time.Millisecond}
	fc := &fakeChecker{failN: 0}
	if err := c.verifyWithRetry(context.Background(), fc, platform.Session{}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if fc.calls != 1 {
		t.Fatalf("expected 1 attempt (no retry on success), got %d", fc.calls)
	}
}
