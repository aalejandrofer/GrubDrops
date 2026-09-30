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
