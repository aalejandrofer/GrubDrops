package api

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/dropsminer/internal/store"
)

type settingsDeps struct {
	s        *store.Settings
	t        Renderer
	sm       *scs.SessionManager
	onUpdate func()
	// status fields surfaced read-only on the settings page
	startedAt   time.Time
	logLevelEnv string
	browserURL  string
	gitCommit   string
	version     string
}

type settingsPageData struct {
	GlobalDiscordWebhook string
	LogRetentionDays     int
	LogLevel             string // empty = use env default
	LogLevelEnv          string
	TickIntervalMs       int
	DiscoveryIntervalSec int
	NotifyClaim          bool
	NotifyProgress       bool
	NotifyAuth           bool
	NotifyError          bool

	// Read-only diagnostics
	Uptime       string
	GoVersion    string
	Goroutines   int
	BrowserURL   string
	GitCommit    string
	Version      string
}

func (d *settingsDeps) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	url, _ := d.s.GlobalDiscordWebhook(ctx)
	days, _ := d.s.LogRetentionDays(ctx)
	level, _ := d.s.LogLevel(ctx)
	tick, _ := d.s.TickIntervalMs(ctx)
	discIv, _ := d.s.DiscoveryIntervalSec(ctx)
	nc, np, na, ne := d.s.NotifyKinds(ctx)

	uptime := ""
	if !d.startedAt.IsZero() {
		uptime = formatUptime(time.Since(d.startedAt))
	}

	flash := d.sm.PopString(ctx, "flash")
	render(w, d.t, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "settings",
		Page: settingsPageData{
			GlobalDiscordWebhook: url,
			LogRetentionDays:     days,
			LogLevel:             level,
			LogLevelEnv:          d.logLevelEnv,
			TickIntervalMs:       tick,
			DiscoveryIntervalSec: discIv,
			NotifyClaim:          nc,
			NotifyProgress:       np,
			NotifyAuth:           na,
			NotifyError:          ne,
			Uptime:               uptime,
			GoVersion:            runtime.Version(),
			Goroutines:           runtime.NumGoroutine(),
			BrowserURL:           d.browserURL,
			GitCommit:            d.gitCommit,
			Version:              d.version,
		},
		Flash: flash,
	})
}

func (d *settingsDeps) post(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	url := r.FormValue("discord_webhook")
	if err := d.s.SetGlobalDiscordWebhook(ctx, url); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if v := r.FormValue("log_retention_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			_ = d.s.SetLogRetentionDays(ctx, n)
		}
	}
	_ = d.s.SetLogLevel(ctx, r.FormValue("log_level"))
	if v := r.FormValue("tick_interval_ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			_ = d.s.SetTickIntervalMs(ctx, n)
		}
	}
	if v := r.FormValue("discovery_interval_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			_ = d.s.SetDiscoveryIntervalSec(ctx, n)
		}
	}
	on := func(name string) bool { return r.FormValue(name) == "1" }
	_ = d.s.SetNotifyKinds(ctx, on("notify_claim"), on("notify_progress"), on("notify_auth"), on("notify_error"))

	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(ctx, "flash", "settings saved — restart container to apply tick/discovery intervals")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
