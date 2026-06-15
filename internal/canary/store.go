package canary

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// Result is one platform's last accrual-canary outcome.
type Result struct {
	OK        bool      `json:"ok"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
}

const keyPrefix = "canary:"

// SaveResult persists r (stamping CheckedAt=now) under canary:<platform>.
func SaveResult(ctx context.Context, q *gen.Queries, platform string, r Result) error {
	r.CheckedAt = time.Now().UTC()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key:   keyPrefix + platform,
		Value: b,
	})
}

// LoadResult returns the stored result for platform; ok=false when none stored.
func LoadResult(ctx context.Context, q *gen.Queries, platform string) (Result, bool, error) {
	v, err := q.GetSettingString(ctx, keyPrefix+platform)
	if err != nil {
		return Result{}, false, nil // not-found treated as "no result"
	}
	var r Result
	if err := json.Unmarshal(v, &r); err != nil {
		return Result{}, false, err
	}
	return r, true, nil
}
