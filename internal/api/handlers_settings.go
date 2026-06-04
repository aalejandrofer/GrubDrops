package api

import (
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"

	"github.com/chano-fernandez/rust-drops-miner/internal/store"
)

type settingsDeps struct {
	s        *store.Settings
	t        Renderer
	sm       *scs.SessionManager
	onUpdate func()
}

type settingsPageData struct {
	GlobalDiscordWebhook string
	LogRetentionDays     int
}

func (d *settingsDeps) get(w http.ResponseWriter, r *http.Request) {
	url, _ := d.s.GlobalDiscordWebhook(r.Context())
	days, _ := d.s.LogRetentionDays(r.Context())
	flash := d.sm.PopString(r.Context(), "flash")
	render(w, d.t, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page:  settingsPageData{GlobalDiscordWebhook: url, LogRetentionDays: days},
		Flash: flash,
	})
}

func (d *settingsDeps) post(w http.ResponseWriter, r *http.Request) {
	url := r.FormValue("discord_webhook")
	if err := d.s.SetGlobalDiscordWebhook(r.Context(), url); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if v := r.FormValue("log_retention_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			_ = d.s.SetLogRetentionDays(r.Context(), n)
		}
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(r.Context(), "flash", "settings saved")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
