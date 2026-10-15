package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeBackend struct {
	platform.Backend
	mu       sync.Mutex
	startErr error
	beatErr  error
	minutes  []int                 // minutes for d1 per poll; last value repeats
	script   [][]platform.Progress // scripted responses per poll; overrides minutes
	polls    int
	beats    int
	stopped  bool
}

func (f *fakeBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	return platform.WatchHandle{Channel: s.Channel}, f.startErr
}
func (f *fakeBackend) Heartbeat(context.Context, platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beats++
	return f.beatErr
}
func (f *fakeBackend) StopWatch(context.Context, platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}
func (f *fakeBackend) InventoryProgress(context.Context, platform.Session) ([]platform.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.script != nil {
		i := f.polls
		if i >= len(f.script) {
			i = len(f.script) - 1
		}
		f.polls++
		return f.script[i], nil
	}
	i := f.polls
	if i >= len(f.minutes) {
		i = len(f.minutes) - 1
	}
	f.polls++
	return []platform.Progress{{BenefitID: "d1", MinutesWatched: f.minutes[i]}}, nil
}

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

func TestRun_ProgressEachTickAndStopOnCancel(t *testing.T) {
	f := &fakeBackend{minutes: []int{1, 2}}
	ticks := make(chan time.Time)
	out := make(chan Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Serves: []string{"d1"}, Ticks: ticks, StallPolls: 5}, out)
		close(done)
	}()

	e := recv(t, out)
	assert.Equal(t, Progress, e.Kind, "first beat is immediate")
	assert.Equal(t, 1, e.Progress[0].MinutesWatched)
	ticks <- time.Now()
	assert.Equal(t, 2, recv(t, out).Progress[0].MinutesWatched)
	cancel()
	<-done
	assert.True(t, f.stopped)
}

func TestRun_HeartbeatErrorIsStreamDown(t *testing.T) {
	f := &fakeBackend{minutes: []int{1}, beatErr: errors.New("offline")}
	out := make(chan Event, 8)
	Run(context.Background(), Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Ticks: make(chan time.Time)}, out)
	e := recv(t, out)
	assert.Equal(t, StreamDown, e.Kind)
	assert.Equal(t, "ch", e.Channel)
	assert.True(t, f.stopped)
}

func TestRun_StartErrorIsStreamDown(t *testing.T) {
	f := &fakeBackend{startErr: errors.New("sidecar")}
	out := make(chan Event, 8)
	Run(context.Background(), Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Ticks: make(chan time.Time)}, out)
	assert.Equal(t, StreamDown, recv(t, out).Kind)
}

func TestRun_StalledOnceAfterNoGain(t *testing.T) {
	f := &fakeBackend{minutes: []int{5}}
	ticks := make(chan time.Time)
	out := make(chan Event, 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Serves: []string{"d1"}, Ticks: ticks, StallPolls: 2}, out)
	require.Equal(t, Progress, recv(t, out).Kind) // poll 1 sets baseline
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind) // no gain 1
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind) // no gain 2
	assert.Equal(t, Stalled, recv(t, out).Kind)
	ticks <- time.Now()
	assert.Equal(t, Progress, recv(t, out).Kind)
	select {
	case e := <-out:
		t.Fatalf("unexpected second event %v", e.Kind)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRun_ClaimedDropLeavingInventoryIsNotAStall(t *testing.T) {
	// Simulate d1 being claimed and disappearing from inventory while d2 keeps accruing.
	// With summed watermark (buggy), the claim would cause false stall.
	// With per-drop watermark (fixed), d2's gain prevents stall.
	f := &fakeBackend{script: [][]platform.Progress{
		{{BenefitID: "d1", MinutesWatched: 30}, {BenefitID: "d2", MinutesWatched: 1}},
		{{BenefitID: "d1", MinutesWatched: 30}, {BenefitID: "d2", MinutesWatched: 2}},
		{{BenefitID: "d2", MinutesWatched: 3}}, // d1 claimed, absent
		{{BenefitID: "d2", MinutesWatched: 4}},
		{{BenefitID: "d2", MinutesWatched: 5}},
	}}
	ticks := make(chan time.Time)
	out := make(chan Event, 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Serves: []string{"d1", "d2"}, Ticks: ticks, StallPolls: 2}, out)

	// Poll 1: d1=30, d2=1 (total=31), baseline set
	require.Equal(t, Progress, recv(t, out).Kind)
	// Poll 2: d1=30, d2=2 (total=32), gain
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind)
	// Poll 3: d1 absent, d2=3, gain on d2 despite d1 gone
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind)
	// Poll 4: d2=4, continued gain
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind)
	// Poll 5: d2=5, still gaining
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind)

	// No Stalled event should arrive
	select {
	case e := <-out:
		t.Fatalf("unexpected stalled event %v", e.Kind)
	case <-time.After(50 * time.Millisecond):
	}
}
