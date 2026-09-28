package loop

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/session"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeBackend: one game "G", campaign c1 with drop d1 (60 min). Tests
// mutate the fields under mu to script the platform.
type fakeBackend struct {
	platform.Backend
	mu         sync.Mutex
	camps      []platform.Campaign
	progress   []platform.DropProgress
	inventory  []platform.Progress
	live       []platform.Stream
	claimRes   platform.ClaimResult
	claims     int
	watching   []string
	stops      int
	beatFailOn string
}

func (f *fakeBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.camps, nil
}
func (f *fakeBackend) DropProgress(context.Context, platform.Session, []platform.Campaign) ([]platform.DropProgress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.DropProgress(nil), f.progress...), nil
}
func (f *fakeBackend) ListEligibleChannels(context.Context, platform.Session, platform.Campaign) ([]platform.Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.Stream(nil), f.live...), nil
}
func (f *fakeBackend) InventoryProgress(context.Context, platform.Session) ([]platform.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.Progress(nil), f.inventory...), nil
}
func (f *fakeBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watching = append(f.watching, s.Channel)
	return platform.WatchHandle{Channel: s.Channel}, nil
}
func (f *fakeBackend) Heartbeat(_ context.Context, h platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beatFailOn != "" && strings.EqualFold(h.Channel, f.beatFailOn) {
		return context.DeadlineExceeded
	}
	return nil
}
func (f *fakeBackend) StopWatch(context.Context, platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}
func (f *fakeBackend) ClaimDrop(context.Context, platform.Session, platform.DropProgress) platform.ClaimResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	return f.claimRes
}

type memStore struct {
	mu   sync.Mutex
	rows map[string]dropstate.Row
}

func (m *memStore) List(context.Context, string) ([]dropstate.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []dropstate.Row
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}
func (m *memStore) Upsert(_ context.Context, r dropstate.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.DropID] = r
	return nil
}
func (m *memStore) get(id string) dropstate.Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[id]
}

type memHistory struct {
	mu       sync.Mutex
	recorded []string
	claimed  map[string]bool
}

func (h *memHistory) RecordClaimIfNew(_ context.Context, _ string, b platform.DropBenefit) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recorded = append(h.recorded, b.ID)
	return true, nil
}
func (h *memHistory) ClaimedBenefitIDs(context.Context, string) (map[string]bool, error) {
	return h.claimed, nil
}

func setup(t *testing.T) (*fakeBackend, *memStore, *memHistory, Config) {
	f := &fakeBackend{
		camps: []platform.Campaign{{ID: "c1", Platform: "twitch", Game: "G", Name: "Camp", Status: "active",
			AccountLinked: true, AccountLinkChecked: true,
			Benefits: []platform.DropBenefit{{ID: "d1", CampaignID: "c1", Name: "Drop", RequiredMinutes: 60}}}},
		progress:  []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 10, Required: 60, Known: true}},
		inventory: []platform.Progress{{BenefitID: "d1", MinutesWatched: 10}},
		live:      []platform.Stream{{Channel: "ch1", ViewerCount: 10}, {Channel: "ch2", ViewerCount: 5}},
		claimRes:  platform.ClaimResult{Outcome: platform.ClaimOK},
	}
	st := &memStore{rows: map[string]dropstate.Row{}}
	h := &memHistory{}
	cfg := Config{
		AccountID: "acc", Platform: "twitch", Backend: f, Store: st, History: h,
		AllowGame:      func(g string) bool { return g == "G" },
		ReconcileEvery: time.Hour, LiveEvery: time.Hour,
		BeatEvery: 10 * time.Millisecond, StallPolls: 1000, // tests opt into stalls
	}
	return f, st, h, cfg
}

func run(t *testing.T, cfg Config) (*Loop, context.CancelFunc) {
	l, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = l.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return l, cancel
}

func TestLoop_ClaimedOnWebsiteMidWatch(t *testing.T) {
	f, st, h, cfg := setup(t)
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch1" }, 2*time.Second, 5*time.Millisecond)

	f.mu.Lock()
	f.inventory = []platform.Progress{{BenefitID: "d1", MinutesWatched: 20, Claimed: true}}
	f.mu.Unlock()

	require.Eventually(t, func() bool { return st.get("d1").Status == dropstate.Claimed }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 5*time.Millisecond)
	h.mu.Lock()
	assert.Contains(t, h.recorded, "d1")
	h.mu.Unlock()
	f.mu.Lock()
	assert.Equal(t, 0, f.claims, "no claim call for an already-claimed drop")
	assert.GreaterOrEqual(t, f.stops, 1)
	f.mu.Unlock()
}

func TestLoop_ClaimsWhenClaimable(t *testing.T) {
	f, st, _, cfg := setup(t)
	f.progress[0].Minutes = 60
	f.inventory[0].MinutesWatched = 60
	run(t, cfg)
	require.Eventually(t, func() bool { return st.get("d1").Status == dropstate.Claimed }, 2*time.Second, 5*time.Millisecond)
	f.mu.Lock()
	assert.Equal(t, 1, f.claims)
	f.mu.Unlock()
}

func TestLoop_NeedsLinkBlocksWithoutLooping(t *testing.T) {
	f, st, _, cfg := setup(t)
	f.progress[0].Minutes = 60
	f.inventory[0].MinutesWatched = 60
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimNeedsLink}
	run(t, cfg)
	require.Eventually(t, func() bool { return st.get("d1").Reason == dropstate.NeedsLink }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	assert.Equal(t, 1, f.claims, "needs_link is not retried")
	f.mu.Unlock()
}

func TestLoop_StallCoolsChannelAndSwaps(t *testing.T) {
	_, _, _, cfg := setup(t)
	cfg.StallPolls = 3
	l, _ := run(t, cfg)
	// Minutes never move, so after StallPolls the loop cools ch1 and moves on.
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch2" }, 3*time.Second, 5*time.Millisecond)
}

func TestLoop_StaleStreamDownIgnored(t *testing.T) {
	f, _, _, cfg := setup(t)
	l, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Drive the loop manually: load, reconcile, refresh, plan.
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.replan(ctx)
	require.Equal(t, "ch1", l.current.Channel.Channel)
	l.onSession(ctx, sessionEvent("ch-old-down"))
	assert.Empty(t, l.cooldowns, "event from another channel must not cool anything")
	l.stopSession()
	_ = f
}

func TestLoop_BridgesManualMarksOnLoad(t *testing.T) {
	_, st, h, cfg := setup(t)
	st.rows["d1"] = dropstate.Row{AccountID: "acc", DropID: "d1", CampaignID: "c1", Platform: "twitch", Status: dropstate.Accruing, Minutes: 10, Required: 60, Source: dropstate.FromPlatform}
	h.claimed = map[string]bool{"d1": true}
	l, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, l.load(context.Background()))
	assert.Equal(t, dropstate.Claimed, st.get("d1").Status)
	assert.Equal(t, dropstate.FromUser, st.get("d1").Source)
}

func TestNew_RejectsBackendWithoutCapabilities(t *testing.T) {
	type bare struct{ platform.Backend }
	_, err := New(Config{AccountID: "a", Backend: bare{}})
	require.Error(t, err)
}

func sessionEvent(ch string) session.Event {
	return session.Event{Kind: session.StreamDown, Channel: ch}
}
