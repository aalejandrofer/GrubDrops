package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const (
	keyGlobalDiscord       = "settings:discord_webhook"
	keyNotifyAvatarURL     = "settings:notify_avatar_url"
	keyLogRetention        = "settings:log_retention_days"
	keyLogLevel            = "settings:log_level"
	keyTickIntervalSec     = "settings:tick_interval_sec"
	keyTickIntervalMs      = "settings:tick_interval_ms" // legacy; migrated to tick_interval_sec
	keyDiscoveryIntvMin    = "settings:discovery_interval_min"
	keyDiscoveryIntvSec    = "settings:discovery_interval_sec" // legacy; migrated to discovery_interval_min
	keyNotifyClaim         = "settings:notify_claim"
	keyNotifyProgress      = "settings:notify_progress"
	keyNotifyAuth          = "settings:notify_auth"
	keyNotifyError         = "settings:notify_error"
	keyNotifyCanary        = "settings:notify_canary"
	keyNotifyProgStep      = "settings:notify_progress_step_pct"
	keyPriorityMode        = "settings:priority_mode"
	keyKickWatchMode       = "settings:kick_watch_mode"
	keyCanaryTwitchChannel = "settings:canary_twitch_channel"
	keyCanaryKickChannel   = "settings:canary_kick_channel"
	keyCanaryIntervalSec   = "settings:canary_interval_sec"
	keyProxyURL            = "settings:proxy_url"
	keyProxyEnabled        = "settings:proxy_enabled"
	keyLatestRelease       = "settings:latest_release"     // most recent GitHub release tag
	keyLastReleaseCheck    = "settings:last_release_check" // unix seconds of last successful check
)

// KickWatchMode selects how Kick watch-time is accrued.
//   - "browser" (default): drive a real IVS <video> in the chromedp sidecar.
//     The verified-working path; needs a per-account sidecar.
//   - "ws" (EXPERIMENTAL): pure-WebSocket viewer presence, no browser/video.
//     Live-verified to accrue (see internal/platform/kick/wswatch.go). Mutually
//     exclusive with the sidecar — the server credits one active watch per
//     account.
//   - "auto" (EXPERIMENTAL): try the WS path first; if the WS connection dies
//     (exhausts reconnects), fall back to the Chrome sidecar for that account.
const (
	KickWatchModeBrowser = "browser"
	KickWatchModeWS      = "ws"
	KickWatchModeAuto    = "auto"
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

// HeartbeatInterval is intentionally NOT a setting. It is locked to 60s in the
// watcher build (cmd/miner): the Twitch watch-ping beacon cadence derives from
// it and Twitch credits exactly 1 minute per beacon, so any value >60s
// under-credits Twitch watch-time. Do not re-expose this as a user setting.

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
// notification category. Defaults: claim+error on, progress+auth+canary off.
func (s *Settings) NotifyKinds(ctx context.Context) (claim, progress, auth, errors, canary bool) {
	get := func(k string, def bool) bool {
		v, _ := s.getString(ctx, k)
		if v == "" {
			return def
		}
		return v == "1"
	}
	return get(keyNotifyClaim, true), get(keyNotifyProgress, false), get(keyNotifyAuth, false), get(keyNotifyError, true), get(keyNotifyCanary, false)
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

// KickWatchMode returns the configured Kick watch path. Default is "auto"
// (WS first, Chrome sidecar fallback): WS needs no Docker so a fresh install
// mines Kick on any Pi out of the box, and auto falls back to the verified
// Chrome IVS path if WS stops accruing. Read at miner startup (reload required
// to apply) and surfaced on the dashboard so the operator can see the active path.
func (s *Settings) KickWatchMode(ctx context.Context) (string, error) {
	v, err := s.getString(ctx, keyKickWatchMode)
	if err != nil || v == "" {
		return KickWatchModeAuto, err
	}
	if v != KickWatchModeBrowser && v != KickWatchModeWS && v != KickWatchModeAuto {
		return KickWatchModeAuto, nil
	}
	return v, nil
}

func (s *Settings) SetKickWatchMode(ctx context.Context, mode string) error {
	if mode != KickWatchModeBrowser && mode != KickWatchModeWS && mode != KickWatchModeAuto {
		mode = KickWatchModeAuto
	}
	return s.setString(ctx, keyKickWatchMode, mode)
}

func (s *Settings) SetNotifyKinds(ctx context.Context, claim, progress, auth, errors, canary bool) error {
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
	if err := s.setString(ctx, keyNotifyError, b(errors)); err != nil {
		return err
	}
	return s.setString(ctx, keyNotifyCanary, b(canary))
}

// CanaryTwitchChannel is the Twitch channel used for accrual canary checks.
// Default "alveussanctuary" (always-live charity stream).
func (s *Settings) CanaryTwitchChannel(ctx context.Context) (string, error) {
	raw, err := s.getString(ctx, keyCanaryTwitchChannel)
	if err != nil || raw == "" {
		return "alveussanctuary", err
	}
	return raw, nil
}

func (s *Settings) SetCanaryTwitchChannel(ctx context.Context, v string) error {
	return s.setString(ctx, keyCanaryTwitchChannel, strings.TrimSpace(v))
}

// CanaryKickChannel is the Kick channel used for accrual canary checks.
// Default "" (empty = canary skipped for Kick).
func (s *Settings) CanaryKickChannel(ctx context.Context) (string, error) {
	return s.getString(ctx, keyCanaryKickChannel)
}

func (s *Settings) SetCanaryKickChannel(ctx context.Context, v string) error {
	return s.setString(ctx, keyCanaryKickChannel, strings.TrimSpace(v))
}

// CanaryIntervalSec is how often the accrual canary runs, in seconds.
// Default 21600 (6 hours). An explicit 0 disables the canary (Runner.Run
// treats <=0 as disabled). Empty/unset → default; 0 → disabled; parse
// error → default.
func (s *Settings) CanaryIntervalSec(ctx context.Context) (int, error) {
	raw, err := s.getString(ctx, keyCanaryIntervalSec)
	if err != nil {
		return 0, err
	}
	// Distinguish "never set" (empty) from an explicit 0 (operator disabled).
	if raw == "" {
		return 6 * 3600, nil
	}
	n, perr := strconv.Atoi(raw)
	if perr != nil {
		return 6 * 3600, nil
	}
	// n == 0 is intentional: the operator explicitly disabled the canary.
	return n, nil
}

func (s *Settings) SetCanaryIntervalSec(ctx context.Context, n int) error {
	return s.setString(ctx, keyCanaryIntervalSec, strconv.Itoa(n))
}

// ProxyURL returns the configured proxy URL (e.g. "http://127.0.0.1:7890"
// or "socks5://127.0.0.1:1080"). Empty means no proxy.
func (s *Settings) ProxyURL(ctx context.Context) (string, error) {
	return s.getString(ctx, keyProxyURL)
}

func (s *Settings) SetProxyURL(ctx context.Context, url string) error {
	return s.setString(ctx, keyProxyURL, strings.TrimSpace(url))
}

// ProxyEnabled returns whether the proxy is active. Default false.
func (s *Settings) ProxyEnabled(ctx context.Context) (bool, error) {
	v, err := s.getString(ctx, keyProxyEnabled)
	if err != nil || v == "" {
		return false, err
	}
	return v == "1", nil
}

func (s *Settings) SetProxyEnabled(ctx context.Context, enabled bool) error {
	if enabled {
		return s.setString(ctx, keyProxyEnabled, "1")
	}
	return s.setString(ctx, keyProxyEnabled, "0")
}

// LatestRelease is the most recent GitHub release tag the update checker saw
// (e.g. "v1.3.5"). Empty when no check has succeeded yet.
func (s *Settings) LatestRelease(ctx context.Context) (string, error) {
	return s.getString(ctx, keyLatestRelease)
}

func (s *Settings) SetLatestRelease(ctx context.Context, tag string) error {
	return s.setString(ctx, keyLatestRelease, strings.TrimSpace(tag))
}

// LastReleaseCheck is the unix-seconds time of the last successful release
// check. 0 when none has succeeded.
func (s *Settings) LastReleaseCheck(ctx context.Context) (int64, error) {
	raw, err := s.getString(ctx, keyLastReleaseCheck)
	if err != nil || raw == "" {
		return 0, err
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil {
		return 0, nil
	}
	return n, nil
}

func (s *Settings) SetLastReleaseCheck(ctx context.Context, unixSec int64) error {
	return s.setString(ctx, keyLastReleaseCheck, strconv.FormatInt(unixSec, 10))
}
