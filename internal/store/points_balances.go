package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Error codes a cached points balance carries after its last read.
const (
	// PointsErrFailed: the last read failed; the balance is the last good one.
	PointsErrFailed = "failed"
	// PointsErrUnavailable: the channel has no balance for the account.
	PointsErrUnavailable = "unavailable"
)

// PointsBalance is the cached channel-points balance of one account on one
// Twitch channel.
type PointsBalance struct {
	AccountID string
	Channel   string
	Balance   *int64    // nil = never read
	FetchedAt time.Time // last successful read; zero = never
	CheckedAt time.Time // last attempt
	Error     string    // "", PointsErrFailed or PointsErrUnavailable
	Failures  int       // failed reads in a row
}

// PointsBalances stores the cached balances.
type PointsBalances struct{ Q *gen.Queries }

func NewPointsBalances(q *gen.Queries) *PointsBalances { return &PointsBalances{Q: q} }

func pointsBalanceFrom(r gen.PointsBalance) PointsBalance {
	b := PointsBalance{
		AccountID: r.AccountID, Channel: r.Channel,
		CheckedAt: time.Unix(r.CheckedAt, 0),
		Error:     r.Error, Failures: int(r.Failures),
	}
	if r.Balance.Valid {
		v := r.Balance.Int64
		b.Balance = &v
	}
	if r.FetchedAt.Valid {
		b.FetchedAt = time.Unix(r.FetchedAt.Int64, 0)
	}
	return b
}

// List returns every cached balance.
func (p *PointsBalances) List(ctx context.Context) ([]PointsBalance, error) {
	rows, err := p.Q.ListPointsBalances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PointsBalance, 0, len(rows))
	for _, r := range rows {
		out = append(out, pointsBalanceFrom(r))
	}
	return out, nil
}

// Get returns one cached balance; ok is false when none is stored.
func (p *PointsBalances) Get(ctx context.Context, accountID, channel string) (PointsBalance, bool, error) {
	r, err := p.Q.GetPointsBalance(ctx, gen.GetPointsBalanceParams{AccountID: accountID, Channel: channel})
	if errors.Is(err, sql.ErrNoRows) {
		return PointsBalance{}, false, nil
	}
	if err != nil {
		return PointsBalance{}, false, err
	}
	return pointsBalanceFrom(r), true, nil
}

// RecordOK stores a successful read at at and clears any error.
func (p *PointsBalances) RecordOK(ctx context.Context, accountID, channel string, balance int64, at time.Time) error {
	return p.Q.UpsertPointsBalance(ctx, gen.UpsertPointsBalanceParams{
		AccountID: accountID, Channel: channel,
		Balance:   sql.NullInt64{Int64: balance, Valid: true},
		FetchedAt: sql.NullInt64{Int64: at.Unix(), Valid: true},
		CheckedAt: at.Unix(),
	})
}

// RecordErr stores a read at at that gave no balance. The last good balance
// and its fetched_at stay.
func (p *PointsBalances) RecordErr(ctx context.Context, accountID, channel, code string, failures int, at time.Time) error {
	return p.Q.UpsertPointsBalanceError(ctx, gen.UpsertPointsBalanceErrorParams{
		AccountID: accountID, Channel: channel,
		CheckedAt: at.Unix(), Error: code, Failures: int64(failures),
	})
}

// Prune deletes balances whose account is no longer assigned to that Twitch
// Force watch channel. Deleted accounts go with their rows by cascade.
func (p *PointsBalances) Prune(ctx context.Context) (int64, error) {
	return p.Q.PrunePointsBalances(ctx)
}
