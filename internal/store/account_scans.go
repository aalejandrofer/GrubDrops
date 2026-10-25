package store

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type AccountScan struct {
	AccountID               string
	ScannedAt, ReconciledAt time.Time
	Error                   string
}

// AccountScans records when each account was last scanned and reconciled.
type AccountScans struct{ Q *gen.Queries }

func NewAccountScans(q *gen.Queries) *AccountScans { return &AccountScans{Q: q} }

// Put upserts. A zero ScannedAt or ReconciledAt keeps the stored value.
// Error is always written, so an empty string clears it.
func (a *AccountScans) Put(ctx context.Context, s AccountScan) error {
	return a.Q.PutAccountScan(ctx, gen.PutAccountScanParams{
		AccountID: s.AccountID, ScannedAt: unixOrZero(s.ScannedAt),
		ReconciledAt: unixOrZero(s.ReconciledAt), Error: s.Error,
	})
}

func (a *AccountScans) List(ctx context.Context) ([]AccountScan, error) {
	rows, err := a.Q.ListAccountScans(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountScan, 0, len(rows))
	for _, r := range rows {
		out = append(out, AccountScan{
			AccountID: r.AccountID, ScannedAt: timeOrZero(r.ScannedAt),
			ReconciledAt: timeOrZero(r.ReconciledAt), Error: r.Error,
		})
	}
	return out, nil
}
