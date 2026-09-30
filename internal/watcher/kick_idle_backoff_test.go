package watcher

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// kickIdleRampBackend is a minimal platform.Backend double for driving
// Watcher.Run through repeated no-live-channel discovery rounds and then
// exactly one round that finds a live channel. One campaign, one
// 1-minute benefit (so a single Heartbeat completes it), and a
// toggleable "live" flag controlling ListEligibleChannels.
type kickIdleRampBackend struct {
	live atomic.Bool

	mu      sync.Mutex
	minutes int
	claimed bool
}

var _ platform.Backend = (*kickIdleRampBackend)(nil)

func (b *kickIdleRampBackend) Name() string { return "kick-idle-ramp-test" }

func (b *kickIdleRampBackend) StartDeviceLogin(context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{}, nil
}

func (b *kickIdleRampBackend) PollDeviceLogin(context.Context, platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{}, nil
}

func (b *kickIdleRampBackend) LoginViaBrowser(context.Context, platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{}, nil
}

func (b *kickIdleRampBackend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	return s, nil
}

func (b *kickIdleRampBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "camp1", Game: "Mock", Name: "Mock Campaign",
		Benefits: []platform.DropBenefit{{ID: "drop1", CampaignID: "camp1", Name: "Mock Drop", RequiredMinutes: 1}},
	}}, nil
}

func (b *kickIdleRampBackend) ListEligibleChannels(context.Context, platform.Session, platform.Campaign) ([]platform.Stream, error) {
	if b.live.Load() {
		return []platform.Stream{{Channel: "mockstreamer", DropsEnabled: true}}, nil
	}
	return nil, nil
}

func (b *kickIdleRampBackend) InventoryProgress(context.Context, platform.Session) ([]platform.Progress, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return []platform.Progress{{BenefitID: "drop1", MinutesWatched: b.minutes, Claimed: b.claimed}}, nil
}

func (b *kickIdleRampBackend) StartWatch(context.Context, platform.Session, platform.Stream) (platform.WatchHandle, error) {
	return platform.WatchHandle{Channel: "mockstreamer"}, nil
}

func (b *kickIdleRampBackend) Heartbeat(context.Context, platform.WatchHandle) error {
	b.mu.Lock()
	b.minutes++
	b.mu.Unlock()
	return nil
}

func (b *kickIdleRampBackend) StopWatch(context.Context, platform.WatchHandle) error { return nil }

func (b *kickIdleRampBackend) Claim(context.Context, platform.Session, platform.DropBenefit) error {
	b.mu.Lock()
	b.claimed = true
	b.mu.Unlock()
	return nil
}

// stateHit is one "state" notification with the wall-clock time it fired.
type stateHit struct {
	at    time.Time
	state string
}

// stateTimeline records every "state" Notify call with its arrival time,
// so a test can measure the real wall-clock gap between state changes —
// in particular, the gap between consecutive StateSleeping entries, which
// is exactly one idle-sleep wait (plus a few negligible tick-cadence
// steps).
type stateTimeline struct {
	mu     sync.Mutex
	events []stateHit
}

func (r *stateTimeline) Notify(_ context.Context, ev string, fields map[string]any) error {
	if ev != "state" {
		return nil
	}
	s, _ := fields["state"].(string)
	r.mu.Lock()
	r.events = append(r.events, stateHit{at: time.Now(), state: s})
	r.mu.Unlock()
	return nil
}

func (r *stateTimeline) snapshot() []stateHit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]stateHit(nil), r.events...)
}

func (r *stateTimeline) count(state string) int {
	n := 0
	for _, e := range r.snapshot() {
		if e.state == state {
			n++
		}
	}
	return n
}

// sleepingGaps returns the wall-clock deltas between consecutive
// StateSleeping hits, in order.
func sleepingGaps(hits []stateHit) []time.Duration {
	var at []time.Time
	for _, h := range hits {
		if h.state == "sleeping" {
			at = append(at, h.at)
		}
	}
	gaps := make([]time.Duration, 0, len(at)-1)
	for i := 1; i < len(at); i++ {
		gaps = append(gaps, at[i].Sub(at[i-1]))
	}
	return gaps
}

func sleepingCount(hits []stateHit) int {
	n := 0
	for _, h := range hits {
		if h.state == "sleeping" {
			n++
		}
	}
	return n
}

// TestKickIdleWait_RampsWhileOfflineAndResetsOnlyAfterWatching is the
// Run()-level regression test for the critical review finding on batch B
// item 3: idleWait must climb (30s->60s->120s->cap in production; scaled
// down here for test speed) across repeated
// pick_campaign->pick_stream(no live)->sleeping rounds, and must be reset
// ONLY by a round that reaches StateWatching — not by the intermediate
// pick_campaign/pick_stream states every offline round also passes
// through. Revert-proof: restoring the old condition ("reset whenever a
// step returns nil and the state isn't Sleeping/AwaitingConnect") zeroes
// idleWait on every pick_campaign/pick_stream transition, so the ramp
// assertions below (gap 2 clearly bigger than gap 1, etc.) fail.
func TestKickIdleWait_RampsWhileOfflineAndResetsOnlyAfterWatching(t *testing.T) {
	prevSteps := kickIdleWaitSteps
	kickIdleWaitSteps = []time.Duration{50 * time.Millisecond, 120 * time.Millisecond, 220 * time.Millisecond, 350 * time.Millisecond}
	t.Cleanup(func() { kickIdleWaitSteps = prevSteps })

	backend := &kickIdleRampBackend{}
	timeline := &stateTimeline{}

	w := New(Config{
		AccountID:         "acc-kick",
		Platform:          "kick",
		Backend:           backend,
		Session:           platform.Session{AccessToken: "x"},
		Notifier:          timeline,
		TickInterval:      2 * time.Millisecond,
		HeartbeatInterval: 2 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Channel stays offline: let the ramp climb through all 4 steps (5
	// "sleeping" hits give 4 gaps: steps 0->1->2 climbing, then the cap
	// held into gap 4).
	require.Eventually(t, func() bool { return timeline.count("sleeping") >= 5 },
		5*time.Second, time.Millisecond, "watcher should idle-sleep repeatedly while offline")

	preWatch := timeline.snapshot()
	gaps := sleepingGaps(preWatch)
	require.GreaterOrEqual(t, len(gaps), 4, "need at least 4 gaps to see the ramp climb + cap")

	// The regression itself: with the old (reverted) reset condition,
	// EVERY round zeroes idleWait via the pick_campaign/pick_stream nil
	// returns, so every gap collapses to ~the 50ms floor and never climbs.
	assert.Greater(t, gaps[1], gaps[0]*3/2,
		"gap 2 must be clearly bigger than gap 1 -- proves the ramp climbs instead of resetting every round")
	assert.Greater(t, gaps[2], gaps[1]*3/2,
		"gap 3 must be clearly bigger than gap 2")
	// Cap: once at the top step, the wait must hold, not keep doubling.
	assert.Less(t, gaps[3], gaps[2]*2,
		"wait must cap at the top step, not keep growing unbounded")

	lastCappedGap := gaps[len(gaps)-1]
	preWatchSleepingCount := sleepingCount(preWatch)

	// Flip the channel live: the next pick_campaign->pick_stream round
	// must reach StateWatching, mine the 1-minute benefit in a single
	// heartbeat, and claim it.
	backend.live.Store(true)
	require.Eventually(t, func() bool { return timeline.count("watching") >= 1 },
		2*time.Second, time.Millisecond, "watcher should start watching once the channel is live")

	// After claiming, nothing is left to mine (the benefit is claimed
	// forever regardless of live state), so the watcher idles again. Wait
	// for a SECOND fresh sleeping hit past the one still using the
	// pre-reset (capped) wait, so the gap we measure is fully governed by
	// the POST-reset idleWait.
	require.Eventually(t, func() bool { return sleepingCount(timeline.snapshot()) >= preWatchSleepingCount+2 },
		5*time.Second, time.Millisecond, "watcher should idle-sleep again after claiming, twice over")

	allGaps := sleepingGaps(timeline.snapshot())
	postResetGap := allGaps[len(allGaps)-1]

	assert.Less(t, postResetGap, lastCappedGap/2,
		"idle wait must reset to the ramp floor once StateWatching is reached, not resume from the capped value")
	assert.Less(t, postResetGap, 100*time.Millisecond,
		"post-reset gap should be close to the 50ms floor step, not mid-ramp")

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
