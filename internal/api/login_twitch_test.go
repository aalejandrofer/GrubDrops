package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// deviceBackend scripts the Twitch device flow on top of platformtest.Fake.
type deviceBackend struct {
	*platformtest.Fake
	challenge platform.DeviceChallenge
	mu        sync.Mutex
	pending   int // polls that answer authorization_pending before success
}

func (b *deviceBackend) Name() string { return "twitch" }

func (b *deviceBackend) StartDeviceLogin(context.Context) (platform.DeviceChallenge, error) {
	return b.challenge, nil
}

func (b *deviceBackend) PollDeviceLogin(context.Context, platform.DeviceChallenge) (platform.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending > 0 {
		b.pending--
		return platform.Session{}, errors.New("authorization_pending")
	}
	return platform.Session{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func waitStatus(t *testing.T, e *env, id, want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, b := e.do("GET", "/api/accounts/"+id+"/twitch/device", "", nil)
		return jsonField(t, b, "status") == want
	}, 3*time.Second, 10*time.Millisecond)
}

func TestTwitchDevice_Flow(t *testing.T) {
	be := &deviceBackend{Fake: platformtest.NewFake(), pending: 2, challenge: platform.DeviceChallenge{
		UserCode: "ABCD-EFGH", VerificationURL: "https://twitch.tv/activate",
		ExpiresAt: time.Now().Add(time.Hour), Interval: 20 * time.Millisecond,
	}}
	e, c := accountsEnv(t, be)
	id := e.createAccount("twitch", "T")
	c.changed = nil

	resp, _ := e.do("GET", "/api/accounts/"+id+"/twitch/device", "", nil)
	assert.Equal(t, 404, resp.StatusCode)

	_, tok := e.session()
	resp, b := e.do("POST", "/api/accounts/"+id+"/twitch/device", tok, nil)
	require.Equal(t, 200, resp.StatusCode, string(b))
	assert.Equal(t, "ABCD-EFGH", jsonField(t, b, "user_code"))
	assert.Equal(t, "https://twitch.tv/activate", jsonField(t, b, "verification_url"))
	assert.NotEmpty(t, jsonField(t, b, "expires_at"))

	_, sb := e.do("GET", "/api/accounts/"+id+"/twitch/device", "", nil)
	assert.Equal(t, "pending", jsonField(t, sb, "status"))

	waitStatus(t, e, id, "done")
	_, ok, err := e.s.d.Sessions.Get(context.Background(), id)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{id}, c.changedList())
}

func TestTwitchDevice_Expired(t *testing.T) {
	be := &deviceBackend{Fake: platformtest.NewFake(), pending: 100, challenge: platform.DeviceChallenge{
		UserCode: "X", ExpiresAt: time.Now().Add(-time.Minute), Interval: 10 * time.Millisecond,
	}}
	e, c := accountsEnv(t, be)
	id := e.createAccount("twitch", "T")
	c.changed = nil
	_, tok := e.session()
	resp, _ := e.do("POST", "/api/accounts/"+id+"/twitch/device", tok, nil)
	require.Equal(t, 200, resp.StatusCode)
	waitStatus(t, e, id, "expired")
	_, ok, err := e.s.d.Sessions.Get(context.Background(), id)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, c.changedList())
}

func TestTwitchDevice_RestartCancelsOld(t *testing.T) {
	be := &deviceBackend{Fake: platformtest.NewFake(), pending: 1 << 20, challenge: platform.DeviceChallenge{
		UserCode: "X", ExpiresAt: time.Now().Add(time.Hour), Interval: 10 * time.Millisecond,
	}}
	e, _ := accountsEnv(t, be)
	id := e.createAccount("twitch", "T")
	_, tok := e.session()
	e.do("POST", "/api/accounts/"+id+"/twitch/device", tok, nil)
	v, _ := e.s.twitch.Load(id)
	old := v.(*twitchFlow)
	e.do("POST", "/api/accounts/"+id+"/twitch/device", tok, nil)
	v2, _ := e.s.twitch.Load(id)
	assert.NotSame(t, old, v2.(*twitchFlow))
	assert.Error(t, old.ctx.Err(), "superseded flow must be cancelled")
	assert.NoError(t, v2.(*twitchFlow).ctx.Err())
	// Cancelling the superseded flow's context means a second cancel is a no-op
	// and the delete below cancels the live one, so no goroutine outlives the test.
	e.do("DELETE", "/api/accounts/"+id, tok, nil)
	_, found := e.s.twitch.Load(id)
	assert.False(t, found)
}
