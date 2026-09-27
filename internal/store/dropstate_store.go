package store

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// DropStateStore persists pipeline v2 drop_state rows and reads the
// per-account streamer priority list.
type DropStateStore struct {
	Q *gen.Queries
}

func NewDropStateStore(q *gen.Queries) *DropStateStore { return &DropStateStore{Q: q} }

func dsUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func dsTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func (s *DropStateStore) List(ctx context.Context, accountID string) ([]dropstate.Row, error) {
	rows, err := s.Q.ListDropStates(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]dropstate.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, dropstate.Row{
			AccountID:  r.AccountID,
			DropID:     r.DropID,
			CampaignID: r.CampaignID,
			Platform:   r.Platform,
			Status:     dropstate.Status(r.Status),
			Reason:     dropstate.Reason(r.BlockReason),
			Minutes:    int(r.Minutes),
			Required:   int(r.Required),
			Source:     dropstate.Source(r.Source),
			FailCount:  int(r.FailCount),
			RetryAfter: dsTime(r.RetryAfter),
			SyncedAt:   dsTime(r.SyncedAt),
			UpdatedAt:  dsTime(r.UpdatedAt),
		})
	}
	return out, nil
}

func (s *DropStateStore) Upsert(ctx context.Context, r dropstate.Row) error {
	return s.Q.UpsertDropState(ctx, gen.UpsertDropStateParams{
		AccountID:   r.AccountID,
		DropID:      r.DropID,
		CampaignID:  r.CampaignID,
		Platform:    r.Platform,
		Status:      string(r.Status),
		BlockReason: string(r.Reason),
		Minutes:     int64(r.Minutes),
		Required:    int64(r.Required),
		Source:      string(r.Source),
		FailCount:   int64(r.FailCount),
		RetryAfter:  dsUnix(r.RetryAfter),
		SyncedAt:    dsUnix(r.SyncedAt),
		UpdatedAt:   dsUnix(r.UpdatedAt),
	})
}

func (s *DropStateStore) StreamerPriority(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.Q.ListStreamerPriority(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Login)
	}
	return out, nil
}
