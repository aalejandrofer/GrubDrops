package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
)

const (
	keyGlobalDiscord       = "settings:discord_webhook"
	keyTelegramBotToken    = "settings:telegram_bot_token"
	keyTelegramChatID      = "settings:telegram_chat_id"
	keyNotifyAvatarURL     = "settings:notify_avatar_url"
	keyNotifyClaim         = "settings:notify_claim"
	keyNotifyProgress      = "settings:notify_progress"
	keyNotifyAuth          = "settings:notify_auth"
	keyNotifyError         = "settings:notify_error"
	keyNotifyCanary        = "settings:notify_canary"
	keyKickWatchMode       = "settings:kick_watch_mode"
	keyCanaryTwitchChannel = "settings:canary_twitch_channel"
	keyCanaryKickChannel   = "settings:canary_kick_channel"
	keyCanaryIntervalSec   = "settings:canary_interval_sec"
	keyProxyURL            = "settings:proxy_url"
	keyProxyEnabled        = "settings:proxy_enabled"
	keyLatestRelease       = "settings:latest_release"     // most recent GitHub release tag
	keyLastReleaseCheck    = "settings:last_release_check" // unix seconds of last successful check
	keyTimezone            = "settings:timezone"           // IANA display timezone (empty = use TZ env/UTC)
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

// TelegramBotToken is the Telegram Bot API token. Empty means unset.
func (s *Settings) TelegramBotToken(ctx context.Context) (string, error) {
	return s.getString(ctx, keyTelegramBotToken)
}

func (s *Settings) SetTelegramBotToken(ctx context.Context, token string) error {
	return s.setString(ctx, keyTelegramBotToken, strings.TrimSpace(token))
}

// TelegramChatID is the chat the bot posts to. Empty means unset.
func (s *Settings) TelegramChatID(ctx context.Context) (string, error) {
	return s.getString(ctx, keyTelegramChatID)
}

func (s *Settings) SetTelegramChatID(ctx context.Context, id string) error {
	return s.setString(ctx, keyTelegramChatID, strings.TrimSpace(id))
}

// NotifyAvatarURL is the avatar image used for the Discord webhook sender.
// Empty means "use the webhook's own avatar" (the payload field is omitted).
func (s *Settings) NotifyAvatarURL(ctx context.Context) (string, error) {
	return s.getString(ctx, keyNotifyAvatarURL)
}

func (s *Settings) SetNotifyAvatarURL(ctx context.Context, url string) error {
	return s.setString(ctx, keyNotifyAvatarURL, url)
}

// HeartbeatInterval is intentionally NOT a setting. It is locked to 60s in the
// runner (runner.DefaultTick): the Twitch watch-ping beacon cadence derives from
// it and Twitch credits exactly 1 minute per beacon, so any value >60s
// under-credits Twitch watch-time. Do not re-expose this as a user setting.

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

// Timezone is the IANA display timezone chosen in-app (e.g. "Asia/Shanghai").
// Empty means "unset" — the caller falls back to the TZ env var, then UTC.
func (s *Settings) Timezone(ctx context.Context) (string, error) {
	return s.getString(ctx, keyTimezone)
}

// SetTimezone validates the IANA name (empty clears it) and persists it. An
// unloadable zone is rejected so the UI can flash an error and the stored
// value is left untouched.
func (s *Settings) SetTimezone(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if !timeutil.Valid(name) {
		return fmt.Errorf("invalid timezone %q", name)
	}
	return s.setString(ctx, keyTimezone, name)
}

// v2 UI settings
const (
	keyUILanguage         = "settings:ui_language"
	keyUITimeFormat       = "settings:ui_time_format"
	keyUITheme            = "settings:ui_theme"
	keyNotifyDailySummary = "settings:notify_daily_summary"
	keyUpdateAuto         = "settings:update_auto"
	keyPriorityMode       = "settings:priority_mode"
	keyScanIntervalMin    = "settings:scan_interval_min"
	keyPreempt            = "settings:preempt"
)

// ScanIntervals are the allowed scan paces, in minutes: 15 minutes to 24 hours.
var ScanIntervals = []int{15, 30, 60, 180, 360, 720, 1440}

// ScanIntervalMin is how often each account checks for new campaigns, in
// minutes. One of ScanIntervals; default 60.
func (s *Settings) ScanIntervalMin(ctx context.Context) (int, error) {
	v, err := s.getString(ctx, keyScanIntervalMin)
	if err != nil {
		return 60, err
	}
	n, perr := strconv.Atoi(v)
	if perr != nil || !slices.Contains(ScanIntervals, n) {
		return 60, nil
	}
	return n, nil
}

func (s *Settings) SetScanIntervalMin(ctx context.Context, n int) error {
	if !slices.Contains(ScanIntervals, n) {
		return fmt.Errorf("invalid value %d for %s", n, keyScanIntervalMin)
	}
	return s.setString(ctx, keyScanIntervalMin, strconv.Itoa(n))
}

// Preempt reports whether a watching account switches to a campaign that now
// outranks the current one. Off means it finishes the current drop first.
// Default on.
func (s *Settings) Preempt(ctx context.Context) (bool, error) {
	return s.boolKey(ctx, keyPreempt, true)
}

func (s *Settings) SetPreempt(ctx context.Context, on bool) error {
	return s.setBool(ctx, keyPreempt, on)
}

// PriorityMode orders the mining queue: ordered (game list order) or
// ending_soonest (campaign ending first). Default ordered.
func (s *Settings) PriorityMode(ctx context.Context) (string, error) {
	return s.enumString(ctx, keyPriorityMode, "ordered", "ordered", "ending_soonest")
}

func (s *Settings) SetPriorityMode(ctx context.Context, v string) error {
	return s.setEnum(ctx, keyPriorityMode, v, "ordered", "ending_soonest")
}

// UILanguage is the UI and notification language: en, es, zh-CN or tr. Default en.
func (s *Settings) UILanguage(ctx context.Context) (string, error) {
	return s.enumString(ctx, keyUILanguage, "en", "en", "es", "zh-CN", "tr")
}

func (s *Settings) SetUILanguage(ctx context.Context, v string) error {
	return s.setEnum(ctx, keyUILanguage, v, "en", "es", "zh-CN", "tr")
}

// UITimeFormat is 24h or 12h. Default 24h.
func (s *Settings) UITimeFormat(ctx context.Context) (string, error) {
	return s.enumString(ctx, keyUITimeFormat, "24h", "24h", "12h")
}

func (s *Settings) SetUITimeFormat(ctx context.Context, v string) error {
	return s.setEnum(ctx, keyUITimeFormat, v, "24h", "12h")
}

// UITheme is dark or light. Default dark.
func (s *Settings) UITheme(ctx context.Context) (string, error) {
	return s.enumString(ctx, keyUITheme, "dark", "dark", "light")
}

func (s *Settings) SetUITheme(ctx context.Context, v string) error {
	return s.setEnum(ctx, keyUITheme, v, "dark", "light")
}

// NotifyDailySummary reports whether the midnight summary is sent. Default off.
func (s *Settings) NotifyDailySummary(ctx context.Context) (bool, error) {
	return s.boolKey(ctx, keyNotifyDailySummary, false)
}

func (s *Settings) SetNotifyDailySummary(ctx context.Context, on bool) error {
	return s.setBool(ctx, keyNotifyDailySummary, on)
}

// UpdateAuto reports whether the app updates itself. Default on.
func (s *Settings) UpdateAuto(ctx context.Context) (bool, error) {
	return s.boolKey(ctx, keyUpdateAuto, true)
}

func (s *Settings) SetUpdateAuto(ctx context.Context, on bool) error {
	return s.setBool(ctx, keyUpdateAuto, on)
}

// enumString returns the stored value when it is one of allowed, else def.
func (s *Settings) enumString(ctx context.Context, key, def string, allowed ...string) (string, error) {
	v, err := s.getString(ctx, key)
	if err != nil {
		return def, err
	}
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return def, nil
}

func (s *Settings) setEnum(ctx context.Context, key, v string, allowed ...string) error {
	for _, a := range allowed {
		if v == a {
			return s.setString(ctx, key, v)
		}
	}
	return fmt.Errorf("invalid value %q for %s", v, key)
}

func (s *Settings) boolKey(ctx context.Context, key string, def bool) (bool, error) {
	v, err := s.getString(ctx, key)
	if err != nil || v == "" {
		return def, err
	}
	return v == "1", nil
}

func (s *Settings) setBool(ctx context.Context, key string, on bool) error {
	if on {
		return s.setString(ctx, key, "1")
	}
	return s.setString(ctx, key, "0")
}

// v2 release notes

const (
	keyLatestReleaseNotes = "settings:latest_release_notes" // newline-joined highlight bullets
	keyLatestReleaseURL   = "settings:latest_release_url"   // html_url of the latest release
)

// LatestReleaseNotes returns the stored release highlights, one per line.
func (s *Settings) LatestReleaseNotes(ctx context.Context) ([]string, error) {
	v, err := s.getString(ctx, keyLatestReleaseNotes)
	if err != nil || v == "" {
		return nil, err
	}
	return strings.Split(v, "\n"), nil
}

func (s *Settings) SetLatestReleaseNotes(ctx context.Context, notes []string) error {
	return s.setString(ctx, keyLatestReleaseNotes, strings.Join(notes, "\n"))
}

// LatestReleaseURL is the GitHub page of the latest release. Empty when unset.
func (s *Settings) LatestReleaseURL(ctx context.Context) (string, error) {
	return s.getString(ctx, keyLatestReleaseURL)
}

func (s *Settings) SetLatestReleaseURL(ctx context.Context, url string) error {
	return s.setString(ctx, keyLatestReleaseURL, strings.TrimSpace(url))
}
