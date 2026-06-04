package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

const (
	keyGlobalDiscord = "settings:discord_webhook"
	keyLogRetention  = "settings:log_retention_days"
)

type Settings struct {
	q *gen.Queries
}

func NewSettings(q *gen.Queries) *Settings { return &Settings{q: q} }

func (s *Settings) getString(ctx context.Context, key string) (string, error) {
	v, err := s.q.GetSettingString(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(v), nil
}

func (s *Settings) setString(ctx context.Context, key, value string) error {
	return s.q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key:   key,
		Value: []byte(value),
	})
}

func (s *Settings) GlobalDiscordWebhook(ctx context.Context) (string, error) {
	return s.getString(ctx, keyGlobalDiscord)
}

func (s *Settings) SetGlobalDiscordWebhook(ctx context.Context, url string) error {
	return s.setString(ctx, keyGlobalDiscord, url)
}

func (s *Settings) LogRetentionDays(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyLogRetention)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return 7, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 7, nil
	}
	return n, nil
}

func (s *Settings) SetLogRetentionDays(ctx context.Context, days int) error {
	return s.setString(ctx, keyLogRetention, strconv.Itoa(days))
}
