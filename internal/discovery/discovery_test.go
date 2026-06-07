package discovery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// recordingPersister captures every batch the Scraper hands to it so
// tests can assert what was scraped + persisted.
type recordingPersister struct {
	mu      sync.Mutex
	batches [][]platform.Campaign
	err     error
}

func (r *recordingPersister) PersistCampaigns(_ context.Context, camps []platform.Campaign) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	// copy so the caller can't mutate the slice we hold for assertions
	cp := append([]platform.Campaign(nil), camps...)
	r.batches = append(r.batches, cp)
	return nil
}

func (r *recordingPersister) all() [][]platform.Campaign {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]platform.Campaign, len(r.batches))
	copy(out, r.batches)
	return out
}

// fakeProvider returns a fixed slice; useful when we want to check
// whitelist plumbing and persister wiring without spinning up a backend.
type fakeProvider struct {
	name      string
	camps     []platform.Campaign
	err       error
	calls     atomic.Int32
	lastList  []string
	listMu    sync.Mutex
	scrapeFn  func(ctx context.Context, whitelist []string) ([]platform.Campaign, error)
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) Scrape(ctx context.Context, whitelist []string) ([]platform.Campaign, error) {
	p.calls.Add(1)
	p.listMu.Lock()
	p.lastList = append([]string(nil), whitelist...)
	p.listMu.Unlock()
	if p.scrapeFn != nil {
		return p.scrapeFn(ctx, whitelist)
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.camps, nil
}

// Tick honors the whitelist source: empty whitelist must short-circuit
// without calling any provider (project_goal.md: never scrape
// non-whitelisted games).
func TestTick_EmptyWhitelistShortCircuits(t *testing.T) {
	p := &fakeProvider{name: "fake"}
	per := &recordingPersister{}
	s := New(per, func(ctx context.Context) ([]string, error) { return nil, nil }, p)

	s.Tick(context.Background())

	assert.Equal(t, int32(0), p.calls.Load(), "provider must not be called when whitelist is empty")
	assert.Empty(t, per.all(), "persister must not be called when whitelist is empty")
}

// Tick forwards the whitelist to every provider and persists what they
// return.
func TestTick_PassesWhitelistAndPersists(t *testing.T) {
	want := []platform.Campaign{{ID: "c1", Platform: "twitch", Game: "Rust"}}
	p := &fakeProvider{name: "twitch", camps: want}
	per := &recordingPersister{}
	s := New(per, func(ctx context.Context) ([]string, error) {
		return []string{"rust", "fortnite"}, nil
	}, p)

	s.Tick(context.Background())

	require.Equal(t, int32(1), p.calls.Load())
	p.listMu.Lock()
	assert.Equal(t, []string{"rust", "fortnite"}, p.lastList)
	p.listMu.Unlock()
	batches := per.all()
	require.Len(t, batches, 1)
	assert.Equal(t, want, batches[0])
}

// A provider error must not block subsequent providers — the Scraper
// logs and continues so one platform failing doesn't poison the others.
func TestTick_OneProviderErrorDoesNotBlockOthers(t *testing.T) {
	bad := &fakeProvider{name: "bad", err: errors.New("boom")}
	good := &fakeProvider{name: "good", camps: []platform.Campaign{{ID: "c2"}}}
	per := &recordingPersister{}
	s := New(per, func(ctx context.Context) ([]string, error) { return []string{"rust"}, nil }, bad, good)

	s.Tick(context.Background())

	assert.Equal(t, int32(1), bad.calls.Load())
	assert.Equal(t, int32(1), good.calls.Load())
	batches := per.all()
	require.Len(t, batches, 1, "only the good provider's batch should be persisted")
	assert.Equal(t, "c2", batches[0][0].ID)
}

// A provider returning (nil, nil) — the graceful no-op shape used when
// auth context is missing — must not invoke the persister at all.
func TestTick_NoOpProviderSkipsPersister(t *testing.T) {
	noop := &fakeProvider{name: "noop"} // camps nil, err nil
	per := &recordingPersister{}
	s := New(per, func(ctx context.Context) ([]string, error) { return []string{"rust"}, nil }, noop)

	s.Tick(context.Background())

	assert.Equal(t, int32(1), noop.calls.Load())
	assert.Empty(t, per.all(), "persister must not be hit when provider returns no campaigns")
}

// Run must invoke Tick once immediately on entry so the /drops page is
// populated before the first ticker fires (which would otherwise add a
// 5-minute delay on cold boot).
func TestRun_FiresImmediately(t *testing.T) {
	p := &fakeProvider{name: "fake", camps: []platform.Campaign{{ID: "c"}}}
	per := &recordingPersister{}
	s := New(per, func(ctx context.Context) ([]string, error) { return []string{"rust"}, nil }, p)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx, 1*time.Hour) // huge interval — we only want the immediate tick
		close(done)
	}()

	// Wait up to 2s for the first tick.
	deadline := time.After(2 * time.Second)
	for p.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("Run did not invoke Tick within 2s of entry")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done

	assert.GreaterOrEqual(t, p.calls.Load(), int32(1))
	assert.NotEmpty(t, per.all())
}

// buildAllowList rejects everything when given an empty slice — the
// safe default for "no opt-ins configured".
func TestBuildAllowList_EmptyRejects(t *testing.T) {
	allow := buildAllowList(nil)
	assert.False(t, allow("Rust"))
	assert.False(t, allow(""))
}

// buildAllowList is case-insensitive and trims whitespace, matching
// the platform.Session.GameFilter contract.
func TestBuildAllowList_CaseInsensitive(t *testing.T) {
	allow := buildAllowList([]string{"rust", "fortnite"})
	assert.True(t, allow("Rust"))
	assert.True(t, allow("  RUST  "))
	assert.True(t, allow("fortnite"))
	assert.False(t, allow("Apex"))
}
