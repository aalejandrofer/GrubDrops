package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/notify"
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
	b.Notify.Claim, _, b.Notify.Auth, b.Notify.Errors, _ = st.NotifyKinds(ctx)
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
	} `json:"notify"`
	UpdateAuto *bool `json:"update_auto"`
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
		(req.DiscordWebhook == nil || validWebhook(*req.DiscordWebhook)) &&
		(req.TelegramBotToken == nil || validOptional(telegramTokenRe, *req.TelegramBotToken)) &&
		(req.TelegramChatID == nil || validOptional(telegramChatRe, *req.TelegramChatID))
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
	if n := req.Notify; n != nil && (n.Claim != nil || n.Auth != nil || n.Errors != nil) {
		claim, progress, auth, errs, canary := st.NotifyKinds(ctx)
		if n.Claim != nil {
			claim = *n.Claim
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
		return st.SetUpdateAuto(ctx, *req.UpdateAuto)
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
