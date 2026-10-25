package collect

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Collector ties the runner hooks and claim log to the store and the Board.
// Every method is safe for concurrent use.
type Collector struct {
	Claims  *store.ClaimLog
	States  *store.DropStates
	Minutes *store.WatchMinutes
	Scans   *store.AccountScans
	Board   *Board
	Now     func() time.Time      // nil = time.Now
	Loc     func() *time.Location // nil = time.Local; picks the minutes day

	mu   sync.Mutex
	last map[string]map[string]store.DropState // account -> benefit -> state
}

var _ runner.ClaimLog = (*Collector)(nil)

func New(db *sql.DB, q *gen.Queries) *Collector {
	return &Collector{
		Claims:  store.NewClaimLog(q),
		States:  store.NewDropStates(db, q),
		Minutes: store.NewWatchMinutes(q),
		Scans:   store.NewAccountScans(q),
		Board:   NewBoard(),
		last:    map[string]map[string]store.DropState{},
	}
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Collector) logErr(msg, account string, err error) {
	slog.Error(msg, "kind", "error", "account", account, "err", err)
}

// OnScan persists one scan. The scan is read-only and shared with the runner.
func (c *Collector) OnScan(ctx context.Context, s runner.Scan) {
	c.Board.Observe(s)
	if s.Err != nil {
		if err := c.Scans.Put(ctx, store.AccountScan{AccountID: s.AccountID, ScannedAt: s.At, Error: s.Err.Error()}); err != nil {
			c.logErr("collect: record scan error failed", s.AccountID, err)
		}
		return
	}
	now := c.now()
	for _, e := range Found(s, now) {
		if err := c.Claims.Record(ctx, e); err != nil {
			c.logErr("collect: record found claim failed", s.AccountID, err)
		}
	}
	scan := store.AccountScan{AccountID: s.AccountID, ScannedAt: s.At}
	if s.InventoryOK || len(s.Campaigns) == 0 {
		rows := Derive(s, now)
		c.mu.Lock()
		if err := c.States.Replace(ctx, s.AccountID, rows); err != nil {
			c.mu.Unlock()
			c.logErr("collect: replace drop states failed", s.AccountID, err)
			return
		}
		cache := make(map[string]store.DropState, len(rows))
		for _, r := range rows {
			cache[r.BenefitID] = r
		}
		c.last[s.AccountID] = cache
		c.mu.Unlock()
		scan.ReconciledAt = s.At
	}
	if err := c.Scans.Put(ctx, scan); err != nil {
		c.logErr("collect: record scan failed", s.AccountID, err)
	}
}

// OnProgress advances cached drop states from fresh inventory progress.
func (c *Collector) OnProgress(ctx context.Context, accountID string, progress []platform.Progress) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	cache := c.last[accountID]
	for _, p := range progress {
		row, ok := cache[p.BenefitID]
		if !ok || row.State == StateBlocked || row.State == StateClaimed {
			continue
		}
		next := row
		next.Minutes = p.MinutesWatched
		next.State = StateFor(p.MinutesWatched, row.RequiredMinutes)
		if p.Claimed {
			next.State, next.Minutes = StateClaimed, row.RequiredMinutes
		}
		if next.State == row.State && next.Minutes == row.Minutes {
			continue
		}
		next.UpdatedAt = now
		if err := c.States.Upsert(ctx, next); err != nil {
			c.logErr("collect: update drop state failed", accountID, err)
			continue
		}
		cache[p.BenefitID] = next
	}
}

// OnBeat counts one watched minute on the day in the configured location.
func (c *Collector) OnBeat(ctx context.Context, accountID string) {
	loc := time.Local
	if c.Loc != nil {
		if l := c.Loc(); l != nil {
			loc = l
		}
	}
	day := c.now().In(loc).Format("2006-01-02")
	if err := c.Minutes.Add(ctx, accountID, day, 1); err != nil {
		c.logErr("collect: add watch minute failed", accountID, err)
	}
}

// RecordClaim logs a claim and flips its drop state so the UI updates before
// the next scan.
func (c *Collector) RecordClaim(ctx context.Context, accountID string, t plan.Target, code string) error {
	now := c.now()
	err := c.Claims.Record(ctx, store.ClaimEntry{
		AccountID: accountID, Platform: t.Campaign.Platform, Game: t.Campaign.Game,
		CampaignID: t.Campaign.ID, CampaignName: t.Campaign.Name,
		BenefitID: t.Benefit.ID, BenefitName: t.Benefit.Name, ImageURL: t.Benefit.ImageURL,
		Code: code, Source: store.ClaimSourceGrubDrops, ClaimedAt: now,
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	row, ok := c.last[accountID][t.Benefit.ID]
	if !ok || row.State == StateClaimed {
		return nil
	}
	row.State, row.Minutes, row.UpdatedAt = StateClaimed, row.RequiredMinutes, now
	if err := c.States.Upsert(ctx, row); err != nil {
		c.logErr("collect: flip claimed state failed", accountID, err)
		return nil
	}
	c.last[accountID][t.Benefit.ID] = row
	return nil
}

func (c *Collector) ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error) {
	return c.Claims.ClaimedBenefitIDs(ctx, accountID)
}
