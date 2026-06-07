// Package platformtest exposes a deterministic in-memory Backend for
// scheduler / watcher tests. Replaces the former platform/fake package
// — kept under a `_test`-style namespace so production binaries don't
// pull it in.
package platformtest

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// MockBackend produces one campaign with one drop benefit. The watcher
// progresses watch-minutes on each Heartbeat until it reaches the
// benefit's RequiredMinutes, then claim transitions to StateSleeping.
type MockBackend struct {
	mu        sync.Mutex
	progress  map[string]int  // benefit_id -> minutes watched
	claimed   map[string]bool // benefit_id -> claimed
	heartbeat atomic.Int64
}

// New returns a backend with one campaign / one drop requiring 2 minutes.
func New() *MockBackend {
	return &MockBackend{
		progress: map[string]int{"drop1": 0},
		claimed:  map[string]bool{},
	}
}

var _ platform.Backend = (*MockBackend)(nil)

func (b *MockBackend) Name() string { return "mock" }

func (b *MockBackend) StartDeviceLogin(_ context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{UserCode: "MOCK", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (b *MockBackend) PollDeviceLogin(_ context.Context, _ platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{AccessToken: "mock-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (b *MockBackend) LoginViaBrowser(_ context.Context, _ platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{}, nil
}

func (b *MockBackend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	return s, nil
}

func (b *MockBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "camp1", Game: "Mock", Name: "Mock Campaign",
		Benefits: []platform.DropBenefit{{ID: "drop1", CampaignID: "camp1", Name: "Mock Drop", RequiredMinutes: 2}},
	}}, nil
}

func (b *MockBackend) ListEligibleChannels(_ context.Context, _ platform.Session, _ platform.Campaign) ([]platform.Stream, error) {
	return []platform.Stream{{Channel: "mockstreamer", DropsEnabled: true}}, nil
}

func (b *MockBackend) InventoryProgress(_ context.Context, _ platform.Session) ([]platform.Progress, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]platform.Progress, 0, len(b.progress))
	for id, m := range b.progress {
		out = append(out, platform.Progress{BenefitID: id, MinutesWatched: m, Claimed: b.claimed[id]})
	}
	return out, nil
}

func (b *MockBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	return platform.WatchHandle{Channel: s.Channel}, nil
}

func (b *MockBackend) Heartbeat(_ context.Context, _ platform.WatchHandle) error {
	b.heartbeat.Add(1)
	b.mu.Lock()
	for id := range b.progress {
		b.progress[id]++ // each heartbeat advances 1 simulated minute
	}
	b.mu.Unlock()
	return nil
}

func (b *MockBackend) StopWatch(_ context.Context, _ platform.WatchHandle) error { return nil }

func (b *MockBackend) Claim(_ context.Context, _ platform.Session, drop platform.DropBenefit) error {
	b.mu.Lock()
	b.claimed[drop.ID] = true
	b.mu.Unlock()
	return nil
}

func (b *MockBackend) Heartbeats() int64 { return b.heartbeat.Load() }
