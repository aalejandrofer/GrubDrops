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
	keyTickIntervalSec  = "settings:tick_interval_sec"
	keyTickIntervalMs   = "settings:tick_interval_ms" // legacy; migrated to tick_interval_sec
	keyDiscoveryIntvMin = "settings:discovery_interval_min"
	keyDiscoveryIntvSec = "settings:discovery_interval_sec" // legacy; migrated to discovery_interval_min
	keyHeartbeatIntvSec = "settings:heartbeat_interval_sec"
	keyHeartbeatsPerMin = "settings:heartbeats_per_min" // legacy; migrated to heartbeat_interval_sec
	keyNotifyClaim      = "settings:notify_claim"
	keyNotifyProgress   = "settings:notify_progress"
	keyNotifyAuth       = "settings:notify_auth"
	keyNotifyError      = "settings:notify_error"
	keyNotifyProgStep   = "settings:notify_progress_step_pct"
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
// GRUB_LOG_LEVEL"; explicit value here overrides it.
func (s *Settings) LogLevel(ctx context.Context) (string, error) {
	return s.getString(ctx, keyLogLevel)
}

func (s *Settings) SetLogLevel(ctx context.Context, level string) error {
	return s.setString(ctx, keyLogLevel, level)
}

// TickIntervalSec is the watcher state-machine pulse in seconds (default 5).
// Pure loop granularity — no network call rides on the tick itself; the
// heartbeat/livecheck cadences are wall-clock and derived independently.
// Falls back to the legacy ms key when unset. Clamped to [1s, 5m].
func (s *Settings) TickIntervalSec(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyTickIntervalSec)
	if err != nil || raw == "" {
		// Legacy migration: tick_interval_ms, rounded up to a whole second.
		if legacy, lerr := s.getString(ctx, keyTickIntervalMs); lerr == nil && legacy != "" {
			if ms, _ := strconv.Atoi(legacy); ms > 0 {
				return clampInt((ms+999)/1000, 1, 300), nil
			}
		}
		return 5, err
	}
	n, _ := strconv.Atoi(raw)
	if n <= 0 {
		return 5, nil
	}
	return clampInt(n, 1, 300), nil
}

func (s *Settings) SetTickIntervalSec(ctx context.Context, sec int) error {
	return s.setString(ctx, keyTickIntervalSec, strconv.Itoa(sec))
}

// DiscoveryIntervalMin is the campaign-catalog re-scan cadence in minutes
// (default 60). Falls back to the legacy seconds key when unset. Clamped to
// [1m, 24h].
func (s *Settings) DiscoveryIntervalMin(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyDiscoveryIntvMin)
	if err != nil || raw == "" {
		// Legacy migration: discovery_interval_sec, rounded up to a minute.
		if legacy, lerr := s.getString(ctx, keyDiscoveryIntvSec); lerr == nil && legacy != "" {
			if sec, _ := strconv.Atoi(legacy); sec > 0 {
				return clampInt((sec+59)/60, 1, 1440), nil
			}
		}
		return 60, err
	}
	n, _ := strconv.Atoi(raw)
	if n <= 0 {
		return 60, nil
	}
	return clampInt(n, 1, 1440), nil
}

func (s *Settings) SetDiscoveryIntervalMin(ctx context.Context, min int) error {
	return s.setString(ctx, keyDiscoveryIntvMin, strconv.Itoa(min))
}

// HeartbeatIntervalSec is how often (seconds) the watcher runs a watch-ping +
// progress-poll cycle. Default 60. Can exceed 60 to slow the API request rate
// (Kick accrues via the presence WS, so polling slower only delays progress
// display). Falls back to the legacy heartbeats_per_min key when unset.
// Clamped to [10s, 1h].
func (s *Settings) HeartbeatIntervalSec(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyHeartbeatIntvSec)
	if err != nil || raw == "" {
		// Legacy migration: heartbeats_per_min N => 60/N seconds.
		if legacy, lerr := s.getString(ctx, keyHeartbeatsPerMin); lerr == nil && legacy != "" {
			if n, _ := strconv.Atoi(legacy); n >= 1 {
				return clampInt(60/n, 10, 3600), nil
			}
		}
		return 60, err
	}
	n, _ := strconv.Atoi(raw)
	if n <= 0 {
		return 60, nil
	}
	return clampInt(n, 10, 3600), nil
}

func (s *Settings) SetHeartbeatIntervalSec(ctx context.Context, sec int) error {
	return s.setString(ctx, keyHeartbeatIntvSec, strconv.Itoa(sec))
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// ProgressNotifyStepPct is the milestone granularity for Discord progress
// notifications: fire at 0% (start), every N%, and 100%. Default 50 (so
// start/50%/100%). 0 disables progress notifications entirely (claim only).
func (s *Settings) ProgressNotifyStepPct(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyNotifyProgStep)
	if err != nil || raw == "" {
		return 50, err
	}
	n, _ := strconv.Atoi(raw)
	if n < 0 {
		return 0, nil
	}
	if n > 100 {
		return 100, nil
	}
	return n, nil
}

func (s *Settings) SetProgressNotifyStepPct(ctx context.Context, pct int) error {
	return s.setString(ctx, keyNotifyProgStep, strconv.Itoa(pct))
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
