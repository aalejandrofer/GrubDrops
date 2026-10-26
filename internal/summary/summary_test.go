package summary

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestNextMidnight_DST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	got := NextMidnight(time.Date(2026, 3, 8, 1, 30, 0, 0, ny), ny)
	assert.Equal(t, time.Date(2026, 3, 9, 0, 0, 0, 0, ny), got)
	assert.Equal(t, "2026-03-09T00:00:00-04:00", got.Format(time.RFC3339))
}

type sent struct {
	event  string
	fields map[string]any
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeNotifier) Notify(_ context.Context, e notify.Event, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{e, fields})
	return nil
}

func (f *fakeNotifier) all() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.sent...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

// waitFor polls cond until it holds or 2s pass.
func waitFor(cond func() bool) {
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end) && !cond(); {
		time.Sleep(5 * time.Millisecond)
	}
}

func runSummary(t *testing.T, enabled bool) []sent {
	t.Helper()
	old := maxWait
	maxWait = 5 * time.Millisecond
	t.Cleanup(func() { maxWait = old })

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	settings := store.NewSettings(q)
	minutes := store.NewWatchMinutes(q)
	claims := store.NewClaimLog(q)
	require.NoError(t, settings.SetNotifyDailySummary(ctx, enabled))

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	require.NoError(t, minutes.Add(ctx, "a1", "2026-05-10", 42))
	require.NoError(t, minutes.Add(ctx, "a2", "2026-05-10", 8))
	require.NoError(t, minutes.Add(ctx, "a1", "2026-05-11", 99))
	rec := func(id string, at time.Time) {
		require.NoError(t, claims.Record(ctx, store.ClaimEntry{AccountID: "a1", BenefitID: id, ClaimedAt: at}))
	}
	rec("b1", time.Date(2026, 5, 10, 9, 0, 0, 0, loc))
	rec("b2", time.Date(2026, 5, 10, 23, 59, 0, 0, loc))
	rec("b3", time.Date(2026, 5, 9, 23, 0, 0, 0, loc))
	rec("b4", time.Date(2026, 5, 11, 0, 30, 0, 0, loc))

	clk := &clock{t: time.Date(2026, 5, 10, 23, 59, 59, 0, loc)}
	n := &fakeNotifier{}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(rctx, Config{Settings: settings, Minutes: minutes, Claims: claims, Notifier: n,
			Zone: func() *time.Location { return loc }, Now: clk.now})
	}()
	// Let the loop spin a few cycles before midnight, then cross it.
	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, n.all(), "nothing before midnight")
	clk.set(time.Date(2026, 5, 11, 0, 0, 1, 0, loc))
	waitFor(func() bool { return len(n.all()) > 0 })
	if !enabled {
		// Off case: give a wrongly-sent event time to show up.
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
	return n.all()
}

func TestSummary_SendsYesterday(t *testing.T) {
	got := runSummary(t, true)
	require.Len(t, got, 1)
	assert.Equal(t, notify.EventSummary, got[0].event)
	assert.Equal(t, "2026-05-10", got[0].fields["day"])
	assert.Equal(t, 50, got[0].fields["minutes"])
	assert.Equal(t, 2, got[0].fields["claims"])
	assert.Equal(t, "2026-05-10: 50 min watched, 2 drops claimed.", got[0].fields["msg"])
}

func TestSummary_ToggleOffSendsNothing(t *testing.T) {
	assert.Empty(t, runSummary(t, false))
}
