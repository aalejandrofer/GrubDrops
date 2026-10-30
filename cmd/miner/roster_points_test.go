package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

func TestScheduledSleepJitteredBoundaries(t *testing.T) {
	loc := time.FixedZone("local", -7*60*60)
	midnight := time.Date(2026, time.July, 12, 0, 0, 0, 0, loc)
	settings := store.PointsSafety{SleepEnabled: true, SleepStart: 120, SleepEnd: 480}
	start := midnight.Add(2*time.Hour + sleepJitter("acc", midnight, "start"))
	end := midnight.Add(8*time.Hour + sleepJitter("acc", midnight, "end"))
	for _, jitter := range []time.Duration{sleepJitter("acc", midnight, "start"), sleepJitter("acc", midnight, "end")} {
		if jitter < -20*time.Minute || jitter > 20*time.Minute {
			t.Fatalf("jitter out of range: %v", jitter)
		}
	}
	if until, asleep := scheduledSleep(start, "acc", settings, loc); !asleep || !until.Equal(end) {
		t.Fatalf("sleep start boundary: asleep=%t until=%v, want %v", asleep, until, end)
	}
	if _, asleep := scheduledSleep(end, "acc", settings, loc); asleep {
		t.Fatal("sleep end boundary must be awake")
	}
	if sleepJitter("acc", midnight, "start") != sleepJitter("acc", midnight, "start") {
		t.Fatal("daily jitter is not stable")
	}
}
func TestRunnerConfigGatesForceChannelsByPointsSetting(t *testing.T) {
	e := newRosterEnv(t)
	notifier := pointsNotifier{}
	e.ro.notifier = notifier
	e.withForce()
	a := e.account(t, "a1", 1, true)
	e.setForce(t, []store.ForceChannel{{Platform: "kick", Channel: "xqc", Accounts: []string{"a1"}}})
	require.NoError(t, store.SavePointsAccountSettings(e.ctx, e.q, a.ID,
		store.PointsAccountSettings{Enabled: false, Streams: 3}))
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	assert.Equal(t, notifier, cfg.Notifier)
	assert.Empty(t, cfg.ForceChannels(), "disabled points must suppress only this account's force channels")
	require.NoError(t, store.SavePointsAccountSettings(e.ctx, e.q, a.ID,
		store.PointsAccountSettings{Enabled: true, Only: true, Streams: 3}))
	cfg = e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	assert.Equal(t, []string{"xqc"}, cfg.ForceChannels())
	assert.True(t, cfg.PointsOnly)
	assert.Equal(t, 3, cfg.PointsStreams())
}
func TestRunnerSleepUsesLiveConfiguredTimezone(t *testing.T) {
	e := newRosterEnv(t)
	a := e.account(t, "a1", 1, true)
	now := time.Date(2026, time.July, 12, 6, 0, 0, 0, time.UTC)
	cfg := e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	if _, asleep := cfg.SleepUntil(now); !asleep {
		t.Fatal("fallback UTC zone should place 06:00 UTC inside the sleep window")
	}
	loc := time.FixedZone("live", 3*60*60)
	e.ro.zone = func() *time.Location { return loc }
	cfg = e.ro.runnerConfig(e.ctx, a, e.fake, platform.Session{}, nil)
	if _, asleep := cfg.SleepUntil(now); asleep {
		t.Fatal("live UTC+3 zone should place 06:00 UTC outside the sleep window")
	}
}

type pointsNotifier struct{}

func (pointsNotifier) Notify(context.Context, string, map[string]any) error { return nil }
