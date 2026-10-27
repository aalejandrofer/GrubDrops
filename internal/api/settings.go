package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/netutil"
	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/timeutil"
)

var webhookPrefixes = []string{
	"https://discord.com/api/webhooks/",
	"https://discordapp.com/api/webhooks/",
}

var (
	telegramTokenRe = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
	telegramChatRe  = regexp.MustCompile(`^(-?[0-9]+|@[A-Za-z0-9_]{5,})$`)
)

func (s *server) routesSettings(r chi.Router) {
	r.Get("/settings", s.getSettings)
	r.Put("/settings", s.putSettings)
	r.Post("/settings/notify-test", s.notifyTest)
	r.Post("/settings/proxy-test", s.proxyTest)
}

type settingsDiscord struct {
	Configured  bool   `json:"configured"`
	WebhookHint string `json:"webhook_hint"`
}

type settingsTelegram struct {
	Configured bool   `json:"configured"`
	TokenHint  string `json:"token_hint"`
	ChatID     string `json:"chat_id"`
}

type settingsNotify struct {
	Claim        bool `json:"claim"`
	Auth         bool `json:"auth"`
	Errors       bool `json:"errors"`
	DailySummary bool `json:"daily_summary"`
	Progress     bool `json:"progress"`
	Canary       bool `json:"canary"`
}

// settingsProxy is the outbound proxy. URL hides any password.
type settingsProxy struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type settingsCanary struct {
	TwitchChannel string `json:"twitch_channel"`
	KickChannel   string `json:"kick_channel"`
	IntervalSec   int    `json:"interval_sec"` // 0 = off
}

type settingsBody struct {
	Language   string `json:"language"`
	TimeFormat string `json:"time_format"`
	Theme      string `json:"theme"`
	Timezone   string `json:"timezone"`
	// ZoneEffective is the zone the server resolves; Local is reported as UTC.
	ZoneEffective string           `json:"zone_effective"`
	Discord       settingsDiscord  `json:"discord"`
	Telegram      settingsTelegram `json:"telegram"`
	Notify        settingsNotify   `json:"notify"`
	UpdateAuto    bool             `json:"update_auto"`
	PriorityMode  string           `json:"priority_mode"`
	// ScanIntervalMin is how often accounts check for new campaigns.
	ScanIntervalMin int  `json:"scan_interval_min"`
	Preempt         bool `json:"preempt"`
	// Proxy, KickWatchMode and the canary interval apply after a restart.
	Proxy           settingsProxy  `json:"proxy"`
	KickWatchMode   string         `json:"kick_watch_mode"`
	NotifyAvatarURL string         `json:"notify_avatar_url"`
	Canary          settingsCanary `json:"canary"`
}

var (
	proxySchemes = []string{"http", "https", "socks5"}
	channelRe    = regexp.MustCompile(`^[A-Za-z0-9_]{0,25}$`)
)

// validProxy accepts an empty value, which clears the proxy.
func validProxy(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	u, err := url.Parse(v)
	return err == nil && oneOf(u.Scheme, proxySchemes...) && u.Host != ""
}

func validAvatar(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	u, err := url.Parse(v)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// webhookHint keeps the URL up to /webhooks/ and hides the id and token.
func webhookHint(url string) string {
	const marker = "/webhooks/"
	if i := strings.Index(url, marker); i >= 0 {
		return url[:i+len(marker)] + "••••"
	}
	return "••••"
}

// tokenHint keeps the bot id before the colon and hides the secret.
func tokenHint(token string) string {
	if i := strings.IndexByte(token, ':'); i > 0 {
		return token[:i+1] + "••••"
	}
	return "••••"
}

// telegramEffective is the stored Telegram token and chat ID, each falling
// back to its env default.
func (s *server) telegramEffective(ctx context.Context) (token, chat string, storedToken bool, err error) {
	st := s.d.Settings
	if token, err = st.TelegramBotToken(ctx); err != nil {
		return "", "", false, err
	}
	storedToken = token != ""
	if token == "" {
		token = s.d.TelegramTokenFallback
	}
	if chat, err = st.TelegramChatID(ctx); err != nil {
		return "", "", false, err
	}
	if chat == "" {
		chat = s.d.TelegramChatFallback
	}
	return token, chat, storedToken, nil
}

func (s *server) loadSettings(ctx context.Context) (settingsBody, error) {
	st := s.d.Settings
	var b settingsBody
	var err error
	if b.Language, err = st.UILanguage(ctx); err != nil {
		return b, err
	}
	if b.TimeFormat, err = st.UITimeFormat(ctx); err != nil {
		return b, err
	}
	if b.Theme, err = st.UITheme(ctx); err != nil {
		return b, err
	}
	if b.Timezone, err = st.Timezone(ctx); err != nil {
		return b, err
	}
	if b.PriorityMode, err = st.PriorityMode(ctx); err != nil {
		return b, err
	}
	if b.ScanIntervalMin, err = st.ScanIntervalMin(ctx); err != nil {
		return b, err
	}
	if b.Preempt, err = st.Preempt(ctx); err != nil {
		return b, err
	}
	if b.Proxy.Enabled, err = st.ProxyEnabled(ctx); err != nil {
		return b, err
	}
	proxy, err := st.ProxyURL(ctx)
	if err != nil {
		return b, err
	}
	b.Proxy.URL = netutil.MaskProxyURL(proxy)
	if b.KickWatchMode, err = st.KickWatchMode(ctx); err != nil {
		return b, err
	}
	if b.NotifyAvatarURL, err = st.NotifyAvatarURL(ctx); err != nil {
		return b, err
	}
	if b.Canary.TwitchChannel, err = st.CanaryTwitchChannel(ctx); err != nil {
		return b, err
	}
	if b.Canary.KickChannel, err = st.CanaryKickChannel(ctx); err != nil {
		return b, err
	}
	if b.Canary.IntervalSec, err = st.CanaryIntervalSec(ctx); err != nil {
		return b, err
	}
	b.ZoneEffective = s.overviewZone().String()
	if b.ZoneEffective == "Local" {
		b.ZoneEffective = "UTC"
	}
	hook, err := st.GlobalDiscordWebhook(ctx)
	if err != nil {
		return b, err
	}
	b.Discord.Configured = hook != "" || s.d.WebhookFallback != ""
	if hook != "" {
		b.Discord.WebhookHint = webhookHint(hook)
	}
	token, chat, storedToken, err := s.telegramEffective(ctx)
	if err != nil {
		return b, err
	}
	b.Telegram.Configured = token != "" && chat != ""
	if storedToken {
		b.Telegram.TokenHint = tokenHint(token)
	}
	b.Telegram.ChatID = chat
	b.Notify.Claim, b.Notify.Progress, b.Notify.Auth, b.Notify.Errors, b.Notify.Canary = st.NotifyKinds(ctx)
	if b.Notify.DailySummary, err = st.NotifyDailySummary(ctx); err != nil {
		return b, err
	}
	b.UpdateAuto, err = st.UpdateAuto(ctx)
	return b, err
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	b, err := s.loadSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, b)
}

type settingsPut struct {
	Language   *string `json:"language"`
	TimeFormat *string `json:"time_format"`
	Theme      *string `json:"theme"`
	Timezone   *string `json:"timezone"`
	// PriorityMode is ordered or ending_soonest.
	PriorityMode *string `json:"priority_mode"`
	// ZoneEffective is accepted so a client can send the GET body back. Ignored.
	ZoneEffective json.RawMessage `json:"zone_effective"`
	// Discord is accepted so a client can send the GET body back. Ignored.
	Discord        json.RawMessage `json:"discord"`
	DiscordWebhook *string         `json:"discord_webhook"`
	// Telegram is accepted so a client can send the GET body back. Ignored.
	Telegram         json.RawMessage `json:"telegram"`
	TelegramBotToken *string         `json:"telegram_bot_token"`
	TelegramChatID   *string         `json:"telegram_chat_id"`
	Notify           *struct {
		Claim        *bool `json:"claim"`
		Auth         *bool `json:"auth"`
		Errors       *bool `json:"errors"`
		DailySummary *bool `json:"daily_summary"`
		Progress     *bool `json:"progress"`
		Canary       *bool `json:"canary"`
	} `json:"notify"`
	UpdateAuto      *bool `json:"update_auto"`
	ScanIntervalMin *int  `json:"scan_interval_min"`
	Preempt         *bool `json:"preempt"`
	// Proxy is accepted so a client can send the GET body back. Ignored.
	Proxy           json.RawMessage `json:"proxy"`
	ProxyEnabled    *bool           `json:"proxy_enabled"`
	ProxyURL        *string         `json:"proxy_url"` // the GET hint itself = unchanged
	KickWatchMode   *string         `json:"kick_watch_mode"`
	NotifyAvatarURL *string         `json:"notify_avatar_url"`
	Canary          *struct {
		TwitchChannel *string `json:"twitch_channel"`
		KickChannel   *string `json:"kick_channel"`
		IntervalSec   *int    `json:"interval_sec"`
	} `json:"canary"`
}

// validCanary accepts channel names and an interval of 0 (off) or 5 minutes
// to 7 days.
func validCanary(c *struct {
	TwitchChannel *string `json:"twitch_channel"`
	KickChannel   *string `json:"kick_channel"`
	IntervalSec   *int    `json:"interval_sec"`
}) bool {
	if c == nil {
		return true
	}
	return (c.TwitchChannel == nil || channelRe.MatchString(strings.TrimSpace(*c.TwitchChannel))) &&
		(c.KickChannel == nil || channelRe.MatchString(strings.TrimSpace(*c.KickChannel))) &&
		(c.IntervalSec == nil || *c.IntervalSec == 0 || (*c.IntervalSec >= 300 && *c.IntervalSec <= 7*24*3600))
}

func oneOf(v string, ok ...string) bool {
	for _, o := range ok {
		if v == o {
			return true
		}
	}
	return false
}

func validWebhook(v string) bool {
	if v == "" {
		return true
	}
	for _, p := range webhookPrefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}

// validOptional accepts an empty value, which clears the setting.
func validOptional(re *regexp.Regexp, v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || re.MatchString(v)
}

func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsPut
	if !readJSON(w, r, &req) {
		return
	}
	valid := (req.Language == nil || oneOf(*req.Language, "en", "es", "zh-CN")) &&
		(req.TimeFormat == nil || oneOf(*req.TimeFormat, "24h", "12h")) &&
		(req.Theme == nil || oneOf(*req.Theme, "dark", "light")) &&
		(req.Timezone == nil || *req.Timezone == "" || validZone(*req.Timezone)) &&
		(req.PriorityMode == nil || oneOf(*req.PriorityMode, "ordered", "ending_soonest")) &&
		(req.DiscordWebhook == nil || validWebhook(*req.DiscordWebhook)) &&
		(req.TelegramBotToken == nil || validOptional(telegramTokenRe, *req.TelegramBotToken)) &&
		(req.TelegramChatID == nil || validOptional(telegramChatRe, *req.TelegramChatID)) &&
		(req.ScanIntervalMin == nil || slices.Contains(store.ScanIntervals, *req.ScanIntervalMin)) &&
		(req.ProxyURL == nil || validProxy(*req.ProxyURL)) &&
		(req.KickWatchMode == nil || oneOf(*req.KickWatchMode, store.KickWatchModeBrowser, store.KickWatchModeWS, store.KickWatchModeAuto)) &&
		(req.NotifyAvatarURL == nil || validAvatar(*req.NotifyAvatarURL)) &&
		validCanary(req.Canary)
	if !valid {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}

	ctx := r.Context()
	st := s.d.Settings
	oldZone, err := st.Timezone(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	oldPriority, err := st.PriorityMode(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	oldScan, err := st.ScanIntervalMin(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := s.applySettings(ctx, req); err != nil {
		slog.Warn("settings save", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.OnSettingsUpdate != nil {
		s.d.OnSettingsUpdate()
	}
	if req.Timezone != nil && *req.Timezone != oldZone && s.d.SetZone != nil {
		s.d.SetZone(*req.Timezone)
	}
	// Runners read the mode at each plan: rescan so the queue reorders now. A
	// new scan pace re-arms the idle waits the same way.
	replan := (req.PriorityMode != nil && *req.PriorityMode != oldPriority) ||
		(req.ScanIntervalMin != nil && *req.ScanIntervalMin != oldScan)
	if replan && s.d.Pool != nil {
		s.d.Pool.RescanAll()
	}
	s.getSettings(w, r)
}

func validZone(name string) bool {
	_, err := time.LoadLocation(name)
	return err == nil && timeutil.Valid(name)
}

func (s *server) applySettings(ctx context.Context, req settingsPut) error {
	st := s.d.Settings
	if req.Language != nil {
		if err := st.SetUILanguage(ctx, *req.Language); err != nil {
			return err
		}
	}
	if req.TimeFormat != nil {
		if err := st.SetUITimeFormat(ctx, *req.TimeFormat); err != nil {
			return err
		}
	}
	if req.Theme != nil {
		if err := st.SetUITheme(ctx, *req.Theme); err != nil {
			return err
		}
	}
	if req.Timezone != nil {
		if err := st.SetTimezone(ctx, *req.Timezone); err != nil {
			return err
		}
	}
	if req.PriorityMode != nil {
		if err := st.SetPriorityMode(ctx, *req.PriorityMode); err != nil {
			return err
		}
	}
	if req.DiscordWebhook != nil {
		if err := st.SetGlobalDiscordWebhook(ctx, *req.DiscordWebhook); err != nil {
			return err
		}
	}
	if req.TelegramBotToken != nil {
		if err := st.SetTelegramBotToken(ctx, *req.TelegramBotToken); err != nil {
			return err
		}
	}
	if req.TelegramChatID != nil {
		if err := st.SetTelegramChatID(ctx, *req.TelegramChatID); err != nil {
			return err
		}
	}
	if n := req.Notify; n != nil && (n.Claim != nil || n.Auth != nil || n.Errors != nil || n.Progress != nil || n.Canary != nil) {
		claim, progress, auth, errs, canary := st.NotifyKinds(ctx)
		if n.Claim != nil {
			claim = *n.Claim
		}
		if n.Progress != nil {
			progress = *n.Progress
		}
		if n.Canary != nil {
			canary = *n.Canary
		}
		if n.Auth != nil {
			auth = *n.Auth
		}
		if n.Errors != nil {
			errs = *n.Errors
		}
		if err := st.SetNotifyKinds(ctx, claim, progress, auth, errs, canary); err != nil {
			return err
		}
	}
	if n := req.Notify; n != nil && n.DailySummary != nil {
		if err := st.SetNotifyDailySummary(ctx, *n.DailySummary); err != nil {
			return err
		}
	}
	if req.UpdateAuto != nil {
		if err := st.SetUpdateAuto(ctx, *req.UpdateAuto); err != nil {
			return err
		}
	}
	return s.applyEngineSettings(ctx, req)
}

// applyEngineSettings saves the scan, proxy, Kick and canary settings.
func (s *server) applyEngineSettings(ctx context.Context, req settingsPut) error {
	st := s.d.Settings
	if req.ScanIntervalMin != nil {
		if err := st.SetScanIntervalMin(ctx, *req.ScanIntervalMin); err != nil {
			return err
		}
	}
	if req.Preempt != nil {
		if err := st.SetPreempt(ctx, *req.Preempt); err != nil {
			return err
		}
	}
	if req.ProxyEnabled != nil {
		if err := st.SetProxyEnabled(ctx, *req.ProxyEnabled); err != nil {
			return err
		}
	}
	if req.ProxyURL != nil {
		cur, err := st.ProxyURL(ctx)
		if err != nil {
			return err
		}
		// A URL still showing the masked password keeps the saved one.
		if v := netutil.RestoreProxyPassword(*req.ProxyURL, cur); v != cur {
			if err := st.SetProxyURL(ctx, v); err != nil {
				return err
			}
		}
	}
	if req.KickWatchMode != nil {
		if err := st.SetKickWatchMode(ctx, *req.KickWatchMode); err != nil {
			return err
		}
	}
	if req.NotifyAvatarURL != nil {
		if err := st.SetNotifyAvatarURL(ctx, strings.TrimSpace(*req.NotifyAvatarURL)); err != nil {
			return err
		}
	}
	if c := req.Canary; c != nil {
		if c.TwitchChannel != nil {
			if err := st.SetCanaryTwitchChannel(ctx, *c.TwitchChannel); err != nil {
				return err
			}
		}
		if c.KickChannel != nil {
			if err := st.SetCanaryKickChannel(ctx, *c.KickChannel); err != nil {
				return err
			}
		}
		if c.IntervalSec != nil {
			if err := st.SetCanaryIntervalSec(ctx, *c.IntervalSec); err != nil {
				return err
			}
		}
	}
	return nil
}

// notifyTest sends one sample claim through the live notifier. The optional
// target query parameter, discord or telegram, limits it to one service.
func (s *server) notifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	target := r.URL.Query().Get("target")
	if target != "" && target != notify.TargetDiscord && target != notify.TargetTelegram {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	hook, err := s.d.Settings.GlobalDiscordWebhook(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	token, chat, _, err := s.telegramEffective(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	discordOn := hook != "" || s.d.WebhookFallback != ""
	telegramOn := token != "" && chat != ""
	configured := discordOn || telegramOn
	switch target {
	case notify.TargetDiscord:
		configured = discordOn
	case notify.TargetTelegram:
		configured = telegramOn
	}
	if !configured {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if target != "" {
		ctx = notify.WithTarget(ctx, target)
	}
	if s.d.Notifier == nil {
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	sample := map[string]any{
		"platform": "twitch",
		"game":     "GrubDrops Test",
		"campaign": "Test Campaign",
		"drop":     "Sample Reward",
		"channel":  "grubdrops",
		"cur_min":  60,
		"req_min":  60,
	}
	if err := s.d.Notifier.Notify(ctx, notify.EventTest, sample); err != nil {
		slog.Warn("notify test failed", "kind", "error", "err", err)
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// proxyTestURL is the page proxyTest fetches through the proxy.
var proxyTestURL = "https://www.twitch.tv/"

// proxyTest fetches proxyTestURL through a proxy. The body's url is tried
// when set and not the saved URL's hint, else the saved URL. It does not save.
func (s *server) proxyTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	saved, err := s.d.Settings.ProxyURL(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	target := netutil.RestoreProxyPassword(strings.TrimSpace(req.URL), saved)
	if target == "" {
		target = saved
	}
	if target == "" || !validProxy(target) {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	dial, err := netutil.ProxyDialer(target)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	// The dialer tunnels the connection through the proxy, as the backends do.
	tr := &http.Transport{DialContext: dial}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(tctx, http.MethodGet, proxyTestURL, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	start := time.Now()
	resp, err := client.Do(hreq)
	if err != nil {
		slog.Info("proxy test failed", "kind", "error", "err", err)
		writeErr(w, http.StatusBadGateway, "proxy_failed")
		return
	}
	_ = resp.Body.Close()
	writeJSON(w, http.StatusOK, map[string]any{"status": resp.StatusCode, "ms": time.Since(start).Milliseconds()})
}
