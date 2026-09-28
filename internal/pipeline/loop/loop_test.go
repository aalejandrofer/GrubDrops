package loop

import (
	"context"
	"errors"
	"fmt"
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
	// claimedAfterFail simulates a "lost claim response": the platform
	// actually recorded the claim server-side even though ClaimDrop reports
	// claimRes (e.g. ClaimFailed) to the caller. Flips every progress item's
	// Claimed flag the moment ClaimDrop is called, so the next reconcile's
	// DropProgress read sees it.
	claimedAfterFail bool
	// listGames records the Session.Games each ListActiveCampaigns saw.
	listGames [][]string
}

func (f *fakeBackend) ListActiveCampaigns(_ context.Context, s platform.Session) ([]platform.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listGames = append(f.listGames, s.Games)
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
	if f.claimedAfterFail {
		for i := range f.progress {
			f.progress[i].Claimed = true
		}
	}
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
	// Same generation as the live session: this exercises the channel check,
	// not the generation check (see TestLoop_StaleGenerationIgnored for that).
	l.onSession(ctx, sessionEvent(l.gen, "ch-old-down"))
	assert.Empty(t, l.cooldowns, "event from another channel must not cool anything")
	l.stopSession()
	_ = f
}

// TestLoop_StaleGenerationIgnored covers the generation stamp itself
// (item 4): an event tagged with a generation older than the current
// session must be dropped even though it matches the current channel/kind,
// otherwise a leftover event from a just-stopped session (e.g. a
// serves-change restart) could set a bogus cooldown.
func TestLoop_StaleGenerationIgnored(t *testing.T) {
	f, _, _, cfg := setup(t)
	l, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.replan(ctx)
	require.Equal(t, "ch1", l.current.Channel.Channel)
	staleGen := l.gen - 1
	l.onSession(ctx, sessionEvent(staleGen, "ch1"))
	assert.Empty(t, l.cooldowns, "event from a stale generation must not cool the live channel")
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

func sessionEvent(gen int, ch string) session.Event {
	return session.Event{Kind: session.StreamDown, Channel: ch, Gen: gen}
}

// TestLoop_ServesChangeRestartsSessionNoFalseStall covers item 1: a
// precondition chain (d1 -> d2) on the same channel. The running session
// pins its stall bookkeeping to the serves set it started with (d1). Once
// d1 is claimed, d2 becomes mineable and the decision's Serves flips to
// [d2] while Kind/Channel stay the same ("Same" per the planner). Without
// restarting the session on the new serves set, the session keeps watching
// for gains on d1 (now absent from inventory) and never credits d2's real
// gains, so it falsely stalls and cools ch1 even though d2 is accruing.
func TestLoop_ServesChangeRestartsSessionNoFalseStall(t *testing.T) {
	f := &fakeBackend{
		camps: []platform.Campaign{{
			ID: "c1", Platform: "twitch", Game: "G", Name: "Camp", Status: "active",
			AccountLinked: true, AccountLinkChecked: true,
			Benefits: []platform.DropBenefit{
				{ID: "d1", CampaignID: "c1", Name: "Drop1", RequiredMinutes: 60},
				{ID: "d2", CampaignID: "c1", Name: "Drop2", RequiredMinutes: 100000, Preconditions: []string{"d1"}},
			},
		}},
		progress:  []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 58, Required: 60, Known: true}},
		inventory: []platform.Progress{{BenefitID: "d1", MinutesWatched: 58}},
		live:      []platform.Stream{{Channel: "ch1", ViewerCount: 10}},
		claimRes:  platform.ClaimResult{Outcome: platform.ClaimOK},
	}
	st := &memStore{rows: map[string]dropstate.Row{}}
	cfg := Config{
		AccountID: "acc", Platform: "twitch", Backend: f, Store: st,
		AllowGame:      func(g string) bool { return g == "G" },
		ReconcileEvery: time.Hour, LiveEvery: time.Hour,
		BeatEvery: 10 * time.Millisecond, StallPolls: 3,
	}
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch1" }, 2*time.Second, 5*time.Millisecond)

	// Push d1 to completion so it gets claimed and d2's precondition clears.
	for m := 59; m <= 60; m++ {
		f.mu.Lock()
		f.progress[0].Minutes = m
		f.inventory[0].MinutesWatched = m
		f.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	require.Eventually(t, func() bool { return st.get("d1").Status == dropstate.Claimed }, 2*time.Second, 5*time.Millisecond)

	// d1 leaves inventory (claimed, as real platforms do); d2 becomes the
	// only servable drop and keeps rising. Cap well under its (huge)
	// required minutes so it never itself completes mid-test.
	f.mu.Lock()
	f.progress = []platform.DropProgress{{DropID: "d2", CampaignID: "c1", Minutes: 0, Required: 100000, Known: true}}
	f.inventory = []platform.Progress{{BenefitID: "d2", MinutesWatched: 0}}
	f.mu.Unlock()

	minute := 0
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		minute++
		f.mu.Lock()
		f.progress[0].Minutes = minute
		f.inventory[0].MinutesWatched = minute
		f.mu.Unlock()
		time.Sleep(3 * time.Millisecond)
	}

	assert.Equal(t, "ch1", l.Snapshot().Channel, "must not have swapped/cooled off ch1 on a false stall")
	assert.NotEqual(t, "idle", l.Snapshot().State, "must still be watching, not idled out by a false stall")
	assert.Greater(t, st.get("d2").Minutes, 0, "d2 must have kept accruing after the serves-set handoff")
}

type flakyHistory struct {
	mu       sync.Mutex
	failed   bool
	recorded []string
}

func (h *flakyHistory) RecordClaimIfNew(_ context.Context, _ string, b platform.DropBenefit) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.failed {
		h.failed = true
		return false, errors.New("transient history store error")
	}
	h.recorded = append(h.recorded, b.ID)
	return true, nil
}
func (h *flakyHistory) ClaimedBenefitIDs(context.Context, string) (map[string]bool, error) {
	return nil, nil
}
func (h *flakyHistory) has(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, x := range h.recorded {
		if x == id {
			return true
		}
	}
	return false
}

// TestLoop_FailedHistoryRecordIsRetried covers item 2: a RecordClaimIfNew
// failure must not be dropped on the floor. The drop id goes into
// pendingHistory and gets retried on a later loop iteration.
func TestLoop_FailedHistoryRecordIsRetried(t *testing.T) {
	f, st, _, cfg := setup(t)
	f.progress[0].Minutes = 60
	f.inventory[0].MinutesWatched = 60
	fh := &flakyHistory{}
	cfg.History = fh
	run(t, cfg)
	require.Eventually(t, func() bool { return st.get("d1").Status == dropstate.Claimed }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return fh.has("d1") }, 2*time.Second, 5*time.Millisecond)
}

// TestLoop_LostClaimResponseCorrectedByReconcile covers item 3: ClaimDrop
// reports failure (a "lost response" - the platform actually recorded the
// claim but the client saw an error), but the platform's own progress read
// on the post-claim reconcile shows Claimed:true. The row must still end
// up Claimed/FromPlatform.
func TestLoop_LostClaimResponseCorrectedByReconcile(t *testing.T) {
	f, st, _, cfg := setup(t)
	f.progress[0].Minutes = 60
	f.inventory[0].MinutesWatched = 60
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimFailed}
	f.claimedAfterFail = true
	run(t, cfg)
	require.Eventually(t, func() bool {
		r := st.get("d1")
		return r.Status == dropstate.Claimed && r.Source == dropstate.FromPlatform
	}, 2*time.Second, 5*time.Millisecond)
	f.mu.Lock()
	assert.GreaterOrEqual(t, f.claims, 1, "a claim attempt must have happened and been reported as failed")
	f.mu.Unlock()
}

type recNotifier struct {
	mu     sync.Mutex
	events []string
}

func (n *recNotifier) Notify(_ context.Context, event string, _ map[string]any) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
	return nil
}

// TestMaybeNotifyProgress_MilestoneNeverLowers covers item 5: a later,
// lower reading of Minutes (e.g. a stale re-sync) must not lower the
// recorded milestone nor re-fire a notification once real progress passes it.
func TestMaybeNotifyProgress_MilestoneNeverLowers(t *testing.T) {
	n := &recNotifier{}
	l := &Loop{
		cfg:        Config{ProgressNotifyStepPct: 10, Notifier: n},
		milestones: map[string]int{},
	}
	ctx := context.Background()
	l.maybeNotifyProgress(ctx, dropstate.Row{DropID: "d1", Minutes: 50, Required: 100}) // baseline @ 50%
	l.maybeNotifyProgress(ctx, dropstate.Row{DropID: "d1", Minutes: 30, Required: 100}) // regressed reading
	assert.Equal(t, 50, l.milestones["d1"], "milestone must not be lowered by a regressed reading")
	l.maybeNotifyProgress(ctx, dropstate.Row{DropID: "d1", Minutes: 60, Required: 100}) // real forward progress
	assert.Equal(t, 60, l.milestones["d1"])
	n.mu.Lock()
	defer n.mu.Unlock()
	assert.Equal(t, []string{"progress"}, n.events, "only genuine forward progress notifies")
}

// TestOnPubSub_ClaimableWithUnknownRequiredSetsClaimableDirectly covers
// item 6: a claimable pubsub event for a drop whose Required is still 0
// (not yet known) must set the row Claimable directly rather than going
// through Apply/derive, which reads required<=0 as "blocked: sub_only".
func TestOnPubSub_ClaimableWithUnknownRequiredSetsClaimableDirectly(t *testing.T) {
	st := &memStore{rows: map[string]dropstate.Row{}}
	l := &Loop{
		cfg:      Config{AccountID: "acc", Store: st, Now: time.Now},
		rows:     map[string]dropstate.Row{"d1": {AccountID: "acc", DropID: "d1", CampaignID: "c1", Status: dropstate.Eligible, Required: 0}},
		progress: map[string]platform.DropProgress{},
	}
	l.onPubSub(context.Background(), pubsubEvent{kind: "claimable", drop: "d1", instance: "inst1"})
	r := l.rows["d1"]
	assert.Equal(t, dropstate.Claimable, r.Status)
	assert.Equal(t, dropstate.NoReason, r.Reason)
	assert.True(t, r.RetryAfter.IsZero())
	assert.Equal(t, dropstate.FromPlatform, r.Source)
	assert.Equal(t, "inst1", l.progress["d1"].InstanceID)
}

// probeBackend adds a ChannelProber that reports a priority streamer live.
type probeBackend struct {
	*fakeBackend
	pmu    sync.Mutex
	probed []string // campaign ids probed
}

func (p *probeBackend) ProbeChannels(_ context.Context, _ platform.Session, c platform.Campaign, _ []string) ([]platform.Stream, error) {
	p.pmu.Lock()
	defer p.pmu.Unlock()
	p.probed = append(p.probed, c.ID)
	return []platform.Stream{{Channel: "prio", ViewerCount: 1}}, nil
}

// I4: a restricted campaign whose allow-list the loop can't see (Twitch
// reports only a count) must not get priority streamers probed in, or the
// planner would watch a channel the campaign doesn't credit.
func TestLoop_RestrictedCampaignWithoutAllowListSkipsPriorityProbe(t *testing.T) {
	f, _, _, cfg := setup(t)
	f.camps[0].AllowedChannelCount = 3
	pb := &probeBackend{fakeBackend: f}
	cfg.Backend = pb
	cfg.StreamerPriority = []string{"prio"}
	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.replan(ctx)
	defer l.stopSession()

	pb.pmu.Lock()
	assert.NotContains(t, pb.probed, "c1", "restricted campaign with unknown allow-list must not be probed")
	pb.pmu.Unlock()
	for _, s := range l.live["c1"] {
		assert.NotEqual(t, "prio", s.Channel)
	}
	require.NotNil(t, l.current)
	assert.Equal(t, "ch1", l.current.Channel.Channel)
}

// Control for the above: an unrestricted campaign still gets priority probing.
func TestLoop_UnrestrictedCampaignProbesPriority(t *testing.T) {
	f, _, _, cfg := setup(t)
	pb := &probeBackend{fakeBackend: f}
	cfg.Backend = pb
	cfg.StreamerPriority = []string{"prio"}
	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	pb.pmu.Lock()
	assert.Contains(t, pb.probed, "c1")
	pb.pmu.Unlock()
}

// integrityBackend fails discovery with the integrity wall.
type integrityBackend struct{ *fakeBackend }

func (integrityBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	return nil, fmt.Errorf("gql: %w", platform.ErrIntegrityBlocked)
}

// I6: the integrity wall surfaces as auth_required and the loop exits cleanly
// (like v1) so the next reload re-spins it once the session is refreshed.
func TestLoop_IntegrityBlockedExitsAuthRequired(t *testing.T) {
	f, _, _, cfg := setup(t)
	cfg.Backend = integrityBackend{f}
	l, err := New(cfg)
	require.NoError(t, err)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- l.Run(ctx) }()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit on integrity wall")
	}
	assert.Equal(t, "auth_required", l.Snapshot().State)
	f.mu.Lock()
	assert.Empty(t, f.watching, "no session starts behind an integrity wall")
	f.mu.Unlock()
}

// subBackend records PubSub subscriptions and detects StopWatch overlapping
// the next StartWatch.
type subBackend struct {
	*fakeBackend
	smu      sync.Mutex
	subs     []string
	unsubs   []string
	inStop   bool
	overlaps int
	starts   int
}

func (s *subBackend) SubscribeChannel(_, id string) {
	s.smu.Lock()
	defer s.smu.Unlock()
	s.subs = append(s.subs, id)
}
func (s *subBackend) UnsubscribeChannel(_, id string) {
	s.smu.Lock()
	defer s.smu.Unlock()
	s.unsubs = append(s.unsubs, id)
}
func (s *subBackend) StartWatch(ctx context.Context, sess platform.Session, st platform.Stream) (platform.WatchHandle, error) {
	s.smu.Lock()
	if s.inStop {
		s.overlaps++
	}
	s.starts++
	s.smu.Unlock()
	return s.fakeBackend.StartWatch(ctx, sess, st)
}
func (s *subBackend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	s.smu.Lock()
	s.inStop = true
	s.smu.Unlock()
	time.Sleep(50 * time.Millisecond)
	s.smu.Lock()
	s.inStop = false
	s.smu.Unlock()
	return s.fakeBackend.StopWatch(ctx, h)
}
func (s *subBackend) counts() (subs, unsubs, starts, overlaps int) {
	s.smu.Lock()
	defer s.smu.Unlock()
	return len(s.subs), len(s.unsubs), s.starts, s.overlaps
}

// A serves-change restart on the same channel waits for the old session's
// StopWatch before the new StartWatch, and keeps the PubSub subscription.
func TestLoop_ServesRestartWaitsForStopAndKeepsSubscription(t *testing.T) {
	f, _, _, cfg := setup(t)
	f.live = []platform.Stream{{Channel: "ch1", ChannelID: "id1", ViewerCount: 10}}
	sb := &subBackend{fakeBackend: f}
	cfg.Backend = sb
	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.replan(ctx)
	require.Eventually(t, func() bool { _, _, starts, _ := sb.counts(); return starts == 1 }, 2*time.Second, 5*time.Millisecond)

	// Force a serves-set change on the same decision.
	l.current.Serves = []string{"stale"}
	l.replan(ctx)
	require.Eventually(t, func() bool { _, _, starts, _ := sb.counts(); return starts == 2 }, 2*time.Second, 5*time.Millisecond)

	subs, unsubs, _, overlaps := sb.counts()
	assert.Equal(t, 0, overlaps, "StartWatch must not run while the previous StopWatch is in flight")
	assert.Equal(t, 1, subs, "same-channel restart must not resubscribe")
	assert.Equal(t, 0, unsubs, "same-channel restart must not unsubscribe")

	l.stopSession()
	subs, unsubs, _, _ = sb.counts()
	assert.Equal(t, 1, subs)
	assert.Equal(t, 1, unsubs, "final stop unsubscribes")
	f.mu.Lock()
	assert.Equal(t, 2, f.stops, "final stop waited for StopWatch")
	f.mu.Unlock()
}

// TV-client Twitch sessions discover channel-first and need Session.Games
// (the whitelisted names); Config.Games must reach every discovery call.
func TestLoop_ConfigGamesReachSession(t *testing.T) {
	f, _, _, cfg := setup(t)
	cfg.Games = []string{"G", "Other Game"}
	run(t, cfg)
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.listGames) > 0
	}, 2*time.Second, 5*time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.listGames {
		assert.Equal(t, []string{"G", "Other Game"}, g)
	}
}

// probeSetup scripts the Twitch "claimed outside the app, campaign left the
// Inventory" case: the platform reports nothing for d1 (DropCampaignDetails
// self: null, not in Inventory), so reconcile derives it Eligible and the
// session never sees a gain on it.
func probeSetup(t *testing.T) (*fakeBackend, *memStore, *memHistory, Config) {
	f, st, h, cfg := setup(t)
	f.progress = nil
	f.inventory = nil
	cfg.StallPolls = 3
	cfg.StallClaimProbe = true
	return f, st, h, cfg
}

func (f *fakeBackend) claimCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims
}

// (a) An Eligible served drop absent from the inventory gets one claim on
// stall; ALREADY_CLAIMED marks it claimed from the platform, records history
// and the planner moves on instead of re-watching it.
func TestLoop_StallClaimProbe_AlreadyClaimedMarksClaimed(t *testing.T) {
	f, st, h, cfg := probeSetup(t)
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimAlready}
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool {
		r := st.get("d1")
		return r.Status == dropstate.Claimed && r.Source == dropstate.FromPlatform
	}, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, f.claimCount(), "exactly one probe claim")
	h.mu.Lock()
	assert.Contains(t, h.recorded, "d1")
	h.mu.Unlock()
}

// (b) A failed probe blocks the drop not_enrolled without counting a claim
// failure, and it is not probed again.
func TestLoop_StallClaimProbe_FailedBlocksNotEnrolled(t *testing.T) {
	f, st, _, cfg := probeSetup(t)
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimFailed}
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool {
		r := st.get("d1")
		return r.Status == dropstate.Blocked && r.Reason == dropstate.NotEnrolled
	}, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	r := st.get("d1")
	assert.Equal(t, 0, r.FailCount, "a probe is not a claim failure")
	assert.Equal(t, dropstate.FromPlatform, r.Source)
	assert.Equal(t, 1, f.claimCount(), "exactly one probe claim")
}

// (c) A drop present in the inventory (really tracked, just frozen) is a
// plain stall: no probe claim.
func TestLoop_StallClaimProbe_SkipsDropInInventory(t *testing.T) {
	f, st, _, cfg := probeSetup(t)
	f.inventory = []platform.Progress{{BenefitID: "d1", MinutesWatched: 0}}
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch2" }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, f.claimCount())
	assert.Equal(t, dropstate.Eligible, st.get("d1").Status)
}

// (d) Probe disabled (Kick): stall behaviour unchanged, no claim.
func TestLoop_StallClaimProbe_DisabledNoClaim(t *testing.T) {
	f, st, _, cfg := probeSetup(t)
	cfg.StallClaimProbe = false
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch2" }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, f.claimCount())
	assert.Equal(t, dropstate.Eligible, st.get("d1").Status)
}

// (e) Only Eligible rows are probed: an Accruing row missing from the
// inventory is not claimed.
func TestLoop_StallClaimProbe_SkipsAccruing(t *testing.T) {
	f, st, _, cfg := probeSetup(t)
	f.progress = []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 10, Required: 60, Known: true}}
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch2" }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, f.claimCount())
	assert.Equal(t, dropstate.Accruing, st.get("d1").Status)
}
