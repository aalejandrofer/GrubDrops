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
	Chosen  *store.ChosenGames
	Board   *Board
	Now     func() time.Time      // nil = time.Now
	Loc     func() *time.Location // nil = time.Local; picks the minutes day
	// OnFound is called after each inventory-found claim OnScan records. nil
	// = no-op. Set it before the first scan.
	OnFound func(ctx context.Context, e store.ClaimEntry)
	// LookupTwitchGameID finds a game's Twitch id by name, for chosen games
	// no Twitch campaign has named yet. nil = no lookup. Set it before the
	// first scan.
	LookupTwitchGameID func(ctx context.Context, name string) (id string, ok bool, err error)

	mu   sync.Mutex
	last map[string]map[string]store.DropState // account -> benefit -> state

	idMu    sync.Mutex
	idTried map[string]time.Time // game row id -> last lookup, while one is due or running
	lookups sync.WaitGroup
}

// twitchIDRetry spaces lookups of a game Twitch did not know, such as a
// Kick-only category.
const twitchIDRetry = 6 * time.Hour

// twitchIDTimeout bounds one batch of Twitch game id lookups.
const twitchIDTimeout = 30 * time.Second

var _ runner.ClaimLog = (*Collector)(nil)

func New(db *sql.DB, q *gen.Queries) *Collector {
	return &Collector{
		Claims:  store.NewClaimLog(q),
		States:  store.NewDropStates(db, q),
		Minutes: store.NewWatchMinutes(q),
		Scans:   store.NewAccountScans(q),
		Chosen:  store.NewChosenGames(db, q),
		Board:   NewBoard(),
		last:    map[string]map[string]store.DropState{},
		idTried: map[string]time.Time{},
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
	c.storeTwitchGameIDs(ctx, s)
	for _, e := range Found(s, now) {
		if err := c.Claims.Record(ctx, e); err != nil {
			c.logErr("collect: record found claim failed", s.AccountID, err)
			continue
		}
		if c.OnFound != nil {
			c.OnFound(ctx, e)
		}
	}
	scan := store.AccountScan{AccountID: s.AccountID, ScannedAt: s.At}
	if !s.InventoryOK && s.InventoryErr != "" {
		scan.Error = "inventory: " + s.InventoryErr
	}
	if s.InventoryOK || len(s.Campaigns) == 0 {
		rows := Derive(s, now)
		c.mu.Lock()
		if err := c.States.Replace(ctx, s.AccountID, rows); err != nil {
			c.mu.Unlock()
			c.logErr("collect: replace drop states failed", s.AccountID, err)
			// Old rows stay, so ReconciledAt stays too.
			scan.Error = "drop states: " + err.Error()
			if err := c.Scans.Put(ctx, scan); err != nil {
				c.logErr("collect: record scan failed", s.AccountID, err)
			}
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

// storeTwitchGameIDs saves each Twitch campaign's game ID onto the matching
// chosen game, so box art can use the ID form. Unchanged IDs are no-op writes.
func (c *Collector) storeTwitchGameIDs(ctx context.Context, s runner.Scan) {
	if s.Platform != "twitch" || c.Chosen == nil {
		return
	}
	chosen, err := c.Chosen.List(ctx)
	if err != nil {
		c.logErr("collect: list chosen games failed", s.AccountID, err)
		return
	}
	games := make([]plan.Game, len(chosen))
	for i, g := range chosen {
		games[i] = plan.Game{Name: g.Name, Slug: g.Slug}
	}
	named := map[int]bool{}
	for _, camp := range s.Campaigns {
		if camp.Platform != "twitch" || camp.GameID == "" {
			continue
		}
		i, ok := plan.Match(games, camp.Game)
		if !ok {
			continue
		}
		named[i] = true
		if err := c.Chosen.Q.SetGameTwitchID(ctx, gen.SetGameTwitchIDParams{TwitchGameID: camp.GameID, ID: chosen[i].ID}); err != nil {
			c.logErr("collect: store twitch game id failed", s.AccountID, err)
		}
	}
	var missing []store.ChosenGame
	for i, g := range chosen {
		if g.TwitchGameID == "" && !named[i] {
			missing = append(missing, g)
		}
	}
	c.lookUpTwitchGameIDs(missing)
}

// lookUpTwitchGameIDs asks Twitch, in the background, for the ids of games
// that are due a lookup. A found id is saved; a miss waits twitchIDRetry;
// an error retries on the next scan.
func (c *Collector) lookUpTwitchGameIDs(games []store.ChosenGame) {
	if c.LookupTwitchGameID == nil || len(games) == 0 {
		return
	}
	now := c.now()
	var due []store.ChosenGame
	c.idMu.Lock()
	for _, g := range games {
		if t, ok := c.idTried[g.ID]; ok && now.Sub(t) < twitchIDRetry {
			continue
		}
		c.idTried[g.ID] = now
		due = append(due, g)
	}
	c.idMu.Unlock()
	if len(due) == 0 {
		return
	}
	c.lookups.Add(1)
	go func() {
		defer c.lookups.Done()
		ctx, cancel := context.WithTimeout(context.Background(), twitchIDTimeout)
		defer cancel()
		for _, g := range due {
			id, ok, err := c.LookupTwitchGameID(ctx, g.Name)
			if err != nil {
				slog.Debug("collect: twitch game id lookup failed", "game", g.Name, "err", err)
				c.idMu.Lock()
				delete(c.idTried, g.ID)
				c.idMu.Unlock()
				continue
			}
			if !ok {
				continue
			}
			if err := c.Chosen.Q.SetGameTwitchID(ctx, gen.SetGameTwitchIDParams{TwitchGameID: id, ID: g.ID}); err != nil {
				c.logErr("collect: store twitch game id failed", "", err)
			}
		}
	}()
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

func (c *Collector) MarkedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error) {
	return c.Claims.MarkedBenefitIDs(ctx, accountID)
}

// Forget drops an account's board scan and cached drop states, e.g. after
// the account is deleted. Stored rows go with the account's cascade.
func (c *Collector) Forget(accountID string) {
	c.Board.Forget(accountID)
	c.mu.Lock()
	delete(c.last, accountID)
	c.mu.Unlock()
}
