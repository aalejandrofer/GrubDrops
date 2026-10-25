package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type DropState struct {
	AccountID, Platform, Game, CampaignID, CampaignName, BenefitID, BenefitName, ImageURL string
	State, Reason                                                                         string
	Minutes, RequiredMinutes                                                              int
	EndsAt, UpdatedAt                                                                     time.Time
}

// DropStates holds the latest per-account state of every known drop.
type DropStates struct {
	DB *sql.DB
	Q  *gen.Queries
}

func NewDropStates(db *sql.DB, q *gen.Queries) *DropStates { return &DropStates{DB: db, Q: q} }

func upsertParams(r DropState) gen.UpsertDropStateParams {
	return gen.UpsertDropStateParams{
		AccountID: r.AccountID, BenefitID: r.BenefitID, Platform: r.Platform,
		Game: r.Game, CampaignID: r.CampaignID, CampaignName: r.CampaignName,
		BenefitName: r.BenefitName, ImageUrl: r.ImageURL, State: r.State,
		Reason: r.Reason, Minutes: int64(r.Minutes), RequiredMinutes: int64(r.RequiredMinutes),
		EndsAt: unixOrZero(r.EndsAt), UpdatedAt: r.UpdatedAt.Unix(),
	}
}

// Replace swaps one account's rows for rows in a single transaction.
func (s *DropStates) Replace(ctx context.Context, accountID string, rows []DropState) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.Q.WithTx(tx)
	if err := q.DeleteDropStatesForAccount(ctx, accountID); err != nil {
		return err
	}
	for _, r := range rows {
		r.AccountID = accountID
		if err := q.UpsertDropState(ctx, upsertParams(r)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *DropStates) Upsert(ctx context.Context, row DropState) error {
	return s.Q.UpsertDropState(ctx, upsertParams(row))
}

func (s *DropStates) List(ctx context.Context, accountID string) ([]DropState, error) {
	rows, err := s.Q.ListDropStatesForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return dropStatesFromGen(rows), nil
}

func (s *DropStates) ListAll(ctx context.Context) ([]DropState, error) {
	rows, err := s.Q.ListAllDropStates(ctx)
	if err != nil {
		return nil, err
	}
	return dropStatesFromGen(rows), nil
}

func dropStatesFromGen(rows []gen.DropState) []DropState {
	out := make([]DropState, 0, len(rows))
	for _, r := range rows {
		out = append(out, DropState{
			AccountID: r.AccountID, Platform: r.Platform, Game: r.Game,
			CampaignID: r.CampaignID, CampaignName: r.CampaignName,
			BenefitID: r.BenefitID, BenefitName: r.BenefitName, ImageURL: r.ImageUrl,
			State: r.State, Reason: r.Reason,
			Minutes: int(r.Minutes), RequiredMinutes: int(r.RequiredMinutes),
			EndsAt: timeOrZero(r.EndsAt), UpdatedAt: time.Unix(r.UpdatedAt, 0),
		})
	}
	return out
}
