package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const (
	pointsAccountPrefix = "points:account:"
	pointsSafetyKey     = "points:safety"
)

type PointsAccountSettings struct {
	Enabled bool `json:"enabled"`
	Only    bool `json:"only"`
	Streams int  `json:"streams"`
}

type PointsSafety struct {
	SleepEnabled bool `json:"sleep_enabled"`
	SleepStart   int  `json:"sleep_start"`
	SleepEnd     int  `json:"sleep_end"`
	FollowRaids  bool `json:"follow_raids"`
}

func LoadPointsAccountSettings(ctx context.Context, q *gen.Queries, accountID string) (PointsAccountSettings, error) {
	v, err := q.GetSettingString(ctx, pointsAccountPrefix+accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return PointsAccountSettings{Enabled: true, Streams: 2}, nil
	}
	if err != nil {
		return PointsAccountSettings{}, err
	}
	var settings PointsAccountSettings
	if err := json.Unmarshal(v, &settings); err != nil {
		return PointsAccountSettings{}, err
	}
	return settings, nil
}

func SavePointsAccountSettings(ctx context.Context, q *gen.Queries, accountID string, settings PointsAccountSettings) error {
	v, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: pointsAccountPrefix + accountID, Value: v})
}

func LoadPointsSafety(ctx context.Context, q *gen.Queries) (PointsSafety, error) {
	v, err := q.GetSettingString(ctx, pointsSafetyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return PointsSafety{SleepEnabled: true, SleepStart: 120, SleepEnd: 480, FollowRaids: true}, nil
	}

	if err != nil {
		return PointsSafety{}, err
	}
	var settings PointsSafety
	if err := json.Unmarshal(v, &settings); err != nil {
		return PointsSafety{}, err
	}
	return settings, nil
}

func SavePointsSafety(ctx context.Context, q *gen.Queries, settings PointsSafety) error {
	v, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: pointsSafetyKey, Value: v})
}

func DeletePointsAccountSettings(ctx context.Context, q *gen.Queries, accountID string) error {
	return q.DeleteKV(ctx, pointsAccountPrefix+accountID)
}
