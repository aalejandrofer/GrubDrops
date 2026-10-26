package store

import (
	"context"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Claim sources recorded in claim_log.source.
const (
	ClaimSourceGrubDrops = "grubdrops" // claimed by the runner
	ClaimSourceInventory = "inventory" // found claimed by reconcile
	ClaimSourceLegacy    = "1.x"       // copied from 1.x claims
)

// unixOrZero maps the zero time to 0 so "unset" survives the integer column.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// timeOrZero is the inverse of unixOrZero.
func timeOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

type ClaimEntry struct {
	AccountID, Platform, Game, CampaignID, CampaignName, BenefitID, BenefitName, ImageURL, Code, Source string
	ClaimedAt                                                                                           time.Time
}

// ClaimLog is the permanent record of claimed drops, one row per account and benefit.
type ClaimLog struct{ Q *gen.Queries }

func NewClaimLog(q *gen.Queries) *ClaimLog { return &ClaimLog{Q: q} }

// Record upserts on (account, benefit). The first claimed_at and source stay.
// A non-empty Code replaces the stored one, an empty Code keeps it.
// A zero ClaimedAt records the current time.
func (l *ClaimLog) Record(ctx context.Context, e ClaimEntry) error {
	if e.ClaimedAt.IsZero() {
		e.ClaimedAt = time.Now()
	}
	return l.Q.UpsertClaimLog(ctx, gen.UpsertClaimLogParams{
		AccountID: e.AccountID, BenefitID: e.BenefitID, Platform: e.Platform,
		Game: e.Game, CampaignID: e.CampaignID, CampaignName: e.CampaignName,
		BenefitName: e.BenefitName, ImageUrl: e.ImageURL, Code: e.Code,
		Source: e.Source, ClaimedAt: e.ClaimedAt.Unix(),
	})
}

func (l *ClaimLog) ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error) {
	ids, err := l.Q.ListClaimLogBenefitIDs(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// Get returns one account's claim of a benefit.
func (l *ClaimLog) Get(ctx context.Context, accountID, benefitID string) (ClaimEntry, error) {
	r, err := l.Q.GetClaimLog(ctx, gen.GetClaimLogParams{AccountID: accountID, BenefitID: benefitID})
	if err != nil {
		return ClaimEntry{}, err
	}
	return claimEntries([]gen.ClaimLog{r})[0], nil
}

// Recent returns the newest claims across all accounts.
func (l *ClaimLog) Recent(ctx context.Context, limit int) ([]ClaimEntry, error) {
	rows, err := l.Q.ListRecentClaimLog(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	return claimEntries(rows), nil
}

func claimEntries(rows []gen.ClaimLog) []ClaimEntry {
	out := make([]ClaimEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, ClaimEntry{
			AccountID: r.AccountID, Platform: r.Platform, Game: r.Game,
			CampaignID: r.CampaignID, CampaignName: r.CampaignName,
			BenefitID: r.BenefitID, BenefitName: r.BenefitName, ImageURL: r.ImageUrl,
			Code: r.Code, Source: r.Source, ClaimedAt: time.Unix(r.ClaimedAt, 0),
		})
	}
	return out
}

// ClaimFilter selects a page of claim history. An empty Game, AccountID or
// Query matches any. Query is a case-insensitive substring of the benefit
// name, campaign name or game.
type ClaimFilter struct {
	Game, AccountID, Query string
	Limit, Offset          int
}

var likeEscaper = strings.NewReplacer(`!`, `!!`, `%`, `!%`, `_`, `!_`)

// Page returns one page of claims, newest first, and the total row count
// that matches the filter.
func (l *ClaimLog) Page(ctx context.Context, f ClaimFilter) ([]ClaimEntry, int, error) {
	pattern := ""
	if f.Query != "" {
		pattern = "%" + likeEscaper.Replace(f.Query) + "%"
	}
	total, err := l.Q.CountClaimLogFiltered(ctx, gen.CountClaimLogFilteredParams{Game: f.Game, AccountID: f.AccountID, Pattern: pattern})
	if err != nil {
		return nil, 0, err
	}
	rows, err := l.Q.ListClaimLogPage(ctx, gen.ListClaimLogPageParams{
		Game: f.Game, AccountID: f.AccountID, Pattern: pattern, Limit: int64(f.Limit), Offset: int64(f.Offset),
	})
	if err != nil {
		return nil, 0, err
	}
	return claimEntries(rows), int(total), nil
}

// Games lists the distinct non-empty games with a claim, sorted.
func (l *ClaimLog) Games(ctx context.Context) ([]string, error) {
	g, err := l.Q.ListClaimLogGames(ctx)
	if g == nil {
		g = []string{}
	}
	return g, err
}

func (l *ClaimLog) CountSince(ctx context.Context, since time.Time) (int, error) {
	n, err := l.Q.CountClaimLogSince(ctx, since.Unix())
	return int(n), err
}
