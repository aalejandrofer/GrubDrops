package discovery

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// stubBackend records the session it was handed so tests can assert
// that the GameFilter closure was attached. Only ListActiveCampaigns is
// exercised by the Scrapers; the rest of platform.Backend can panic
// without affecting these tests.
type stubBackend struct {
	mu         sync.Mutex
	gotSession platform.Session
	result     []platform.Campaign
	err        error
	called     int
}

func (b *stubBackend) Name() string { return "stub" }

func (b *stubBackend) StartDeviceLogin(context.Context) (platform.DeviceChallenge, error) {
	panic("not used")
}

func (b *stubBackend) PollDeviceLogin(context.Context, platform.DeviceChallenge) (platform.Session, error) {
	panic("not used")
}

func (b *stubBackend) LoginViaBrowser(context.Context, platform.BrowserRPC) (platform.Session, error) {
	panic("not used")
}

func (b *stubBackend) RefreshSession(_ context.Context, s platform.Session) (platform.Session, error) {
	return s, nil
}

func (b *stubBackend) ListActiveCampaigns(_ context.Context, s platform.Session) ([]platform.Campaign, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gotSession = s
	b.called++
	return b.result, b.err
}

func (b *stubBackend) ListEligibleChannels(context.Context, platform.Session, platform.Campaign) ([]platform.Stream, error) {
	return nil, nil
}

func (b *stubBackend) InventoryProgress(context.Context, platform.Session) ([]platform.Progress, error) {
	return nil, nil
}

func (b *stubBackend) StartWatch(context.Context, platform.Session, platform.Stream) (platform.WatchHandle, error) {
	return platform.WatchHandle{}, nil
}

func (b *stubBackend) Heartbeat(context.Context, platform.WatchHandle) error { return nil }
func (b *stubBackend) StopWatch(context.Context, platform.WatchHandle) error { return nil }
func (b *stubBackend) Claim(context.Context, platform.Session, platform.DropBenefit) error {
	return nil
}

// When the session source reports no Twitch account is loginable, the
// Scraper must return (nil, nil) — the Scraper translates that into a
// log + continue, not an error.
func TestTwitchScraper_NoSessionIsNoOp(t *testing.T) {
	b := &stubBackend{}
	source := func(context.Context) (string, platform.Session, bool, error) {
		return "", platform.Session{}, false, nil
	}
	s := NewTwitchScraper(b, source)
	camps, err := s.Scrape(context.Background(), []string{"rust"})
	require.NoError(t, err)
	assert.Nil(t, camps)
	assert.Equal(t, 0, b.called, "backend must not be called when no session is available")
}

// Source error propagates so the Scraper can log it (but not crash).
func TestTwitchScraper_SourceErrorPropagates(t *testing.T) {
	b := &stubBackend{}
	wantErr := errors.New("session store offline")
	source := func(context.Context) (string, platform.Session, bool, error) {
		return "", platform.Session{}, false, wantErr
	}
	s := NewTwitchScraper(b, source)
	_, err := s.Scrape(context.Background(), []string{"rust"})
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// Happy path: session loaded → GameFilter wired → backend called →
// every campaign passed through. The GameFilter is now a fetch-details
// gate inside the backend, not a top-level drop, so non-whitelisted
// campaigns appear with empty Benefits for the /drops Discoverable tab.
func TestTwitchScraper_AttachesGameFilterAndFilters(t *testing.T) {
	b := &stubBackend{result: []platform.Campaign{
		{ID: "c-rust", Platform: "twitch", Game: "Rust"},
		{ID: "c-apex", Platform: "twitch", Game: "Apex"}, // non-WL: shell row reaches persister
	}}
	source := func(context.Context) (string, platform.Session, bool, error) {
		return "acc-1", platform.Session{AccessToken: "tok"}, true, nil
	}
	s := NewTwitchScraper(b, source)
	camps, err := s.Scrape(context.Background(), []string{"rust"})
	require.NoError(t, err)

	require.Len(t, camps, 2, "both whitelisted and non-whitelisted campaigns emitted (Discoverable depends on shell rows)")
	ids := map[string]bool{}
	for _, c := range camps {
		ids[c.ID] = true
	}
	assert.True(t, ids["c-rust"])
	assert.True(t, ids["c-apex"])

	b.mu.Lock()
	defer b.mu.Unlock()
	require.NotNil(t, b.gotSession.GameFilter, "session must have GameFilter attached")
	assert.True(t, b.gotSession.GameFilter("Rust"))
	assert.False(t, b.gotSession.GameFilter("Apex"))
	assert.Equal(t, "acc-1", b.gotSession.AccountID, "session must carry the source's AccountID")
}

// Kick scraper mirrors Twitch in graceful no-op behaviour.
func TestKickScraper_NoSessionIsNoOp(t *testing.T) {
	b := &stubBackend{}
	source := func(context.Context) (string, platform.Session, bool, error) {
		return "", platform.Session{}, false, nil
	}
	s := NewKickScraper(b, source)
	camps, err := s.Scrape(context.Background(), []string{"rust"})
	require.NoError(t, err)
	assert.Nil(t, camps)
	assert.Equal(t, 0, b.called)
}

// Nil backend (no sidecar configured) ⇒ silent no-op.
func TestKickScraper_NilBackendIsNoOp(t *testing.T) {
	s := NewKickScraper(nil, func(context.Context) (string, platform.Session, bool, error) {
		return "acc-kick", platform.Session{}, true, nil
	})
	camps, err := s.Scrape(context.Background(), []string{"rust"})
	require.NoError(t, err)
	assert.Nil(t, camps)
}

func TestKickScraper_AttachesGameFilter(t *testing.T) {
	b := &stubBackend{result: []platform.Campaign{
		{ID: "kick-rust", Platform: "kick", Game: "Rust"},
	}}
	source := func(context.Context) (string, platform.Session, bool, error) {
		return "acc-kick", platform.Session{}, true, nil
	}
	s := NewKickScraper(b, source)
	camps, err := s.Scrape(context.Background(), []string{"rust"})
	require.NoError(t, err)
	require.Len(t, camps, 1)
	assert.Equal(t, "kick-rust", camps[0].ID)
	b.mu.Lock()
	defer b.mu.Unlock()
	require.NotNil(t, b.gotSession.GameFilter)
	assert.True(t, b.gotSession.GameFilter("Rust"))
}
