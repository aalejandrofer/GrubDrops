package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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

func (s *server) routesSettings(r chi.Router) {
	r.Get("/settings", s.getSettings)
	r.Put("/settings", s.putSettings)
	r.Post("/settings/notify-test", s.notifyTest)
}

type settingsDiscord struct {
	Configured  bool   `json:"configured"`
	WebhookHint string `json:"webhook_hint"`
}

type settingsNotify struct {
	Claim        bool `json:"claim"`
	Auth         bool `json:"auth"`
	Errors       bool `json:"errors"`
	DailySummary bool `json:"daily_summary"`
}

type settingsBody struct {
	Language   string          `json:"language"`
	TimeFormat string          `json:"time_format"`
	Theme      string          `json:"theme"`
	Timezone   string          `json:"timezone"`
	Discord    settingsDiscord `json:"discord"`
	Notify     settingsNotify  `json:"notify"`
	UpdateAuto bool            `json:"update_auto"`
}

// webhookHint keeps the URL up to /webhooks/ and hides the id and token.
func webhookHint(url string) string {
	const marker = "/webhooks/"
	if i := strings.Index(url, marker); i >= 0 {
		return url[:i+len(marker)] + "••••"
	}
	return "••••"
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
	hook, err := st.GlobalDiscordWebhook(ctx)
	if err != nil {
		return b, err
	}
	if hook != "" {
		b.Discord = settingsDiscord{Configured: true, WebhookHint: webhookHint(hook)}
	}
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
	// Discord is accepted so a client can send the GET body back. Ignored.
	Discord        json.RawMessage `json:"discord"`
	DiscordWebhook *string         `json:"discord_webhook"`
	Notify         *struct {
		Claim        *bool `json:"claim"`
		Auth         *bool `json:"auth"`
		Errors       *bool `json:"errors"`
		DailySummary *bool `json:"daily_summary"`
	} `json:"notify"`
	UpdateAuto *bool `json:"update_auto"`
}

func in(v string, ok ...string) bool {
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

func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsPut
	if !readJSON(w, r, &req) {
		return
	}
	valid := (req.Language == nil || in(*req.Language, "en", "es", "zh-CN")) &&
		(req.TimeFormat == nil || in(*req.TimeFormat, "24h", "12h")) &&
		(req.Theme == nil || in(*req.Theme, "dark", "light")) &&
		(req.Timezone == nil || *req.Timezone == "" || validZone(*req.Timezone)) &&
		(req.DiscordWebhook == nil || validWebhook(*req.DiscordWebhook))
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
	if n := req.Notify; n != nil {
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
		if n.DailySummary != nil {
			if err := st.SetNotifyDailySummary(ctx, *n.DailySummary); err != nil {
				return err
			}
		}
	}
	if req.UpdateAuto != nil {
		return st.SetUpdateAuto(ctx, *req.UpdateAuto)
	}
	return nil
}

// notifyTest sends one sample claim through the live notifier.
func (s *server) notifyTest(w http.ResponseWriter, r *http.Request) {
	hook, err := s.d.Settings.GlobalDiscordWebhook(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if hook == "" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
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
	if err := s.d.Notifier.Notify(r.Context(), notify.EventTest, sample); err != nil {
		slog.Warn("notify test failed", "kind", "error", "err", err)
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
