package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const (
	keyGlobalDiscord    = "settings:discord_webhook"
	keyNotifyAvatarURL  = "settings:notify_avatar_url"
	keyLogRetention     = "settings:log_retention_days"
	keyLogLevel         = "settings:log_level"
	keyTickIntervalMs   = "settings:tick_interval_ms"
	keyDiscoveryIntvSec = "settings:discovery_interval_sec"
	keyNotifyClaim      = "settings:notify_claim"
	keyNotifyProgress   = "settings:notify_progress"
	keyNotifyAuth       = "settings:notify_auth"
	keyNotifyError      = "settings:notify_error"
	keyPriorityMode     = "settings:priority_mode"
)

// PriorityMode controls campaign pick ordering when multiple
// whitelisted campaigns are eligible.
//   - "ordered" (default): sort by whitelist rank, top = first.
//   - "ending_soonest": sort by ends_at ascending so campaigns near
//     expiry get mined first.
//
// Mirrors DevilXD/TwitchDropsMiner's PRIORITY_ORDER toggle.
const (
	PriorityModeOrdered       = "ordered"
	PriorityModeEndingSoonest = "ending_soonest"
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

// NotifyAvatarURL is the avatar image used for the Discord webhook sender.
// Empty means "use the webhook's own avatar" (the payload field is omitted).
func (s *Settings) NotifyAvatarURL(ctx context.Context) (string, error) {
	return s.getString(ctx, keyNotifyAvatarURL)
}

func (s *Settings) SetNotifyAvatarURL(ctx context.Context, url string) error {
	return s.setString(ctx, keyNotifyAvatarURL, url)
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

// LogLevel is the runtime log level. "" means "use the launch env
// MINER_LOG_LEVEL"; explicit value here overrides it.
func (s *Settings) LogLevel(ctx context.Context) (string, error) {
	return s.getString(ctx, keyLogLevel)
}

func (s *Settings) SetLogLevel(ctx context.Context, level string) error {
	return s.setString(ctx, keyLogLevel, level)
}

func (s *Settings) TickIntervalMs(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyTickIntervalMs)
	if err != nil || raw == "" {
		return 500, err
	}
	n, _ := strconv.Atoi(raw)
	if n <= 0 {
		return 500, nil
	}
	return n, nil
}

func (s *Settings) SetTickIntervalMs(ctx context.Context, ms int) error {
	return s.setString(ctx, keyTickIntervalMs, strconv.Itoa(ms))
}

func (s *Settings) DiscoveryIntervalSec(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyDiscoveryIntvSec)
	if err != nil || raw == "" {
		return 300, err
	}
	n, _ := strconv.Atoi(raw)
	if n <= 0 {
		return 300, nil
	}
	return n, nil
}

func (s *Settings) SetDiscoveryIntervalSec(ctx context.Context, sec int) error {
	return s.setString(ctx, keyDiscoveryIntvSec, strconv.Itoa(sec))
}

// NotifyKinds returns the boolean toggles for each Discord
// notification category. Defaults: claim+error on, progress+auth off.
func (s *Settings) NotifyKinds(ctx context.Context) (claim, progress, auth, errors bool) {
	get := func(k string, def bool) bool {
		v, _ := s.getString(ctx, k)
		if v == "" {
			return def
		}
		return v == "1"
	}
	return get(keyNotifyClaim, true), get(keyNotifyProgress, false), get(keyNotifyAuth, false), get(keyNotifyError, true)
}

func (s *Settings) PriorityMode(ctx context.Context) (string, error) {
	v, err := s.getString(ctx, keyPriorityMode)
	if err != nil || v == "" {
		return PriorityModeOrdered, err
	}
	if v != PriorityModeOrdered && v != PriorityModeEndingSoonest {
		return PriorityModeOrdered, nil
	}
	return v, nil
}

func (s *Settings) SetPriorityMode(ctx context.Context, mode string) error {
	if mode != PriorityModeOrdered && mode != PriorityModeEndingSoonest {
		mode = PriorityModeOrdered
	}
	return s.setString(ctx, keyPriorityMode, mode)
}

func (s *Settings) SetNotifyKinds(ctx context.Context, claim, progress, auth, errors bool) error {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	if err := s.setString(ctx, keyNotifyClaim, b(claim)); err != nil {
		return err
	}
	if err := s.setString(ctx, keyNotifyProgress, b(progress)); err != nil {
		return err
	}
	if err := s.setString(ctx, keyNotifyAuth, b(auth)); err != nil {
		return err
	}
	return s.setString(ctx, keyNotifyError, b(errors))
}
