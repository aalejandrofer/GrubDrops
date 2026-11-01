package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// RewardCode is one stored reward an account received. Code is decrypted
// and is a secret: never log it. Zero times mean unknown or not yet.
type RewardCode struct {
	ID          int64
	AccountID   string
	Key         string
	Name        string
	Game        string
	Code        string
	RedeemURL   string
	ExpiresAt   time.Time
	ReceivedAt  time.Time
	FirstSeen   time.Time
	NotifiedAt  time.Time
	RemindedAt  time.Time
	RedeemedAt  time.Time
	DismissedAt time.Time
	// NotifySkippedAt is when the new-code notice was skipped: no target
	// took it, or the code was first seen already expired.
	NotifySkippedAt time.Time
}

// Open reports whether the reward is neither redeemed nor dismissed.
func (r RewardCode) Open() bool { return r.RedeemedAt.IsZero() && r.DismissedAt.IsZero() }

// RewardCodes stores reward codes with each code age-encrypted at rest.
type RewardCodes struct {
	Q *gen.Queries
	C *Cryptor
}

func NewRewardCodes(q *gen.Queries, c *Cryptor) *RewardCodes { return &RewardCodes{Q: q, C: c} }

func nullUnix(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

func fromNullUnix(n sql.NullInt64) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return time.Unix(n.Int64, 0)
}

func (s *RewardCodes) from(r gen.RewardCode) RewardCode {
	out := RewardCode{
		ID: r.ID, AccountID: r.AccountID, Key: r.RewardKey, Name: r.Name, Game: r.Game,
		RedeemURL:       r.RedeemUrl,
		ExpiresAt:       fromNullUnix(r.ExpiresAt),
		ReceivedAt:      fromNullUnix(r.ReceivedAt),
		FirstSeen:       time.Unix(r.FirstSeen, 0),
		NotifiedAt:      fromNullUnix(r.NotifiedAt),
		RemindedAt:      fromNullUnix(r.RemindedAt),
		RedeemedAt:      fromNullUnix(r.RedeemedAt),
		DismissedAt:     fromNullUnix(r.DismissedAt),
		NotifySkippedAt: fromNullUnix(r.NotifySkippedAt),
	}
	if len(r.Code) > 0 {
		plain, err := s.C.Decrypt(r.Code)
		if err != nil {
			// The error never carries the code; the row stays listed without it.
			slog.Warn("reward codes: decrypt failed", "kind", "error", "id", r.ID, "err", err)
		} else {
			out.Code = string(plain)
		}
	}
	return out
}

// Save stores one read reward for accountID, first seen at now. A known
// reward keeps its stored values where the read has none. It returns the
// stored row and whether the reward is new.
func (s *RewardCodes) Save(ctx context.Context, accountID string, rc platform.RewardCode, now time.Time) (RewardCode, bool, error) {
	if rc.Key == "" {
		return RewardCode{}, false, errors.New("reward codes: empty key")
	}
	_, err := s.Q.GetRewardCodeByKey(ctx, gen.GetRewardCodeByKeyParams{AccountID: accountID, RewardKey: rc.Key})
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return RewardCode{}, false, err
	}
	var ct []byte
	if rc.Code != "" {
		if ct, err = s.C.Encrypt([]byte(rc.Code)); err != nil {
			return RewardCode{}, false, fmt.Errorf("reward codes: encrypt: %w", err)
		}
	}
	if err := s.Q.UpsertRewardCode(ctx, gen.UpsertRewardCodeParams{
		AccountID: accountID, RewardKey: rc.Key, Name: rc.Name, Game: rc.Game,
		Code: ct, RedeemUrl: rc.RedeemURL,
		ExpiresAt: nullUnix(rc.ExpiresAt), ReceivedAt: nullUnix(rc.ReceivedAt),
		FirstSeen: now.Unix(),
	}); err != nil {
		return RewardCode{}, false, err
	}
	row, err := s.Q.GetRewardCodeByKey(ctx, gen.GetRewardCodeByKeyParams{AccountID: accountID, RewardKey: rc.Key})
	if err != nil {
		return RewardCode{}, false, err
	}
	return s.from(row), isNew, nil
}

// List returns every stored reward, newest first; openOnly leaves out
// redeemed and dismissed ones.
func (s *RewardCodes) List(ctx context.Context, openOnly bool) ([]RewardCode, error) {
	var rows []gen.RewardCode
	var err error
	if openOnly {
		rows, err = s.Q.ListOpenRewardCodes(ctx)
	} else {
		rows, err = s.Q.ListRewardCodes(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := make([]RewardCode, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.from(r))
	}
	return out, nil
}

// Get returns one reward; ok is false when none has that id.
func (s *RewardCodes) Get(ctx context.Context, id int64) (RewardCode, bool, error) {
	r, err := s.Q.GetRewardCode(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return RewardCode{}, false, nil
	}
	if err != nil {
		return RewardCode{}, false, err
	}
	return s.from(r), true, nil
}

// MarkNotified records that the new-code notification went out at at.
func (s *RewardCodes) MarkNotified(ctx context.Context, id int64, at time.Time) error {
	return s.Q.MarkRewardCodeNotified(ctx, gen.MarkRewardCodeNotifiedParams{NotifiedAt: nullUnix(at), ID: id})
}

// MarkNotifySkipped records that the new-code notification was skipped at
// at, so it is never sent.
func (s *RewardCodes) MarkNotifySkipped(ctx context.Context, id int64, at time.Time) error {
	return s.Q.MarkRewardCodeNotifySkipped(ctx, gen.MarkRewardCodeNotifySkippedParams{NotifySkippedAt: nullUnix(at), ID: id})
}

// MarkReminded records that the expiry reminder went out at at.
func (s *RewardCodes) MarkReminded(ctx context.Context, id int64, at time.Time) error {
	return s.Q.MarkRewardCodeReminded(ctx, gen.MarkRewardCodeRemindedParams{RemindedAt: nullUnix(at), ID: id})
}

// SetRedeemed marks the reward redeemed at at; false when no reward has id.
func (s *RewardCodes) SetRedeemed(ctx context.Context, id int64, at time.Time) (bool, error) {
	n, err := s.Q.SetRewardCodeRedeemed(ctx, gen.SetRewardCodeRedeemedParams{RedeemedAt: nullUnix(at), ID: id})
	return n > 0, err
}

// SetDismissed marks the reward dismissed at at; false when no reward has id.
func (s *RewardCodes) SetDismissed(ctx context.Context, id int64, at time.Time) (bool, error) {
	n, err := s.Q.SetRewardCodeDismissed(ctx, gen.SetRewardCodeDismissedParams{DismissedAt: nullUnix(at), ID: id})
	return n > 0, err
}
