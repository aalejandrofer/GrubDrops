package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// fakeCanary counts RunCanary calls and signals each one on ran.
type fakeCanary struct {
	n   atomic.Int32
	ran chan struct{}
}

func newFakeCanary(e *env) *fakeCanary {
	f := &fakeCanary{ran: make(chan struct{}, 8)}
	e.s.d.RunCanary = func(context.Context) error {
		f.n.Add(1)
		f.ran <- struct{}{}
		return nil
	}
	return f
}

// settle waits until no canary run is in flight.
func settle(t *testing.T, e *env) {
	t.Helper()
	require.Eventually(t, func() bool { return !e.s.canaryRunning.Load() }, time.Second, time.Millisecond)
}

func storeCanary(t *testing.T, e *env, platform, value string) {
	t.Helper()
	require.NoError(t, e.s.d.Q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key: "canary:" + platform, Value: []byte(value),
	}))
}

const (
	canarySkipped = `{"ok":false,"skipped":true,"detail":"","checked_at":"2026-10-25T12:00:00Z"}`
	canaryOK      = `{"ok":true,"skipped":false,"detail":"","checked_at":"2026-10-25T12:00:00Z"}`
)

func TestCanary_ResumeRerunsSkipped(t *testing.T) {
	e, _ := accountsEnv(t)
	f := newFakeCanary(e)
	id := e.createAccount("twitch", "Alpha")
	_, tok := e.session()
	storeCanary(t, e, "twitch", canarySkipped)
	put := func(on bool) {
		t.Helper()
		resp, b := e.do("PUT", "/api/accounts/"+id+"/enabled", tok, map[string]bool{"enabled": on})
		require.Equal(t, 204, resp.StatusCode, string(b))
	}

	put(false)
	settle(t, e)
	assert.EqualValues(t, 0, f.n.Load(), "pause does not run the canary")

	put(true)
	select {
	case <-f.ran:
	case <-time.After(time.Second):
		t.Fatal("resume with a skipped result did not run the canary")
	}
	settle(t, e)
	assert.EqualValues(t, 1, f.n.Load())

	put(true) // already enabled: not a resume
	settle(t, e)
	assert.EqualValues(t, 1, f.n.Load())
}

func TestCanary_ResumeKeepsStoredOK(t *testing.T) {
	e, _ := accountsEnv(t)
	f := newFakeCanary(e)
	id := e.createAccount("twitch", "Alpha")
	_, tok := e.session()
	storeCanary(t, e, "twitch", canaryOK)
	storeCanary(t, e, "kick", canarySkipped) // other platform: ignored

	for _, on := range []bool{false, true} {
		resp, b := e.do("PUT", "/api/accounts/"+id+"/enabled", tok, map[string]bool{"enabled": on})
		require.Equal(t, 204, resp.StatusCode, string(b))
	}
	time.Sleep(50 * time.Millisecond)
	settle(t, e)
	assert.EqualValues(t, 0, f.n.Load())
}

func TestCanary_KickSignInRerunsSkipped(t *testing.T) {
	e, _ := accountsEnv(t)
	e.s.d.Registrar = &fakeRegistrar{verifyOK: true}
	f := newFakeCanary(e)
	id := e.createAccount("kick", "K")
	_, tok := e.session()
	storeCanary(t, e, "kick", canarySkipped)

	resp, b := e.do("POST", "/api/accounts/"+id+"/kick/session", tok, map[string]any{"cookies_txt": kickCookies})
	require.Equal(t, 200, resp.StatusCode, string(b))
	select {
	case <-f.ran:
	case <-time.After(time.Second):
		t.Fatal("kick sign-in with a skipped result did not run the canary")
	}
	settle(t, e)
	assert.EqualValues(t, 1, f.n.Load())
}
