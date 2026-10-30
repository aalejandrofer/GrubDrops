package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

type balanceBackend struct {
	platform.Backend
	balance   int
	err       error
	accountID string
	calls     chan string
	done      chan struct{}
}

func (*balanceBackend) Name() string { return "twitch" }
func (b *balanceBackend) ChannelPointsBalance(_ context.Context, session platform.Session, channel string) (int, error) {
	b.accountID = session.AccountID
	b.calls <- channel
	defer close(b.done)
	return b.balance, b.err
}

type delayedBalanceRequest struct {
	token   string
	release chan struct{}
	done    chan struct{}
}

type delayedBalanceBackend struct {
	platform.Backend
	calls chan delayedBalanceRequest
}

func (*delayedBalanceBackend) Name() string { return "twitch" }
func (b *delayedBalanceBackend) ChannelPointsBalance(_ context.Context, session platform.Session, _ string) (int, error) {
	request := delayedBalanceRequest{token: session.AccessToken, release: make(chan struct{}), done: make(chan struct{})}
	b.calls <- request
	defer close(request.done)
	<-request.release
	if request.token == "old" {
		return 111, nil
	}
	return 222, nil
}

func TestPointsHandlersDefaultsValidationAndAccountSettings(t *testing.T) {
	e, _ := accountsEnv(t)
	id := e.createAccount("twitch", "T")
	_, token := e.session()

	resp, body := e.do("GET", "/api/points", "", nil)
	require.Equal(t, 200, resp.StatusCode, string(body))
	assert.JSONEq(t, `{"sleep_enabled":true,"sleep_start":"02:00","sleep_end":"08:00","follow_raids":true}`, string(body))
	resp, body = e.do("GET", "/api/accounts", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), `"points_enabled":true,"points_only":false,"points_streams":2`)

	for _, invalid := range []map[string]any{
		{"sleep_enabled": true, "sleep_start": "02:00", "sleep_end": "02:00", "follow_raids": true},
		{"sleep_enabled": true, "sleep_start": "24:00", "sleep_end": "08:00", "follow_raids": true},
		{"sleep_enabled": true, "sleep_start": "2:00", "sleep_end": "08:00", "follow_raids": true},
	} {
		resp, _ = e.do("PUT", "/api/points/safety", token, invalid)
		assert.Equal(t, 400, resp.StatusCode)
	}
	resp, body = e.do("PUT", "/api/points/safety", token, map[string]any{"sleep_enabled": false, "sleep_start": "22:30", "sleep_end": "06:30", "follow_raids": false})
	require.Equal(t, 200, resp.StatusCode, string(body))
	assert.JSONEq(t, `{"sleep_enabled":false,"sleep_start":"22:30","sleep_end":"06:30","follow_raids":false}`, string(body))

	resp, body = e.do("PUT", "/api/accounts/"+id+"/points", token, map[string]any{"points_enabled": false, "points_only": true, "points_streams": 10000})
	require.Equal(t, 204, resp.StatusCode, string(body))
	resp, body = e.do("GET", "/api/accounts", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), `"points_enabled":false,"points_only":true,"points_streams":10000`)
	for _, invalid := range []map[string]any{
		{"points_enabled": true, "points_only": false},
		{"points_enabled": true, "points_only": false, "points_streams": 0},
		{"points_enabled": true, "points_only": false, "points_streams": -1},
	} {
		resp, _ = e.do("PUT", "/api/accounts/"+id+"/points", token, invalid)
		assert.Equal(t, 400, resp.StatusCode)
	}
	kickID := e.createAccount("kick", "K")
	resp, body = e.do("PUT", "/api/accounts/"+kickID+"/points", token, map[string]any{"points_enabled": true, "points_only": false})
	require.Equal(t, 204, resp.StatusCode, string(body))
	resp, body = e.do("GET", "/api/accounts", "", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), `"platform":"kick","name":"K","platform_name":"K","avatar_url":"","state":"idle","needs_login":true,"enabled":true,"own_games":false,"own_game_count":0,"points_enabled":true,"points_only":false,"points_streams":1`)
}

func TestTwitchAccountLimitCountsDisabledButAllowsRelogin(t *testing.T) {
	e, _ := accountsEnv(t, &androidBackend{Fake: platformtest.NewFake()})
	ids := []string{e.createAccount("twitch", "A"), e.createAccount("twitch", "B"), e.createAccount("twitch", "C")}
	_, token := e.session()
	resp, body := e.do("PUT", "/api/accounts/"+ids[0]+"/enabled", token, map[string]bool{"enabled": false})
	require.Equal(t, 204, resp.StatusCode, string(body))

	resp, body = e.do("POST", "/api/accounts", token, map[string]string{"platform": "twitch", "name": "D"})
	assert.Equal(t, 400, resp.StatusCode, string(body))
	assert.Contains(t, string(body), "account_limit")

	resp, body = e.do("POST", "/api/login/twitch/android", token, map[string]string{"url": "other"})
	assert.Equal(t, 400, resp.StatusCode, string(body), "new Android-login accounts obey the cap")

	balance := 123
	e.s.pointsMu.Lock()
	e.s.pointsCache = map[string]pointBalanceEntry{ids[0] + "/channel": {balance: &balance}}
	e.s.pointsMu.Unlock()
	resp, body = e.do("POST", "/api/login/twitch/android", token, map[string]string{"url": "good", "account_id": ids[0]})
	require.Equal(t, 200, resp.StatusCode, string(body), "existing-account relogin remains allowed at the cap")
	assert.Equal(t, ids[0], jsonField(t, body, "id"))
	e.s.pointsMu.Lock()
	assert.NotContains(t, e.s.pointsCache, ids[0]+"/channel")
	e.s.pointsMu.Unlock()
}

func TestPointsBalanceRefreshIsAsyncAndFailureKeepsStaleValue(t *testing.T) {
	backend := &balanceBackend{balance: 321, calls: make(chan string, 2), done: make(chan struct{})}
	e, _ := accountsEnv(t, backend)
	id := e.createAccount("twitch", "T")
	require.NoError(t, e.s.d.Sessions.Put(context.Background(), id, platform.Session{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}))
	e.s.d.Hub = NewHub()
	events, cancel := e.s.d.Hub.Subscribe()
	defer cancel()

	first := e.s.pointsFor("twitch", "channel", []string{id})
	require.Len(t, first, 1)
	assert.Nil(t, first[0].Balance)
	assert.Equal(t, "channel", <-backend.calls)
	select {
	case <-backend.done:
	case <-time.After(time.Second):
		t.Fatal("balance read did not finish")
	}
	assert.Equal(t, id, backend.accountID)
	assert.Equal(t, EventForceWatch, recv(t, events).Name)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := e.s.pointsFor("twitch", "channel", []string{id})
		if got[0].Balance != nil {
			assert.Equal(t, 321, *got[0].Balance)
			break
		}
		time.Sleep(time.Millisecond)
	}
	got := e.s.pointsFor("twitch", "channel", []string{id})
	require.NotNil(t, got[0].Balance)
	assert.Equal(t, 321, *got[0].Balance)
	assert.NotNil(t, got[0].UpdatedAt)

	backend.err = errors.New("provider unavailable")
	oldTime := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	e.s.pointsMu.Lock()
	entry := e.s.pointsCache[id+"/channel"]
	entry.lastAttempt = time.Now().Add(-2 * time.Hour)
	entry.updatedAt = &oldTime
	e.s.pointsCache[id+"/channel"] = entry
	backend.done = make(chan struct{})
	e.s.pointsMu.Unlock()
	stale := e.s.pointsFor("twitch", "channel", []string{id})
	require.NotNil(t, stale[0].Balance)
	assert.Equal(t, 321, *stale[0].Balance)
	select {
	case <-backend.calls:
	case <-time.After(time.Second):
		t.Fatal("stale balance was not refreshed")
	}
	select {
	case <-backend.done:
	case <-time.After(time.Second):
		t.Fatal("failed balance refresh did not finish")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.s.pointsMu.Lock()
		inFlight := e.s.pointsInFlight[id+"/channel"]
		retained := e.s.pointsCache[id+"/channel"].balance
		e.s.pointsMu.Unlock()
		if inFlight == nil {
			require.NotNil(t, retained)
			assert.Equal(t, 321, *retained)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("refresh remained in flight")
}

func TestHomeSkipsPointRefreshWithoutActiveWatchers(t *testing.T) {
	backend := &balanceBackend{calls: make(chan string, 1), done: make(chan struct{})}
	e := newResEnv(t, func(d *Deps) {
		registry := platform.NewRegistry()
		registry.Register(backend)
		d.Registry = registry
		d.ForceLive = func(string, string) *bool {
			live := true
			return &live
		}
	})
	require.NoError(t, e.s.d.ForceWatch.Set(context.Background(), []store.ForceChannel{
		{Platform: "twitch", Channel: "channel", Accounts: []string{"a1"}},
	}))

	force := e.overview()["force_watch"].(map[string]any)
	live := force["live"].([]any)
	require.Len(t, live, 1)
	assert.NotContains(t, live[0].(map[string]any), "points")
	e.s.pointsMu.Lock()
	assert.Empty(t, e.s.pointsCache)
	assert.Empty(t, e.s.pointsInFlight)
	e.s.pointsMu.Unlock()
	select {
	case channel := <-backend.calls:
		t.Fatalf("unexpected balance refresh for %q", channel)
	default:
	}
}

func TestPointsBalanceKickHasNoProjection(t *testing.T) {
	e := newEnv(t)
	assert.Nil(t, e.s.pointsFor("kick", "channel", []string{"a1"}))
}

func TestPointBalanceIdentityChangeFencesOldRead(t *testing.T) {
	backend := &delayedBalanceBackend{calls: make(chan delayedBalanceRequest, 2)}
	e, _ := accountsEnv(t, backend)
	id := e.createAccount("twitch", "T")
	require.NoError(t, e.s.d.Sessions.Put(context.Background(), id, platform.Session{AccessToken: "old", ExpiresAt: time.Now().Add(time.Hour)}))

	const channel = "channel"
	key := id + "/" + channel
	oldFlight := &pointBalanceFlight{}
	e.s.pointsCache = make(map[string]pointBalanceEntry)
	e.s.pointsInFlight = make(map[string]*pointBalanceFlight)
	e.s.pointsMu.Lock()
	e.s.pointsCache[key] = pointBalanceEntry{}
	e.s.pointsInFlight[key] = oldFlight
	e.s.pointsMu.Unlock()
	oldDone := make(chan struct{})
	go func() {
		e.s.refreshPointBalance(key, id, channel, oldFlight)
		close(oldDone)
	}()
	oldCall := <-backend.calls
	require.Equal(t, "old", oldCall.token)

	e.s.forgetPointBalances(id)
	require.NoError(t, e.s.d.Sessions.Put(context.Background(), id, platform.Session{AccessToken: "new", ExpiresAt: time.Now().Add(time.Hour)}))
	e.s.pointsFor("twitch", channel, []string{id})
	newCall := <-backend.calls
	require.Equal(t, "new", newCall.token)

	close(oldCall.release)
	<-oldCall.done
	<-oldDone
	e.s.pointsMu.Lock()
	assert.Nil(t, e.s.pointsCache[key].balance, "old identity must not repopulate an invalidated cache")
	assert.NotNil(t, e.s.pointsInFlight[key], "old completion must not clear the new identity's flight")
	e.s.pointsMu.Unlock()

	close(newCall.release)
	<-newCall.done
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.s.pointsMu.Lock()
		balance := e.s.pointsCache[key].balance
		inFlight := e.s.pointsInFlight[key]
		e.s.pointsMu.Unlock()
		if balance != nil && *balance == 222 && inFlight == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("new identity's balance was not retained")
}
