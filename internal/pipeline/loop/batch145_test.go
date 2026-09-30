package loop

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
