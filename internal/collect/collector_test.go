package collect

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

var cscanAt = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func newCollector(t *testing.T) *Collector {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "a1", Platform: "twitch", DisplayName: "a1", Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	return New(db, q)
}

func ctarget(id string, required, minutes int) plan.Target {
	return plan.Target{
		Campaign: platform.Campaign{ID: "c1", Platform: "twitch", Game: "Rust", Name: "Camp", Status: "active"},
		Benefit:  platform.DropBenefit{ID: id, CampaignID: "c1", Name: "Drop " + id, RequiredMinutes: required},
		Minutes:  minutes,
	}
}

func ccampaignOf(ts ...plan.Target) platform.Campaign {
	c := ts[0].Campaign
	for _, t := range ts {
		c.Benefits = append(c.Benefits, t.Benefit)
	}
	return c
}

// baseScan queues b1 at 30/60 and lists b2 as claimed.
func cbaseScan() runner.Scan {
	q, cl := ctarget("b1", 60, 30), ctarget("b2", 60, 60)
	return runner.Scan{
		AccountID: "a1", Platform: "twitch", At: cscanAt, InventoryOK: true,
		Campaigns: []platform.Campaign{ccampaignOf(q, cl)},
		Result:    plan.Result{Queue: []plan.Target{q}, Claimed: []plan.Target{cl}},
	}
}

func statesByBenefit(t *testing.T, c *Collector) map[string]store.DropState {
	t.Helper()
	rows, err := c.States.List(context.Background(), "a1")
	require.NoError(t, err)
	out := map[string]store.DropState{}
	for _, r := range rows {
		out[r.BenefitID] = r
	}
	return out
}

func TestCollector_ScanPersistsStates(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	st := statesByBenefit(t, c)
	require.Len(t, st, 2)
	require.Equal(t, StateAccruing, st["b1"].State)
	require.Equal(t, 30, st["b1"].Minutes)
	require.Equal(t, StateClaimed, st["b2"].State)
	scans, err := c.Scans.List(ctx)
	require.NoError(t, err)
	require.Len(t, scans, 1)
	require.True(t, scans[0].ScannedAt.Equal(cscanAt))
	require.True(t, scans[0].ReconciledAt.Equal(cscanAt))
	require.Empty(t, scans[0].Error)
}

func TestCollector_InventoryFailureKeepsStates(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	later := cscanAt.Add(time.Hour)
	s := cbaseScan()
	s.At, s.InventoryOK, s.InventoryErr = later, false, "inventory down"
	s.Result = plan.Result{Queue: []plan.Target{ctarget("b1", 60, 0), ctarget("b2", 60, 0)}}
	c.OnScan(ctx, s)
	st := statesByBenefit(t, c)
	require.Equal(t, StateAccruing, st["b1"].State)
	require.Equal(t, StateClaimed, st["b2"].State)
	scans, err := c.Scans.List(ctx)
	require.NoError(t, err)
	require.True(t, scans[0].ScannedAt.Equal(later))
	require.True(t, scans[0].ReconciledAt.Equal(cscanAt))
	require.Equal(t, "inventory: inventory down", scans[0].Error)
}

func TestCollector_ReplaceFailureStillRecordsScan(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	_, err := c.States.DB.ExecContext(ctx, `CREATE TRIGGER ds_fail BEFORE INSERT ON drop_states BEGIN SELECT RAISE(ABORT, 'nope'); END`)
	require.NoError(t, err)
	later := cscanAt.Add(time.Hour)
	s := cbaseScan()
	s.At = later
	c.OnScan(ctx, s)
	scans, err := c.Scans.List(ctx)
	require.NoError(t, err)
	require.Len(t, scans, 1)
	require.True(t, scans[0].ScannedAt.Equal(later))
	require.True(t, scans[0].ReconciledAt.Equal(cscanAt))
	require.Contains(t, scans[0].Error, "drop states: ")
	require.Len(t, statesByBenefit(t, c), 2)
}

func TestCollector_Forget(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	c.Forget("a1")
	g := c.Board.Games([]plan.Game{{Name: "Rust", Slug: "rust"}})
	require.Empty(t, g[0].Campaigns)
	// the cache is gone too, so progress writes nothing
	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 50}})
	require.Equal(t, 30, statesByBenefit(t, c)["b1"].Minutes)
}

func TestCollector_NoCampaignsClearsStates(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	c.OnScan(ctx, runner.Scan{AccountID: "a1", Platform: "twitch", At: cscanAt.Add(time.Hour)})
	require.Empty(t, statesByBenefit(t, c))
	// the cache is cleared too, so progress writes nothing
	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 40}})
	require.Empty(t, statesByBenefit(t, c))
}

func TestCollector_FoundClaimLogged(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	s := cbaseScan()
	s.Progress = []platform.Progress{{BenefitID: "b2", Claimed: true}}
	c.OnScan(ctx, s)
	got, err := c.Claims.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "b2", got[0].BenefitID)
	require.Equal(t, store.ClaimSourceInventory, got[0].Source)

	s.Logged = map[string]bool{"b2": true}
	c.OnScan(ctx, s)
	got, err = c.Claims.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, store.ClaimSourceInventory, got[0].Source)
}

func TestCollector_OnFound(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	var got []store.ClaimEntry
	c.OnFound = func(_ context.Context, e store.ClaimEntry) { got = append(got, e) }
	s := cbaseScan()
	s.Progress = []platform.Progress{{BenefitID: "b2", Claimed: true}}
	c.OnScan(ctx, s)
	require.Len(t, got, 1)
	require.Equal(t, "b2", got[0].BenefitID)
	require.Equal(t, "a1", got[0].AccountID)

	s.Logged = map[string]bool{"b2": true}
	c.OnScan(ctx, s)
	require.Len(t, got, 1)

	c.OnFound = nil
	s.Logged = nil
	c.OnScan(ctx, s) // nil hook is a no-op
}

func TestCollector_ScanErrorRecorded(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	later := cscanAt.Add(time.Hour)
	c.OnScan(ctx, runner.Scan{AccountID: "a1", Platform: "twitch", At: later, Err: errors.New("boom")})
	scans, err := c.Scans.List(ctx)
	require.NoError(t, err)
	require.Equal(t, "boom", scans[0].Error)
	require.True(t, scans[0].ScannedAt.Equal(later))
	require.True(t, scans[0].ReconciledAt.Equal(cscanAt))
	require.Len(t, statesByBenefit(t, c), 2)
	g := c.Board.Games([]plan.Game{{Name: "Rust", Slug: "rust"}})
	require.Len(t, g[0].Campaigns, 1)
	require.True(t, g[0].LastScanAt.Equal(cscanAt))
}

func TestCollector_ProgressAdvancesState(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	s := cbaseScan()
	s.Result.Queue = []plan.Target{ctarget("b1", 60, 0)}
	c.OnScan(ctx, s)
	require.Equal(t, StateEligible, statesByBenefit(t, c)["b1"].State)

	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 30}, {BenefitID: "zzz", MinutesWatched: 5}})
	st := statesByBenefit(t, c)
	require.Equal(t, StateAccruing, st["b1"].State)
	require.Equal(t, 30, st["b1"].Minutes)
	require.Len(t, st, 2)

	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 60}})
	require.Equal(t, StateClaimable, statesByBenefit(t, c)["b1"].State)

	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 60, Claimed: true}})
	st = statesByBenefit(t, c)
	require.Equal(t, StateClaimed, st["b1"].State)
	require.Equal(t, 60, st["b1"].Minutes)

	// claimed rows stay claimed
	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 10}})
	require.Equal(t, StateClaimed, statesByBenefit(t, c)["b1"].State)
}

func TestCollector_ProgressLeavesBlocked(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	s := cbaseScan()
	s.Result = plan.Result{Blocked: []plan.Blocked{{Target: ctarget("b1", 60, 0), Reason: plan.ReasonNotLinked}}}
	c.OnScan(ctx, s)
	c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: 40}})
	st := statesByBenefit(t, c)["b1"]
	require.Equal(t, StateBlocked, st.State)
	require.Equal(t, 0, st.Minutes)
}

func TestCollector_BeatUsesLocationDay(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	sh, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	c.Now = func() time.Time { return time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC) }
	c.Loc = func() *time.Location { return sh }
	c.OnBeat(ctx, "a1")
	n, err := c.Minutes.Day(ctx, "2026-10-26")
	require.NoError(t, err)
	require.Equal(t, 1, n)
	c.OnBeat(ctx, "a1")
	c.OnBeat(ctx, "a1")
	n, err = c.Minutes.Day(ctx, "2026-10-26")
	require.NoError(t, err)
	require.Equal(t, 3, n)
}

func TestCollector_RecordClaimFlipsState(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	s := cbaseScan()
	s.Result.Queue = []plan.Target{ctarget("b1", 60, 60)}
	c.OnScan(ctx, s)
	require.Equal(t, StateClaimable, statesByBenefit(t, c)["b1"].State)
	require.NoError(t, c.RecordClaim(ctx, "a1", ctarget("b1", 60, 60), "CODE"))
	got, err := c.Claims.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, store.ClaimSourceGrubDrops, got[0].Source)
	require.Equal(t, "Rust", got[0].Game)
	require.Equal(t, "CODE", got[0].Code)
	require.Equal(t, StateClaimed, statesByBenefit(t, c)["b1"].State)
	ids, err := c.ClaimedBenefitIDs(ctx, "a1")
	require.NoError(t, err)
	require.True(t, ids["b1"])
}

func TestCollector_ConcurrentHooks(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	c.OnScan(ctx, cbaseScan())
	now := time.Now().Unix()
	_, err := c.Scans.Q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "a2", Platform: "twitch", DisplayName: "a2", Status: "idle",
		FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	const workers, n = 4, 50
	var wg sync.WaitGroup
	for _, acc := range []string{"a1", "a2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				s := cbaseScan()
				s.AccountID = acc
				s.At = cscanAt.Add(time.Duration(i) * time.Second)
				c.OnScan(ctx, s)
				_ = c.Board.Games([]plan.Game{{Name: "Rust", Slug: "rust"}})
			}
		}()
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				c.OnProgress(ctx, "a1", []platform.Progress{{BenefitID: "b1", MinutesWatched: i}})
				c.OnBeat(ctx, "a1")
				_ = c.RecordClaim(ctx, "a1", ctarget("b1", 60, 60), "")
			}
		}()
	}
	wg.Wait()
	total, err := c.Minutes.Total(ctx)
	require.NoError(t, err)
	require.Equal(t, workers*n, total)
}

func TestCollector_ScanStoresTwitchGameID(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	q := gen.New(c.States.DB)
	_, err := c.States.DB.Exec(`DELETE FROM games`) // migrations seed a Rust row
	require.NoError(t, err)
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "rust", Name: "Rust", Slug: "rust"}))
	require.NoError(t, c.Chosen.Set(ctx, []string{"rust"}))
	twitchID := func() string {
		var id string
		require.NoError(t, c.States.DB.QueryRow(`SELECT twitch_game_id FROM games WHERE id='rust'`).Scan(&id))
		return id
	}

	k := cbaseScan()
	k.Platform = "kick"
	k.Campaigns[0].Platform, k.Campaigns[0].GameID = "kick", "999"
	c.OnScan(ctx, k)
	require.Equal(t, "", twitchID())

	s := cbaseScan()
	s.Campaigns[0].GameID = "263490"
	c.OnScan(ctx, s)
	require.Equal(t, "263490", twitchID())
}

// A chosen game no Twitch campaign names (added from Kick, or no campaign
// yet) gets its Twitch id looked up by name, so box art can use the id form.
func TestCollector_LooksUpMissingTwitchGameIDs(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	q := gen.New(c.States.DB)
	_, err := c.States.DB.Exec(`DELETE FROM games`)
	require.NoError(t, err)
	for _, g := range []gen.UpsertGameParams{
		{ID: "rust", Name: "Rust", Slug: "rust"},
		{ID: "bf6", Name: "Battlefield 6", Slug: "battlefield-6"},
		{ID: "ko", Name: "Kick Only", Slug: "kick-only"},
	} {
		require.NoError(t, q.UpsertGame(ctx, g))
	}
	require.NoError(t, c.Chosen.Set(ctx, []string{"rust", "bf6", "ko"}))
	now := cscanAt
	c.Now = func() time.Time { return now }
	var mu sync.Mutex
	var asked []string
	c.LookupTwitchGameID = func(_ context.Context, name string) (string, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, name)
		if name == "Battlefield 6" {
			return "168648543", true, nil
		}
		return "", false, nil
	}
	twitchID := func(id string) string {
		var v string
		require.NoError(t, c.States.DB.QueryRow(`SELECT twitch_game_id FROM games WHERE id=?`, id).Scan(&v))
		return v
	}

	s := cbaseScan()
	s.Campaigns[0].GameID = "263490"
	c.OnScan(ctx, s)
	c.lookups.Wait()
	require.Equal(t, "263490", twitchID("rust"))
	require.Equal(t, "168648543", twitchID("bf6"))
	require.ElementsMatch(t, []string{"Battlefield 6", "Kick Only"}, asked, "a campaign-named game needs no lookup")

	asked = nil
	c.OnScan(ctx, s)
	c.lookups.Wait()
	require.Empty(t, asked, "a miss is not retried until twitchIDRetry passes")

	now = now.Add(twitchIDRetry + time.Minute)
	c.OnScan(ctx, s)
	c.lookups.Wait()
	require.Equal(t, []string{"Kick Only"}, asked)

	k := cbaseScan()
	k.Platform = "kick"
	k.Campaigns[0].Platform = "kick"
	asked = nil
	now = now.Add(twitchIDRetry + time.Minute)
	c.OnScan(ctx, k)
	c.lookups.Wait()
	require.Empty(t, asked, "only Twitch scans trigger lookups")
}

// A failed lookup (no signed-in Twitch session yet) retries on the next scan.
func TestCollector_TwitchGameIDLookupErrorRetries(t *testing.T) {
	c := newCollector(t)
	ctx := context.Background()
	q := gen.New(c.States.DB)
	_, err := c.States.DB.Exec(`DELETE FROM games`)
	require.NoError(t, err)
	require.NoError(t, q.UpsertGame(ctx, gen.UpsertGameParams{ID: "bf6", Name: "Battlefield 6", Slug: "battlefield-6"}))
	require.NoError(t, c.Chosen.Set(ctx, []string{"bf6"}))
	calls := 0
	c.LookupTwitchGameID = func(context.Context, string) (string, bool, error) {
		calls++
		return "", false, errors.New("no session")
	}
	s := cbaseScan()
	s.Campaigns = nil
	c.OnScan(ctx, s)
	c.lookups.Wait()
	c.OnScan(ctx, s)
	c.lookups.Wait()
	require.Equal(t, 2, calls)
}
