package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// withForce turns force watch on with the uncached roster lookup.
func (e *rosterEnv) withForce() {
	e.ro.force = store.NewForceWatch(e.db, e.q)
	e.ro.forceLookup = e.ro.lookupForce
}

func (e *rosterEnv) setForce(t *testing.T, list []store.ForceChannel) {
	t.Helper()
	require.NoError(t, e.ro.force.Set(e.ctx, list))
}

func (e *rosterEnv) boot(t *testing.T) {
	t.Helper()
	bs, err := e.ro.builders(e.ctx)
	require.NoError(t, err)
	require.NoError(t, e.ro.pool.Reload(e.ctx, bs))
}

func TestRoster_ForceForCachedUntilChanged(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.account(t, "a2", 1, true)
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "amouranth", Accounts: []string{"a1", "a2"}},
	})
	assert.Equal(t, []string{"xqc", "amouranth"}, e.ro.forceFor("a1", "kick"))
	assert.Equal(t, []string{"amouranth"}, e.ro.forceFor("a2", "kick"))
	assert.Empty(t, e.ro.forceFor("a1", "twitch"))

	// A store write alone is not seen until forceChanged.
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a2"}}})
	assert.Equal(t, []string{"xqc", "amouranth"}, e.ro.forceFor("a1", "kick"))
	e.ro.forceChanged()
	assert.Empty(t, e.ro.forceFor("a1", "kick"))
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a2", "kick"))
}

func TestRoster_ForceOffWithoutStore(t *testing.T) {
	e := newRosterEnv(t)
	a := e.account(t, "a1", 1, true)
	assert.Nil(t, e.ro.forceFor("a1", "kick"))
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	assert.Nil(t, cfg.ForceLookup, "no checker: feature off")
	e.ro.forceChanged() // nil-safe
}

func TestRoster_ForceLookupUsesLoadedSession(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ViewerCount: 9}, Live: true}
	e.account(t, "a0", 0, true) // disabled: never lends its session
	a := e.account(t, "a1", 1, true)

	_, _, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
	require.ErrorIs(t, err, errNoForceLookupAccount, "nothing loaded yet")

	e.ro.builder(a)()
	s, live, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
	require.NoError(t, err)
	assert.True(t, live)
	assert.Equal(t, 9, s.ViewerCount)
	_, _, err = e.ro.lookupForce(e.ctx, "twitch", "xqc")
	assert.ErrorIs(t, err, errNoForceLookupAccount, "no twitch account")

	// A build that ends signed out drops the borrowed session.
	_, err = e.db.Exec(`DELETE FROM sessions WHERE account_id = 'a1'`)
	require.NoError(t, err)
	e.ro.builder(a)()
	_, _, err = e.ro.lookupForce(e.ctx, "kick", "xqc")
	assert.ErrorIs(t, err, errNoForceLookupAccount)
}

func TestRoster_NoGamesForceWatches(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.fake.Lookups["xqc"] = platformtest.Lookup{Stream: platform.Stream{Channel: "xqc", ChannelID: "1"}, Live: true}
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})
	e.boot(t)
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateForceWatching && s.Channel == "xqc"
	}, 5*time.Second, 20*time.Millisecond)
}

// A force-list edit rebuilds only a runner that has exited as no_games, so
// it can start force watching. Other runners take the edit live: an idle one
// re-picks, a force-watching one rechecks in place, one mining a drop is left
// alone.
func TestRoster_ForceChangedRebuildsOnlyExited(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		e.account(t, id, 1, true)
		// Own empty lists: these never mine a drop.
		require.NoError(t, e.ro.own.Set(e.ctx, id, true, nil))
	}
	// a5 mines the global list and is watching a drop.
	e.account(t, "a5", 1, true)
	e.chooseRust(t)
	e.rustDrop(10, false)
	e.fake.Lookups["live3"] = platformtest.Lookup{Stream: platform.Stream{Channel: "live3", ChannelID: "3"}, Live: true}
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "off1", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "live3", Accounts: []string{"a3"}},
	})
	builds := map[string]int{}
	var mu sync.Mutex
	e.ro.backendFor = func(a gen.Account) (platform.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		builds[a.ID]++
		return e.fake, nil
	}
	e.boot(t)
	for id, want := range map[string]runner.State{
		"a1": runner.StateSleeping, "a2": runner.StateNoGames,
		"a3": runner.StateForceWatching, "a5": runner.StateWatching,
	} {
		require.Eventually(t, func() bool {
			s, ok := snapshotOf(t, e, id)
			return ok && s.State == want
		}, 5*time.Second, 20*time.Millisecond, id)
	}
	mu.Lock()
	clear(builds)
	mu.Unlock()

	// a1 leaves off1, a2 and a5 join it; live3 moves below off1 and keeps
	// a3, which is force watching. a5 is mining a drop, so it is left alone.
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "off1", Accounts: []string{"a2", "a5"}},
		{Platform: "kick", Channel: "live3", Accounts: []string{"a3"}},
	})
	for range 3 {
		e.ro.forceChanged()
	}
	assert.Len(t, e.ro.forceQueue.wake, 1, "requests fold into one wake")
	mu.Lock()
	assert.Empty(t, builds, "forceChanged returns before any reload")
	mu.Unlock()

	go e.ro.runForceReloader()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return builds["a2"] == 1
	}, 5*time.Second, 20*time.Millisecond)
	// a1 takes the edit live: rescanned, it has nothing left and idles.
	require.Eventually(t, func() bool {
		s, ok := snapshotOf(t, e, "a1")
		return ok && s.State == runner.StateNoGames
	}, 5*time.Second, 20*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, map[string]int{"a2": 1}, builds, "only the exited runner is rebuilt")
	mu.Unlock()
	s, ok := snapshotOf(t, e, "a3")
	require.True(t, ok)
	assert.Equal(t, runner.StateForceWatching, s.State)
	assert.Equal(t, "live3", s.Channel)
	e.fake.Mu.Lock()
	assert.NotContains(t, e.fake.Calls, "Stop:live3", "the force watch is not restarted")
	e.fake.Mu.Unlock()
	s, ok = snapshotOf(t, e, "a5")
	require.True(t, ok)
	assert.Equal(t, runner.StateWatching, s.State)
}

// lookupBackend is a Fake whose LookupChannel is replaced.
type lookupBackend struct {
	*platformtest.Fake
	lookup func(login string) (platform.Stream, bool, error)
}

func (b lookupBackend) LookupChannel(_ context.Context, _ platform.Session, login string) (platform.Stream, bool, error) {
	return b.lookup(login)
}

// noLookBackend hides every optional interface, LookupChannel included.
type noLookBackend struct{ platform.Backend }

func TestRoster_ForceLookupFallsThrough(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	var mu sync.Mutex
	calls := []string{}
	backends := map[string]platform.Backend{
		"a0": lookupBackend{e.fake, func(string) (platform.Stream, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "a0")
			return platform.Stream{}, true, nil
		}},
		"a1": lookupBackend{e.fake, func(string) (platform.Stream, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "a1")
			return platform.Stream{}, false, errors.New("boom")
		}},
		"a2": lookupBackend{e.fake, func(login string) (platform.Stream, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "a2")
			return platform.Stream{Channel: login, ViewerCount: 7}, true, nil
		}},
	}
	e.ro.backendFor = func(a gen.Account) (platform.Backend, error) { return backends[a.ID], nil }
	for _, id := range []string{"a0", "a1", "a2"} {
		e.account(t, id, 1, true)
	}
	e.boot(t)
	// a0 has a loaded session but its runner now needs a login: skipped.
	a0, err := e.q.GetAccount(e.ctx, "a0")
	require.NoError(t, err)
	e.ro.pool.ReloadAccount("a0", func() runner.Entry { return e.ro.idle(a0, runner.StateNeedsLogin) })
	e.ro.loaded.set("a0", loadedSession{backend: backends["a0"]})

	s, live, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
	require.NoError(t, err)
	assert.True(t, live)
	assert.Equal(t, 7, s.ViewerCount, "result from the second candidate")
	mu.Lock()
	assert.Equal(t, []string{"a1", "a2"}, calls)
	mu.Unlock()

	// Every candidate fails: the last error comes back.
	backends["a2"] = backends["a1"]
	e.ro.builder(gen.Account{ID: "a2"})()
	_, _, err = e.ro.lookupForce(e.ctx, "kick", "xqc")
	assert.EqualError(t, err, "boom")
}

func TestRoster_ForceLookupNoLookerWarnsOnce(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	var buf bytes.Buffer
	e.ro.logger = slog.New(slog.NewTextHandler(&buf, nil))
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) { return noLookBackend{e.fake}, nil }
	a := e.account(t, "a1", 1, true)
	e.ro.builder(a)()
	for range 3 {
		_, _, err := e.ro.lookupForce(e.ctx, "kick", "xqc")
		require.ErrorIs(t, err, errNoForceLookupAccount)
	}
	assert.Equal(t, 1, strings.Count(buf.String(), "cannot look up channels"))
}

func TestRoster_ForceChangedWarmsNewChannels(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	a := e.account(t, "a1", 1, true)
	e.ro.builder(a)()
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "old1", Accounts: []string{"a1"}}})
	e.ro.forceList() // cache old1
	e.setForce(t, []store.ForceChannel{
		{Platform: "kick", Channel: "old1", Accounts: []string{"a1"}},
		{Platform: "kick", Channel: "new1"},
	})
	e.ro.forceChanged()
	require.Eventually(t, func() bool {
		e.fake.Mu.Lock()
		defer e.fake.Mu.Unlock()
		return slices.Contains(e.fake.Calls, "Lookup:new1")
	}, 5*time.Second, 20*time.Millisecond)
	e.fake.Mu.Lock()
	assert.NotContains(t, e.fake.Calls, "Lookup:old1", "only new channels are warmed")
	e.fake.Mu.Unlock()
}

func TestRoster_ForceChangedReadFailureRetries(t *testing.T) {
	e := newRosterEnv(t)
	e.withForce()
	e.account(t, "a1", 1, true)
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a1", "kick"))
	_, err := e.db.Exec(`ALTER TABLE force_channels RENAME TO force_channels_gone`)
	require.NoError(t, err)
	e.ro.forceChanged()
	assert.Nil(t, e.ro.forceFor("a1", "kick"), "failed read gives nil")
	_, err = e.db.Exec(`ALTER TABLE force_channels_gone RENAME TO force_channels`)
	require.NoError(t, err)
	assert.Equal(t, []string{"xqc"}, e.ro.forceFor("a1", "kick"), "next read retries")
}
