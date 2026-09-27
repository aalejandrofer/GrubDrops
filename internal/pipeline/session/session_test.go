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
	minutes  []int // minutes for d1 per poll; last value repeats
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
