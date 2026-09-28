package watcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/platformtest"
)

// claimFailBackend serves one campaign with two watch drops. "stuck" is
// already fully watched but its Claim always errors (Twitch returning null
// claimDropRewards); it stays in the in-progress inventory, complete and
// unclaimed, forever. "other" accrues normally and claims fine.
type claimFailBackend struct {
	*platformtest.MockBackend
	mu          sync.Mutex
	progress    map[string]int
	claimed     map[string]bool
	stuckClaims int
	// stuckFailN, when > 0, makes only the first stuckFailN "stuck" claims
	// fail (a transient failure); 0 = always fail.
	stuckFailN int
}

func newClaimFailBackend() *claimFailBackend {
	return &claimFailBackend{
		MockBackend: platformtest.New(),
		progress:    map[string]int{"stuck": 5, "other": 0},
		claimed:     map[string]bool{},
	}
}

func (b *claimFailBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "camp", Game: "Rust", Name: "Rust Camp", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{
			{ID: "stuck", CampaignID: "camp", Name: "Stuck", RequiredMinutes: 2},
			{ID: "other", CampaignID: "camp", Name: "Other", RequiredMinutes: 3},
		},
	}}, nil
}

func (b *claimFailBackend) InventoryProgress(_ context.Context, _ platform.Session) ([]platform.Progress, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]platform.Progress, 0, len(b.progress))
	for id, m := range b.progress {
		out = append(out, platform.Progress{BenefitID: id, MinutesWatched: m, Claimed: b.claimed[id]})
	}
	return out, nil
}

func (b *claimFailBackend) Heartbeat(_ context.Context, _ platform.WatchHandle) error {
	b.mu.Lock()
	b.progress["other"]++
	b.mu.Unlock()
	return nil
}

func (b *claimFailBackend) Claim(_ context.Context, _ platform.Session, drop platform.DropBenefit) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if drop.ID == "stuck" {
		b.stuckClaims++
		if b.stuckFailN == 0 || b.stuckClaims <= b.stuckFailN {
			return errors.New("claim: claimDropRewards null")
		}
	}
	b.claimed[drop.ID] = true
	return nil
}

func (b *claimFailBackend) snapshot() (stuckClaims int, otherClaimed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stuckClaims, b.claimed["other"]
}

// fieldNotifier records every notification with its fields.
type fieldNotifier struct {
	mu     sync.Mutex
	events []notifyRec
}

type notifyRec struct {
	ev     string
	fields map[string]any
}

func (n *fieldNotifier) Notify(_ context.Context, ev string, f map[string]any) error {
	n.mu.Lock()
	n.events = append(n.events, notifyRec{ev: ev, fields: f})
	n.mu.Unlock()
	return nil
}

// fullProgressFor counts "progress" notifications reporting 100% for drop.
func (n *fieldNotifier) fullProgressFor(drop string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := 0
	for _, e := range n.events {
		if e.ev != "progress" || e.fields["drop"] != drop {
			continue
		}
		cur, _ := e.fields["cur_min"].(int)
		req, _ := e.fields["req_min"].(int)
		if req > 0 && cur >= req {
			c++
		}
	}
	return c
}

// TestWatcher_UnclaimableCompletedDrop_SkippedAfterRepeatedClaimFailures:
// a completed drop whose Claim keeps failing must not be re-picked forever
// (starving every other drop and re-sending a "100%" notification each
// cycle). After claimFailSkipThreshold consecutive failures the watcher
// skips it (persisted via SkipRecorder), mines the other drop, and sends
// at most one 100% notification for the stuck drop. The skip must also
// survive the ghost-skip self-heal: the drop is still in the in-progress
// inventory (complete, unclaimed), which alone must not un-skip it.
func TestWatcher_UnclaimableCompletedDrop_SkippedAfterRepeatedClaimFailures(t *testing.T) {
	prev := stepErrBackoff
	stepErrBackoff = time.Millisecond
	defer func() { stepErrBackoff = prev }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const accID = "acc-claimfail"
	rec := newRecordingSkipRecorder()
	backend := newClaimFailBackend()
	notif := &fieldNotifier{}
	w := New(Config{
		AccountID:             accID,
		Backend:               backend,
		Session:               platform.Session{AccessToken: "tok"},
		Notifier:              notif,
		TickInterval:          2 * time.Millisecond,
		HeartbeatInterval:     2 * time.Millisecond,
		ProgressNotifyStepPct: 50,
		SkipRecorder:          rec.recordSkip,
	})
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	require.Eventually(t, func() bool {
		_, otherClaimed := backend.snapshot()
		return otherClaimed
	}, 4*time.Second, 2*time.Millisecond, "watcher never moved on to (and claimed) the other drop")

	// Let the watcher run a few more pick cycles: a self-healed skip would
	// re-pick "stuck" and claim it again.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}

	stuckClaims, _ := backend.snapshot()
	assert.Equal(t, claimFailSkipThreshold, stuckClaims, "stuck drop claimed exactly threshold times, then skipped")
	assert.True(t, rec.skips(accID)["stuck"], "skip must be persisted via SkipRecorder")
	w.mu.Lock()
	_, skipped := w.skippedBenefits["stuck"]
	w.mu.Unlock()
	assert.True(t, skipped, "stuck drop must remain skipped (self-heal must not undo it)")
	assert.LessOrEqual(t, notif.fullProgressFor("Stuck"), 1, "at most one 100% progress notification for the stuck drop")
}

// TestWatcher_TransientClaimFailure_NotSkipped: fewer than
// claimFailSkipThreshold consecutive failures followed by a success claims
// the drop normally, never skips it, and resets its failure counter.
func TestWatcher_TransientClaimFailure_NotSkipped(t *testing.T) {
	prev := stepErrBackoff
	stepErrBackoff = time.Millisecond
	defer func() { stepErrBackoff = prev }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rec := newRecordingSkipRecorder()
	backend := newClaimFailBackend()
	backend.stuckFailN = claimFailSkipThreshold - 1
	w := New(Config{
		AccountID:         "acc-flaky",
		Backend:           backend,
		Session:           platform.Session{AccessToken: "tok"},
		Notifier:          &fieldNotifier{},
		TickInterval:      2 * time.Millisecond,
		HeartbeatInterval: 2 * time.Millisecond,
		SkipRecorder:      rec.recordSkip,
	})
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	require.Eventually(t, func() bool {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		return backend.claimed["stuck"] && backend.claimed["other"]
	}, 4*time.Second, 2*time.Millisecond, "both drops should end up claimed")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}

	stuckClaims, _ := backend.snapshot()
	assert.Equal(t, claimFailSkipThreshold, stuckClaims, "two failures then one success")
	assert.Empty(t, rec.skips("acc-flaky"), "a transient failure must not skip the drop")
	w.mu.Lock()
	defer w.mu.Unlock()
	assert.Zero(t, w.claimFailures["stuck"], "counter resets on a successful claim")
	_, skipped := w.skippedBenefits["stuck"]
	assert.False(t, skipped)
}
