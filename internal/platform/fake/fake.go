package fake

import (
	"context"
	"sync"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

// Verify that *Backend satisfies platform.Backend at compile time.
var _ platform.Backend = (*Backend)(nil)

type Option func(*Backend)

func WithFastTime() Option {
	return func(b *Backend) { b.fast = true }
}

type Backend struct {
	mu       sync.Mutex
	fast     bool
	progress map[string]int
	claims   map[string]time.Time
	handles  map[string]platform.Stream
}

func New(opts ...Option) *Backend {
	b := &Backend{
		progress: map[string]int{},
		claims:   map[string]time.Time{},
		handles:  map[string]platform.Stream{},
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *Backend) Name() string { return "fake" }

func (b *Backend) StartDeviceLogin(ctx context.Context) (platform.DeviceChallenge, error) {
	return platform.DeviceChallenge{
		UserCode:        "FAKE-CODE",
		VerificationURL: "https://example.invalid/device",
		ExpiresAt:       time.Now().Add(5 * time.Minute),
		Interval:        100 * time.Millisecond,
	}, nil
}

func (b *Backend) PollDeviceLogin(ctx context.Context, ch platform.DeviceChallenge) (platform.Session, error) {
	return platform.Session{
		AccessToken: "fake-access",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}, nil
}

func (b *Backend) LoginViaBrowser(ctx context.Context, rpc platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{AccessToken: "fake-browser", ExpiresAt: time.Now().Add(1 * time.Hour)}, nil
}

func (b *Backend) RefreshSession(ctx context.Context, s platform.Session) (platform.Session, error) {
	s.ExpiresAt = time.Now().Add(1 * time.Hour)
	return s, nil
}

func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	now := time.Now()
	required := 5
	if b.fast {
		required = 2
	}
	return []platform.Campaign{
		{
			ID: "camp_fake_rust_1", Platform: "fake", Game: "Rust",
			Name: "Fake Rust Drops", Status: "active",
			StartsAt: now.Add(-1 * time.Hour), EndsAt: now.Add(24 * time.Hour),
			Benefits: []platform.DropBenefit{
				{ID: "ben_fake_helmet", CampaignID: "camp_fake_rust_1", Name: "Fake Helmet", RequiredMinutes: required, ImageURL: ""},
			},
		},
	}, nil
}

func (b *Backend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	return []platform.Stream{
		{Channel: "fakestreamer", ViewerCount: 9001, DropsEnabled: true},
	}, nil
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []platform.Progress{}
	for benefitID, mins := range b.progress {
		_, claimed := b.claims[benefitID]
		out = append(out, platform.Progress{
			BenefitID: benefitID, MinutesWatched: mins, Claimed: claimed,
		})
	}
	return out, nil
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handles[stream.Channel] = stream
	return platform.WatchHandle{Channel: stream.Channel}, nil
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.progress["ben_fake_helmet"]++
	return nil
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.handles, h.Channel)
	return nil
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, ben platform.DropBenefit) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.claims[ben.ID] = time.Now()
	return nil
}
