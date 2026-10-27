package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const skipTimeout = 5 * time.Second

// SkipOverridePrefix keys a ghost-skip assertion in kv.
const SkipOverridePrefix = "skip_override:"

// Skips persists ghost-skip assertions in kv under the 1.x keys
// SkipOverridePrefix + benefitID + ":" + accountID, value "1".
type Skips struct{ Q *gen.Queries }

func NewSkips(q *gen.Queries) *Skips { return &Skips{Q: q} }

func (s *Skips) Skips(accountID string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), skipTimeout)
	defer cancel()
	rows, err := s.Q.ListKVByPrefix(ctx, sql.NullString{String: SkipOverridePrefix, Valid: true})
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, kv := range rows {
		if string(kv.Value) != "1" {
			continue
		}
		// rest = benefitID + ":" + accountID
		benefitID, acc, ok := strings.Cut(strings.TrimPrefix(kv.Key, SkipOverridePrefix), ":")
		if !ok || acc != accountID {
			continue
		}
		out[benefitID] = true
	}
	return out, nil
}

func (s *Skips) RecordSkip(accountID, benefitID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), skipTimeout)
	defer cancel()
	return s.Q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key:   SkipOverridePrefix + benefitID + ":" + accountID,
		Value: []byte("1"),
	})
}

func (s *Skips) ClearSkip(accountID, benefitID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), skipTimeout)
	defer cancel()
	return s.Q.DeleteKV(ctx, SkipOverridePrefix+benefitID+":"+accountID)
}

// Unskip removes one skip and reports whether it was there.
func (s *Skips) Unskip(ctx context.Context, accountID, benefitID string) (bool, error) {
	key := SkipOverridePrefix + benefitID + ":" + accountID
	_, err := s.Q.GetSettingString(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, s.Q.DeleteKV(ctx, key)
}
