package loop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Batch 1.4.5 item 2: a PubSub "claimable" event must not override a user
// skip or a campaign link block, even when Required is still unknown (the
// Required==0 branch used to set Claimable directly and bypass every block).
// Revert-proof: drop the block guard in onPubSub "claimable" and the
// Required==0 cases flip to Claimable.
func TestOnPubSub_ClaimableRespectsUserSkipAndUnlinked(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reason   dropstate.Reason
		required int
	}{
		{"user_skip unknown required", dropstate.UserSkip, 0},
		{"unlinked unknown required", dropstate.Unlinked, 0},
		{"user_skip known required", dropstate.UserSkip, 60},
		{"unlinked known required", dropstate.Unlinked, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &memStore{rows: map[string]dropstate.Row{}}
			prev := dropstate.Row{AccountID: "acc", DropID: "d1", CampaignID: "c1", Status: dropstate.Blocked, Reason: tc.reason, Required: tc.required, Minutes: 5, Source: dropstate.FromUser}
			l := &Loop{
				cfg:      Config{AccountID: "acc", Store: st, Now: time.Now},
				rows:     map[string]dropstate.Row{"d1": prev},
				progress: map[string]platform.DropProgress{},
			}
			l.onPubSub(context.Background(), pubsubEvent{kind: "claimable", drop: "d1", instance: "inst1"})
			assert.Equal(t, prev, l.rows["d1"], "blocked row must be untouched")
			assert.Equal(t, dropstate.Row{}, st.get("d1"), "nothing persisted")
			assert.Empty(t, l.progress, "no instance recorded for a blocked drop")
		})
	}
}

// Control: a claimable event for a platform-confirmed claim does nothing,
// and one for an ordinary Accruing row with a known Required makes it
// Claimable through Apply.
func TestOnPubSub_ClaimableNormalRowBecomesClaimable(t *testing.T) {
	st := &memStore{rows: map[string]dropstate.Row{}}
	claimed := dropstate.Row{AccountID: "acc", DropID: "d2", CampaignID: "c1", Status: dropstate.Claimed, Source: dropstate.FromPlatform, Required: 60, Minutes: 60}
	l := &Loop{
		cfg: Config{AccountID: "acc", Store: st, Now: time.Now},
		rows: map[string]dropstate.Row{
			"d1": {AccountID: "acc", DropID: "d1", CampaignID: "c1", Status: dropstate.Accruing, Required: 60, Minutes: 30, Source: dropstate.FromPlatform},
			"d2": claimed,
		},
		progress: map[string]platform.DropProgress{},
	}
	l.onPubSub(context.Background(), pubsubEvent{kind: "claimable", drop: "d1", instance: "inst1"})
	l.onPubSub(context.Background(), pubsubEvent{kind: "claimable", drop: "d2", instance: "inst2"})
	assert.Equal(t, dropstate.Claimable, l.rows["d1"].Status)
	assert.Equal(t, 60, l.rows["d1"].Minutes)
	assert.Equal(t, "inst1", l.progress["d1"].InstanceID)
	assert.Equal(t, claimed, l.rows["d2"])
}

// liveProber is a ChannelProber whose answer is scripted per login.
type liveProber struct {
	*fakeBackend
	pmu    sync.Mutex
	isLive map[string]bool
	calls  [][]string // logins per call
}

func (p *liveProber) ProbeChannels(_ context.Context, _ platform.Session, _ platform.Campaign, logins []string) ([]platform.Stream, error) {
	p.pmu.Lock()
	defer p.pmu.Unlock()
	p.calls = append(p.calls, append([]string(nil), logins...))
	var out []platform.Stream
	for _, lg := range logins {
		if p.isLive[strings.ToLower(lg)] {
			out = append(out, platform.Stream{Channel: lg, ViewerCount: 3})
		}
	}
	return out, nil
}

func (f *fakeBackend) setLive(s ...platform.Stream) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live = s
}

// flipSetup lands the loop on ch1 (c1), then makes the next directory page
// omit ch1 (top-N churn) while ch2 stays listed.
func flipSetup(t *testing.T, backend platform.Backend, f *fakeBackend, cfg Config) (*Loop, context.Context) {
	t.Helper()
	cfg.Backend = backend
	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.replan(ctx)
	t.Cleanup(l.stopSession)
	require.Equal(t, "ch1", l.current.Channel.Channel)
	require.Eventually(t, func() bool { return len(f.watchedChannels()) == 1 }, 2*time.Second, 5*time.Millisecond)
	f.setLive(platform.Stream{Channel: "ch2", ViewerCount: 5})
	return l, ctx
}

// Batch 1.4.5 item 3: the current channel dropping out of a fresh
// ListEligibleChannels page is re-checked with the prober; still live means
// no swap. Revert-proof: remove the refreshLive re-check and v2 swaps to ch2.
func TestLoop_RefreshKeepsCurrentChannelWhenProberSaysLive(t *testing.T) {
	f, _, _, cfg := setup(t)
	p := &liveProber{fakeBackend: f, isLive: map[string]bool{"ch1": true}}
	l, ctx := flipSetup(t, p, f, cfg)

	l.refreshLive(ctx)
	l.replan(ctx)
	time.Sleep(30 * time.Millisecond)

	assert.Equal(t, "ch1", l.current.Channel.Channel, "no swap while the current channel is still live")
	assert.Equal(t, "ch1", l.Snapshot().Channel)
	assert.Equal(t, []string{"ch1"}, f.watchedChannels(), "StartWatch count unchanged")
	p.pmu.Lock()
	assert.Equal(t, [][]string{{"ch1"}}, p.calls, "exactly one re-check, for the current channel only")
	p.pmu.Unlock()
}

// The prober reporting the current channel offline lets the swap happen.
func TestLoop_RefreshSwapsWhenProberSaysCurrentOffline(t *testing.T) {
	f, _, _, cfg := setup(t)
	p := &liveProber{fakeBackend: f, isLive: map[string]bool{}}
	l, ctx := flipSetup(t, p, f, cfg)

	l.refreshLive(ctx)
	l.replan(ctx)

	assert.Equal(t, "ch2", l.current.Channel.Channel)
	require.Eventually(t, func() bool { return len(f.watchedChannels()) == 2 }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"ch1", "ch2"}, f.watchedChannels())
}

// No prober: behaviour unchanged, the refresh that omits ch1 swaps to ch2.
func TestLoop_RefreshWithoutProberSwapsAsBefore(t *testing.T) {
	f, _, _, cfg := setup(t)
	l, ctx := flipSetup(t, f, f, cfg)

	l.refreshLive(ctx)
	l.replan(ctx)

	assert.Equal(t, "ch2", l.current.Channel.Channel)
}

// A current channel the fresh page still lists is not re-probed.
func TestLoop_RefreshDoesNotReprobeListedCurrentChannel(t *testing.T) {
	f, _, _, cfg := setup(t)
	p := &liveProber{fakeBackend: f, isLive: map[string]bool{"ch1": true}}
	l, ctx := flipSetup(t, p, f, cfg)
	f.setLive(platform.Stream{Channel: "ch1", ViewerCount: 10}, platform.Stream{Channel: "ch2", ViewerCount: 5})

	l.refreshLive(ctx)
	l.replan(ctx)

	assert.Equal(t, "ch1", l.current.Channel.Channel)
	p.pmu.Lock()
	assert.Empty(t, p.calls)
	p.pmu.Unlock()
}

// catchUpSetup scripts a Twitch account added after it finished its drops:
// campaign c1 is active and in scope with n drops, and the platform reports
// nothing for any of them (campaign left the Inventory, details self null),
// so reconcile derives every row Eligible from a Known=false observation.
func catchUpSetup(t *testing.T, n int) (*fakeBackend, *memStore, Config) {
	f, st, _, cfg := setup(t)
	var bs []platform.DropBenefit
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("d%d", i)
		bs = append(bs, platform.DropBenefit{ID: id, CampaignID: "c1", Name: id, RequiredMinutes: 60 * i})
	}
	f.camps[0].Benefits = bs
	f.progress = nil
	f.inventory = nil
	f.live = nil // nothing live: any claim below comes from catch-up, never a watch
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimFailed}
	cfg.ClaimProbe = true
	cfg.CatchUpEvery = 2 * time.Millisecond
	return f, st, cfg
}

func (f *fakeBackend) claimedIDsCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.claimedIDs...)
}

func (f *fakeBackend) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listGames)
}

// Batch 1.4.5 item 1: a new account whose drops are already done. Already
// → Claimed/FromPlatform without any watch; Failed leaves the row Eligible
// and is not re-probed on the next reconcile. Revert-proof: remove the
// catch-up enqueue in reconcile and no claim is ever sent (nothing is live,
// so the stall probe never runs either).
func TestLoop_CatchUpClaimProbe_NewAccountAlreadyClaimed(t *testing.T) {
	f, st, cfg := catchUpSetup(t, 3)
	f.claimByDrop = map[string]platform.ClaimResult{
		"d1": {Outcome: platform.ClaimAlready},
		"d2": {Outcome: platform.ClaimAlready},
		"d3": {Outcome: platform.ClaimFailed},
	}
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool {
		return st.get("d1").Status == dropstate.Claimed && st.get("d2").Status == dropstate.Claimed && f.claimCount() == 3
	}, 2*time.Second, 2*time.Millisecond)
	for _, id := range []string{"d1", "d2"} {
		assert.Equal(t, dropstate.FromPlatform, st.get(id).Source, id)
	}
	r3 := st.get("d3")
	assert.Equal(t, dropstate.Eligible, r3.Status, "a failed catch-up probe leaves the row unchanged")
	assert.Equal(t, dropstate.NoReason, r3.Reason)
	assert.Equal(t, 0, r3.FailCount, "a catch-up probe is not a claim failure")
	assert.Empty(t, f.watchedChannels(), "claimed without any watch")

	before := f.listCount()
	l.Nudge()
	require.Eventually(t, func() bool { return f.listCount() > before }, 2*time.Second, 2*time.Millisecond)
	time.Sleep(40 * time.Millisecond) // many CatchUpEvery periods
	assert.ElementsMatch(t, []string{"d1", "d2", "d3"}, f.claimedIDsCopy(), "each drop probed once per loop run")
	assert.Equal(t, dropstate.Eligible, st.get("d3").Status)
}

// Fix round 1 ruling: an ALREADY_CLAIMED catch-up answer is a historical
// claim (recorded in history, no notification); only a claim that succeeded
// just now (OK) notifies. Revert-proof: make commitProbeResult always call
// commit and the Already count becomes 1.
func TestLoop_CatchUpClaimProbe_NotifiesOnlyOK(t *testing.T) {
	f, st, cfg := catchUpSetup(t, 2)
	f.claimByDrop = map[string]platform.ClaimResult{
		"d1": {Outcome: platform.ClaimAlready},
		"d2": {Outcome: platform.ClaimOK},
	}
	h := &memHistory{}
	cfg.History = h
	n := &recNotifier{}
	cfg.Notifier = n
	run(t, cfg)
	require.Eventually(t, func() bool {
		return st.get("d1").Status == dropstate.Claimed && st.get("d2").Status == dropstate.Claimed
	}, 2*time.Second, 2*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 1, n.count("claim"), "only the OK claim notifies")
	h.mu.Lock()
	assert.ElementsMatch(t, []string{"d1", "d2"}, h.recorded, "both claims still reach the claim history")
	h.mu.Unlock()
}

// NeedsLink maps to the needs_link block.
func TestLoop_CatchUpClaimProbe_NeedsLinkBlocks(t *testing.T) {
	f, st, cfg := catchUpSetup(t, 1)
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimNeedsLink}
	run(t, cfg)
	require.Eventually(t, func() bool { return st.get("d1").Reason == dropstate.NeedsLink }, 2*time.Second, 2*time.Millisecond)
	assert.Equal(t, dropstate.Blocked, st.get("d1").Status)
}

// Cap: with 8 candidates one reconcile queues only catchUpPerReconcile; the
// rest wait for the next reconcile, and nothing is probed twice. Driven by
// hand so the drain is deterministic.
func TestLoop_CatchUpClaimProbe_CapPerReconcile(t *testing.T) {
	f, _, cfg := catchUpSetup(t, 8)
	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	drain := func() {
		for i := 0; i < 20; i++ {
			l.catchUpOne(ctx)
		}
	}

	l.reconcile(ctx)
	assert.Equal(t, 0, f.claimCount(), "reconcile only queues; probes are spaced by the loop timer")
	drain()
	assert.Equal(t, catchUpPerReconcile, f.claimCount())

	l.reconcile(ctx)
	drain()
	assert.Equal(t, 8, f.claimCount(), "the remaining candidates go on the next reconcile")

	l.reconcile(ctx)
	drain()
	assert.Equal(t, 8, f.claimCount(), "no drop is probed twice")
	assert.ElementsMatch(t, []string{"d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8"}, f.claimedIDsCopy())
}

// Only Eligible rows with a Known=false observation, Required>0 and an
// active campaign qualify.
func TestLoop_CatchUpClaimProbe_SkipsNonCandidates(t *testing.T) {
	f, _, cfg := catchUpSetup(t, 3)
	f.progress = []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 0, Required: 60, Known: true}}
	f.camps = append(f.camps, platform.Campaign{ID: "c2", Platform: "twitch", Game: "G", Name: "Old", Status: "expired",
		AccountLinked: true, AccountLinkChecked: true,
		Benefits: []platform.DropBenefit{{ID: "e1", CampaignID: "c2", RequiredMinutes: 60}}})
	l, err := New(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, l.load(ctx))
	r := dropstate.Row{AccountID: "acc", DropID: "d2", CampaignID: "c1", Platform: "twitch", Status: dropstate.Accruing, Minutes: 5, Required: 120, Source: dropstate.FromPlatform}
	l.rows["d2"] = r
	l.reconcile(ctx)
	for i := 0; i < 10; i++ {
		l.catchUpOne(ctx)
	}
	assert.Equal(t, []string{"d3"}, f.claimedIDsCopy(), "d1 is Known, d2 is not Eligible (Accruing then not_enrolled), e1 is in an expired campaign")
}

// Probes are not sent inline: with an hour between probes, none is sent.
func TestLoop_CatchUpClaimProbe_SpacedNotInline(t *testing.T) {
	f, _, cfg := catchUpSetup(t, 3)
	cfg.CatchUpEvery = time.Hour
	run(t, cfg)
	require.Eventually(t, func() bool { return f.listCount() >= 1 }, 2*time.Second, 2*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, 0, f.claimCount())
}

// Kick keeps claimed rewards listed, so main.go leaves ClaimProbe off: no
// catch-up probes.
func TestLoop_CatchUpClaimProbe_KickOff(t *testing.T) {
	f, st, cfg := catchUpSetup(t, 3)
	cfg.Platform = "kick"
	cfg.ClaimProbe = false
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimAlready}
	run(t, cfg)
	require.Eventually(t, func() bool { return f.listCount() >= 1 }, 2*time.Second, 2*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, 0, f.claimCount())
	assert.Equal(t, dropstate.Eligible, st.get("d1").Status)
}
