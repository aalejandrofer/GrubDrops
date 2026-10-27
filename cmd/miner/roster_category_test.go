package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/api"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// categoryBackend adds CategoryLooker to a backend.
type categoryBackend struct {
	platform.Backend
	lookup func(slug string) (string, bool, error)
}

func (b categoryBackend) LookupCategory(_ context.Context, _ platform.Session, slug string) (string, bool, error) {
	return b.lookup(slug)
}

func TestRoster_LookupCategoryFallsThrough(t *testing.T) {
	e := newRosterEnv(t)
	calls := []string{}
	backends := map[string]platform.Backend{
		"a0": categoryBackend{e.fake, func(string) (string, bool, error) {
			calls = append(calls, "a0")
			return "Wrong", true, nil
		}},
		"a1": categoryBackend{e.fake, func(string) (string, bool, error) {
			calls = append(calls, "a1")
			return "", false, errors.New("boom")
		}},
		"a2": noLookBackend{e.fake},
		"a3": categoryBackend{e.fake, func(slug string) (string, bool, error) {
			calls = append(calls, "a3")
			return "Valorant", slug == "valorant", nil
		}},
	}
	e.ro.backendFor = func(a gen.Account) (platform.Backend, error) { return backends[a.ID], nil }
	for _, id := range []string{"a0", "a1", "a2", "a3"} {
		e.account(t, id, 1, true)
	}
	e.boot(t)
	// a0 has a loaded session but its runner now needs a login: skipped.
	a0, err := e.q.GetAccount(e.ctx, "a0")
	require.NoError(t, err)
	e.ro.pool.ReloadAccount("a0", func() runner.Entry { return e.ro.idle(a0, runner.StateNeedsLogin) })
	e.ro.loaded.set("a0", loadedSession{backend: backends["a0"]})

	name, found, err := e.ro.lookupCategory(e.ctx, "kick", "valorant")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "Valorant", name)
	assert.Equal(t, []string{"a1", "a3"}, calls)

	// Every candidate fails: the last error comes back.
	backends["a3"] = backends["a1"]
	e.ro.builder(gen.Account{ID: "a3"})()
	_, _, err = e.ro.lookupCategory(e.ctx, "kick", "valorant")
	assert.EqualError(t, err, "boom")
}

func TestRoster_LookupCategoryNoAccount(t *testing.T) {
	e := newRosterEnv(t)
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) { return noLookBackend{e.fake}, nil }
	e.account(t, "a1", 1, true)
	e.boot(t)

	_, _, err := e.ro.lookupCategory(e.ctx, "kick", "valorant")
	assert.ErrorIs(t, err, api.ErrNoCategoryAccount)
	_, _, err = e.ro.lookupCategory(e.ctx, "twitch", "valorant")
	assert.ErrorIs(t, err, api.ErrNoCategoryAccount, "no account on that platform")
}
