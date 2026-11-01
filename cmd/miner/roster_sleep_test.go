package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
)

// The runner's SleepUntil reads the saved window in the display zone, live
// after settingsChanged, and an account with skip_sleep never sleeps.
func TestRoster_SleepWindow(t *testing.T) {
	e := newRosterEnv(t)
	e.ro.settings = store.NewSettings(e.q)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	e.ro.zone = func() *time.Location { return tokyo }

	a := e.account(t, "a1", 1, true)
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	require.NotNil(t, cfg.SleepUntil)
	now := time.Now()
	_, in := cfg.SleepUntil(now)
	assert.False(t, in, "off by default")

	// A window around now in Tokyo; wide enough that jitter keeps now inside.
	local := now.In(tokyo)
	m := local.Hour()*60 + local.Minute()
	win := store.SleepWindow{Enabled: true, Start: timeutil.FormatClock(m - 120), End: timeutil.FormatClock(m + 120)}
	require.NoError(t, e.ro.settings.SetSleepWindow(e.ctx, win))
	_, in = cfg.SleepUntil(now)
	assert.False(t, in, "cached until settingsChanged")
	e.ro.settingsChanged()
	wake, in := cfg.SleepUntil(now)
	require.True(t, in)
	want := now.Add(120 * time.Minute)
	assert.WithinDuration(t, want, wake, timeutil.SleepJitter+time.Minute)

	// The same window read in UTC (Tokyo is UTC+9) is not around now.
	e.ro.zone = func() *time.Location { return time.UTC }
	_, in = cfg.SleepUntil(now)
	assert.False(t, in, "the window is read in the display zone")
	e.ro.zone = func() *time.Location { return tokyo }

	b := e.account(t, "a2", 1, true)
	b.SkipSleep = 1
	bcfg := e.ro.runnerConfig(e.ctx, b, e.fake, platform.Session{}, nil)
	if bcfg.SleepUntil != nil {
		_, in = bcfg.SleepUntil(now)
		assert.False(t, in, "skip_sleep ignores the window")
	}

	win.Enabled = false
	require.NoError(t, e.ro.settings.SetSleepWindow(e.ctx, win))
	e.ro.settingsChanged()
	_, in = cfg.SleepUntil(now)
	assert.False(t, in, "disabled")
}

// A settings read that began before a save does not cache the old values
// past that save's settingsChanged: the next read sees the save.
func TestRoster_RunSettingsReadRacingSave(t *testing.T) {
	e := newRosterEnv(t)
	e.ro.settings = store.NewSettings(e.q)
	g := e.ro.runGen.Load()
	old, ok := e.ro.readRunSettings() // a reader misses the cache and reads
	require.True(t, ok)
	require.False(t, old.sleep)

	win := store.SleepWindow{Enabled: true, Start: "01:00", End: "05:00"}
	require.NoError(t, e.ro.settings.SetSleepWindow(e.ctx, win))
	e.ro.settingsChanged()
	e.ro.cacheRunSettings(old, g) // the reader stores after the save
	assert.True(t, e.ro.runSettings().sleep, "the save is seen, not the racing read")
}

// The runner's FollowRaids reads the saved setting, off by default, live
// after settingsChanged.
func TestRoster_FollowRaids(t *testing.T) {
	e := newRosterEnv(t)
	e.ro.settings = store.NewSettings(e.q)
	a := e.account(t, "a1", 1, true)
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	require.NotNil(t, cfg.FollowRaids)
	assert.False(t, cfg.FollowRaids(), "off by default")

	require.NoError(t, e.ro.settings.SetFollowRaids(e.ctx, true))
	e.ro.settingsChanged()
	assert.True(t, cfg.FollowRaids())

	require.NoError(t, e.ro.settings.SetFollowRaids(e.ctx, false))
	e.ro.settingsChanged()
	assert.False(t, cfg.FollowRaids())
}

// sharedCatalogFake records the shared catalog a build sets.
type sharedCatalogFake struct {
	*platformtest.Fake
	shared twitch.SharedCatalog
}

func (f *sharedCatalogFake) SetSharedCatalog(c twitch.SharedCatalog) { f.shared = c }

// A build hands the backend the shared catalog, which serves the Discover
// catalog only while the experimental setting is on, read live.
func TestRoster_SharedTwitchCatalog(t *testing.T) {
	e := newRosterEnv(t)
	e.ro.settings = store.NewSettings(e.q)
	calls := 0
	e.ro.catalog = func(_ context.Context, plat string) ([]platform.Campaign, error) {
		calls++
		assert.Equal(t, "twitch", plat)
		return []platform.Campaign{{ID: "c"}}, nil
	}
	fb := &sharedCatalogFake{Fake: e.fake}
	e.ro.backendFor = func(gen.Account) (platform.Backend, error) { return fb, nil }
	a := e.account(t, "a1", 1, true)
	e.ro.builder(a)()
	require.NotNil(t, fb.shared, "the build sets the shared catalog")

	camps, err := fb.shared(e.ctx)
	require.NoError(t, err)
	assert.Nil(t, camps, "off by default")
	assert.Equal(t, 0, calls, "off never reads the catalog")

	require.NoError(t, e.ro.settings.SetExpShareCatalog(e.ctx, true))
	e.ro.settingsChanged()
	camps, err = fb.shared(e.ctx)
	require.NoError(t, err)
	assert.Len(t, camps, 1)
	assert.Equal(t, 1, calls)

	require.NoError(t, e.ro.settings.SetExpShareCatalog(e.ctx, false))
	e.ro.settingsChanged()
	camps, _ = fb.shared(e.ctx)
	assert.Nil(t, camps)

	e.ro.catalog = nil
	require.NoError(t, e.ro.settings.SetExpShareCatalog(e.ctx, true))
	e.ro.settingsChanged()
	camps, err = e.ro.sharedTwitchCatalog(e.ctx)
	assert.NoError(t, err)
	assert.Nil(t, camps, "no source")
}
