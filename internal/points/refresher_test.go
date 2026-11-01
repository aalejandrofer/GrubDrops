package points

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type env struct {
	t      *testing.T
	st     *store.PointsBalances
	force  *store.ForceWatch
	clock  time.Time
	mu     sync.Mutex
	reads  []Target
	sleeps []time.Duration
	answer map[Target]func() (int, error)
	r      *Refresher
	change int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	for _, id := range []string{"a1", "a2"} {
		_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
			ID: id, Platform: "twitch", DisplayName: id, Status: "idle",
			FingerprintJson: "{}", Enabled: 1, CreatedAt: 1, UpdatedAt: 1,
		})
		require.NoError(t, err)
	}
	e := &env{t: t, st: store.NewPointsBalances(q), force: store.NewForceWatch(db, q),
		clock: time.Unix(1_000_000, 0), answer: map[Target]func() (int, error){}}
	e.setForce(map[string][]string{"one": {"a1", "a2"}, "two": {"a1"}})
	e.r = &Refresher{
		Store: e.st,
		Targets: func(ctx context.Context) ([]Target, error) {
			list, err := e.force.List(ctx)
			if err != nil {
				return nil, err
			}
			var out []Target
			for _, c := range list {
				for _, a := range c.Accounts {
					out = append(out, Target{AccountID: a, Channel: c.Channel})
				}
			}
			return out, nil
		},
		Fetch: func(ctx context.Context, acct, ch string) (int, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			t := Target{acct, ch}
			e.reads = append(e.reads, t)
			if f, ok := e.answer[t]; ok {
				return f()
			}
			return 100, nil
		},
		OnChange: func() { e.change++ },
		Now:      func() time.Time { return e.clock },
		Sleep: func(ctx context.Context, d time.Duration) bool {
			e.sleeps = append(e.sleeps, d)
			return ctx.Err() == nil
		},
	}
	return e
}

// setForce assigns Twitch channels in name order: "one" then "two".
func (e *env) setForce(m map[string][]string) {
	e.t.Helper()
	var list []store.ForceChannel
	for _, ch := range []string{"one", "two"} {
		if accts, ok := m[ch]; ok {
			list = append(list, store.ForceChannel{Platform: "twitch", Channel: ch, Accounts: accts})
		}
	}
	require.NoError(e.t, e.force.Set(context.Background(), list))
}

func (e *env) pass() []Target {
	e.mu.Lock()
	e.reads = nil
	e.mu.Unlock()
	e.r.Pass(context.Background())
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Target(nil), e.reads...)
}

func (e *env) row(acct, ch string) store.PointsBalance {
	e.t.Helper()
	b, ok, err := e.st.Get(context.Background(), acct, ch)
	require.NoError(e.t, err)
	require.True(e.t, ok, "%s/%s cached", acct, ch)
	return b
}

func TestPass_ReadsAllThenHourly(t *testing.T) {
	e := newEnv(t)
	got := e.pass()
	// One account at a time, each in channel order.
	assert.Equal(t, []Target{{"a1", "one"}, {"a1", "two"}, {"a2", "one"}}, got)
	assert.Equal(t, []time.Duration{DefaultSpacing, DefaultSpacing}, e.sleeps, "spaced, no pause before the first")
	assert.Equal(t, 1, e.change)
	b := e.row("a1", "one")
	assert.Equal(t, int64(100), *b.Balance)
	assert.Equal(t, e.clock, b.FetchedAt)

	e.clock = e.clock.Add(59 * time.Minute)
	assert.Empty(t, e.pass(), "fresh for the hour")
	e.clock = e.clock.Add(time.Minute)
	assert.Len(t, e.pass(), 3, "due after an hour")
	assert.Equal(t, 1, e.change, "same balances: no change event")
}

func TestPass_FailureRetriesSoonerWithCappedBackoff(t *testing.T) {
	e := newEnv(t)
	e.setForce(map[string][]string{"one": {"a1"}})
	fail := true
	e.answer[Target{"a1", "one"}] = func() (int, error) {
		if fail {
			return 0, errors.New("gql 500")
		}
		return 7, nil
	}
	start := e.clock
	require.Len(t, e.pass(), 1)
	b := e.row("a1", "one")
	assert.Equal(t, store.PointsErrFailed, b.Error)
	assert.Equal(t, 1, b.Failures)
	assert.Nil(t, b.Balance)

	// Retry waits 5m, 10m, 20m, then holds at 30m: always under the hour.
	for i, wait := range []time.Duration{5, 10, 20, 30, 30} {
		e.clock = e.clock.Add(wait*time.Minute - time.Second)
		require.Empty(t, e.pass(), "retry %d too early", i)
		e.clock = e.clock.Add(time.Second)
		require.Len(t, e.pass(), 1, "retry %d due", i)
		assert.Equal(t, i+2, e.row("a1", "one").Failures)
	}
	assert.Equal(t, 95*time.Minute, e.clock.Sub(start))

	fail = false
	e.clock = e.clock.Add(30 * time.Minute)
	require.Len(t, e.pass(), 1)
	b = e.row("a1", "one")
	assert.Empty(t, b.Error)
	assert.Zero(t, b.Failures)
	assert.Equal(t, int64(7), *b.Balance)
	e.clock = e.clock.Add(30 * time.Minute)
	assert.Empty(t, e.pass(), "back on the hourly cadence")
}

func TestPass_FailureKeepsLastGoodBalance(t *testing.T) {
	e := newEnv(t)
	e.setForce(map[string][]string{"one": {"a1"}})
	e.pass()
	e.answer[Target{"a1", "one"}] = func() (int, error) { return 0, errors.New("timeout") }
	e.clock = e.clock.Add(time.Hour)
	before := e.change
	e.pass()
	b := e.row("a1", "one")
	assert.Equal(t, int64(100), *b.Balance)
	assert.Equal(t, store.PointsErrFailed, b.Error)
	assert.Equal(t, before+1, e.change, "error state change is an event")
}

func TestPass_NoBalanceIsUnavailableAndHourly(t *testing.T) {
	e := newEnv(t)
	e.setForce(map[string][]string{"one": {"a1"}})
	e.answer[Target{"a1", "one"}] = func() (int, error) { return 0, platform.ErrNoPointsBalance }
	e.pass()
	b := e.row("a1", "one")
	assert.Equal(t, store.PointsErrUnavailable, b.Error)
	assert.Zero(t, b.Failures)
	e.clock = e.clock.Add(30 * time.Minute)
	assert.Empty(t, e.pass(), "no fast retry for a channel without points")
	e.clock = e.clock.Add(30 * time.Minute)
	assert.Len(t, e.pass(), 1)
}

func TestRecord_ClaimLoopBalanceDefersHourlyRead(t *testing.T) {
	e := newEnv(t)
	e.setForce(map[string][]string{"one": {"a1"}})
	e.pass()
	e.clock = e.clock.Add(50 * time.Minute)
	changed, err := Record(context.Background(), e.st, "a1", "one", 150, e.clock)
	require.NoError(t, err)
	assert.True(t, changed)
	b := e.row("a1", "one")
	assert.Equal(t, int64(150), *b.Balance)
	assert.Equal(t, e.clock, b.FetchedAt)

	changed, err = Record(context.Background(), e.st, "a1", "one", 150, e.clock)
	require.NoError(t, err)
	assert.False(t, changed, "same balance")

	e.clock = e.clock.Add(30 * time.Minute) // 80m after the pass read
	assert.Empty(t, e.pass(), "the claim loop's balance counts as a read")
	e.clock = e.clock.Add(30 * time.Minute)
	assert.Len(t, e.pass(), 1)
}

func TestRecord_ClearsFailure(t *testing.T) {
	e := newEnv(t)
	e.setForce(map[string][]string{"one": {"a1"}})
	e.answer[Target{"a1", "one"}] = func() (int, error) { return 0, errors.New("boom") }
	e.pass()
	changed, err := Record(context.Background(), e.st, "a1", "one", 100, e.clock)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Empty(t, e.row("a1", "one").Error)
}

func TestPass_StopsReadingRemovedPairs(t *testing.T) {
	e := newEnv(t)
	e.pass()
	e.setForce(map[string][]string{"one": {"a1"}})
	e.clock = e.clock.Add(time.Hour)
	before := e.change
	assert.Equal(t, []Target{{"a1", "one"}}, e.pass())
	list, err := e.st.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1, "unassigned rows pruned")
	assert.Equal(t, before+1, e.change, "a prune is a change")
}

func TestPass_CancelStopsBetweenReads(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.r.Sleep = func(ctx context.Context, d time.Duration) bool {
		cancel()
		return false
	}
	assert.Equal(t, 1, e.r.Pass(ctx))
}

func TestBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{0: 5, 1: 5, 2: 10, 3: 20, 4: 30, 9: 30} {
		assert.Equal(t, want*time.Minute, Backoff(n, 5*time.Minute, 30*time.Minute), "failures=%d", n)
	}
}

func TestRun_PassesOnTick(t *testing.T) {
	e := newEnv(t)
	e.setForce(map[string][]string{"one": {"a1"}})
	e.r.Tick = 5 * time.Millisecond
	e.r.Every = time.Nanosecond // every pass finds the pair due
	e.r.Now = time.Now
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.r.Run(ctx); close(done) }()
	require.Eventually(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.reads) >= 2
	}, 2*time.Second, 2*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRefreshNow_MakesEveryPairDue(t *testing.T) {
	e := newEnv(t)
	e.pass()
	e.clock = e.clock.Add(10 * time.Minute)
	require.Empty(t, e.pass(), "fresh for the hour")
	before := e.change

	ok, wait := e.r.RefreshNow()
	require.True(t, ok)
	assert.Zero(t, wait)
	got := e.pass()
	assert.Equal(t, []Target{{"a1", "one"}, {"a1", "two"}, {"a2", "one"}}, got, "every pair, one account at a time")
	assert.Equal(t, before+1, e.change, "a forced read is an event even with the same balances")
	assert.Empty(t, e.pass(), "read once, then hourly again")
}

func TestRefreshNow_Cooldown(t *testing.T) {
	e := newEnv(t)
	ok, _ := e.r.RefreshNow()
	require.True(t, ok)
	e.clock = e.clock.Add(20 * time.Second)
	ok, wait := e.r.RefreshNow()
	assert.False(t, ok, "inside the cooldown")
	assert.Equal(t, 40*time.Second, wait)
	e.clock = e.clock.Add(40 * time.Second)
	ok, wait = e.r.RefreshNow()
	assert.True(t, ok, "cooldown over")
	assert.Zero(t, wait)
	assert.Len(t, e.r.wakeC(), 1, "a second wake while one is pending does not block or queue")
}

func TestRefreshNow_CoalescesWithPassInFlight(t *testing.T) {
	e := newEnv(t)
	e.answer[Target{"a2", "one"}] = func() (int, error) { return 0, errors.New("gql 500") }
	e.pass()
	// a2/one retries at +5m; the other two are fresh.
	e.clock = e.clock.Add(5 * time.Minute)
	var started bool
	e.answer[Target{"a2", "one"}] = func() (int, error) {
		started, _ = e.r.RefreshNow() // clicked while this pass reads
		return 9, nil
	}
	assert.Equal(t, []Target{{"a2", "one"}}, e.pass(), "the tick pass had passed the fresh pairs")
	require.True(t, started)
	assert.Len(t, e.r.wakeC(), 1, "one pending wake")

	assert.Equal(t, []Target{{"a1", "one"}, {"a1", "two"}}, e.pass(),
		"the woken pass skips a2/one, read after the click")
	assert.Empty(t, e.pass())
}

func TestRun_WakesOnRefreshNow(t *testing.T) {
	e := newEnv(t)
	var cmu sync.Mutex
	clock := e.clock
	e.r.Now = func() time.Time { cmu.Lock(); defer cmu.Unlock(); return clock }
	e.r.Tick = time.Hour // only RefreshNow can start the second pass
	reads := func() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.reads) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.r.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { return reads() == 3 }, 2*time.Second, time.Millisecond)

	cmu.Lock()
	clock = clock.Add(time.Minute)
	cmu.Unlock()
	ok, _ := e.r.RefreshNow()
	require.True(t, ok)
	require.Eventually(t, func() bool { return reads() == 6 }, 2*time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}
