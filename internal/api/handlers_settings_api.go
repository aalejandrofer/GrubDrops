package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/netutil"
)

func (d *settingsDeps) apiSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.settingsViewData(r))
}

func (d *settingsDeps) apiGlobalGamesOrder(w http.ResponseWriter, r *http.Request) {
	var b struct {
		GameIDs []string `json:"game_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSetGlobalGamesOrder(r.Context(), b.GameIDs); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiGlobalGamesAdd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doAddGlobalGame(r.Context(), b.Name); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.applyReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiGeneral(w http.ResponseWriter, r *http.Request) {
	var b struct {
		LogRetentionDays     int    `json:"log_retention_days"`
		LogLevel             string `json:"log_level"`
		TickIntervalSec      int    `json:"tick_interval_sec"`
		DiscoveryIntervalMin int    `json:"discovery_interval_min"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	changed, err := d.doSaveGeneral(r.Context(), b.LogRetentionDays, b.LogLevel, b.TickIntervalSec, b.DiscoveryIntervalMin)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "intervals_changed": changed})
}

func (d *settingsDeps) apiPriorityMode(w http.ResponseWriter, r *http.Request) {
	var b struct {
		PriorityMode string `json:"priority_mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if err := d.doSetPriorityMode(r.Context(), b.PriorityMode); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiNotifications(w http.ResponseWriter, r *http.Request) {
	var b struct {
		DiscordWebhook     string `json:"discord_webhook"`
		NotifyAvatarURL    string `json:"notify_avatar_url"`
		NotifyClaim        bool   `json:"notify_claim"`
		NotifyProgress     bool   `json:"notify_progress"`
		NotifyAuth         bool   `json:"notify_auth"`
		NotifyError        bool   `json:"notify_error"`
		NotifyCanary       bool   `json:"notify_canary"`
		ProgressNotifyStep int    `json:"progress_notify_step"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSaveNotifications(r.Context(), b.DiscordWebhook, b.NotifyAvatarURL, b.NotifyClaim, b.NotifyProgress, b.NotifyAuth, b.NotifyError, b.NotifyCanary, b.ProgressNotifyStep); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiExperimental(w http.ResponseWriter, r *http.Request) {
	var b struct {
		KickWatchMode string `json:"kick_watch_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSaveExperimental(r.Context(), b.KickWatchMode); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *settingsDeps) apiProxy(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ProxyEnabled bool   `json:"proxy_enabled"`
		ProxyURL     string `json:"proxy_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := d.doSaveProxy(r.Context(), b.ProxyEnabled, b.ProxyURL); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiNotifyTest mirrors the exact action of the legacy notifyTest handler but
// returns JSON instead of an HTMX fragment.
func (d *settingsDeps) apiNotifyTest(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLang(r)
	ctx := r.Context()

	if d.notifier == nil {
		slog.Warn("api notify test: no notifier wired", "kind", "notify")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": i18n.T(lang, "notify.no_notifier")})
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
	globalURL, _ := d.s.GlobalDiscordWebhook(ctx)
	target := "global webhook"
	if globalURL == "" {
		if d.q != nil {
			if accs, err := d.q.ListEnabledAccounts(ctx); err == nil {
				for _, a := range accs {
					if a.WebhookUrl.Valid && strings.TrimSpace(a.WebhookUrl.String) != "" {
						sample["account"] = a.ID
						target = "account " + a.ID
						break
					}
				}
			}
		}
		if _, ok := sample["account"]; !ok {
			slog.Warn("api notify test: no webhook configured", "kind", "notify")
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": i18n.T(lang, "notify.no_webhook")})
			return
		}
	}

	slog.Info("api notify test firing", "kind", "notify", "target", target)
	if err := d.notifier.Notify(ctx, "test", sample); err != nil {
		slog.Warn("api notify test failed", "kind", "error", "target", target, "err", err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": i18n.T(lang, "notify.test_failed") + ": " + err.Error()})
		return
	}
	slog.Info("api notify test sent", "kind", "notify", "target", target)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiProxyTest mirrors the exact action of the legacy proxyTest handler but
// returns JSON instead of an HTMX fragment.
func (d *settingsDeps) apiProxyTest(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLang(r)
	ctx := r.Context()
	proxyURL, _ := d.s.ProxyURL(ctx)
	proxyEnabled, _ := d.s.ProxyEnabled(ctx)

	if !proxyEnabled || proxyURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": i18n.T(lang, "proxy.test_not_configured")})
		return
	}

	transport := netutil.NewTransport(proxyURL)
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	resp, err := client.Get("https://api.ipify.org?format=json")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": i18n.T(lang, "proxy.test_fail") + ": " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var result struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": i18n.T(lang, "proxy.test_fail")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ip": result.IP})
}
