package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type settingsDeps struct {
	s        *store.Settings
	q        *gen.Queries // for inline accounts table
	sch      *scheduler.Scheduler
	t        Renderer
	sm       *scs.SessionManager
	onUpdate func()
	// reload re-spins the scheduler so whitelist/priority POSTs take
	// effect without the operator clicking "Apply changes" first.
	reload func(context.Context) error
	// notifier is the live Discord notifier, for the "send test" button.
	notifier Notifier
	// status fields surfaced read-only on the settings page
	startedAt   time.Time
	logLevelEnv string
	browserURL  string
	gitCommit   string
	version     string
	// oidc is the configured SSO provider, surfaced read-only on settings.
	// Nil or disabled renders the "not configured" state.
	oidc *oidc.Provider
}

type settingsAccountRow struct {
	ID          string
	DisplayName string
	Platform    string
	Login       string
	Status      string
	StatusClass string
}

type settingsGameRow struct {
	ID       string
	Name     string
	Selected bool
}

type settingsOIDC struct {
	Enabled       bool
	ProviderName  string
	Issuer        string
	CallbackURL   string
	AllowedEmails []string
	AllowedGroups []string
}

type settingsPageData struct {
	GlobalDiscordWebhook string
	NotifyAvatarURL      string
	LogRetentionDays     int
	LogLevel             string // empty = use env default
	LogLevelEnv          string
	TickIntervalSec      int
	DiscoveryIntervalMin int
	HeartbeatIntervalSec int
	PriorityMode         string // "ordered" | "ending_soonest"
	NotifyClaim          bool
	NotifyProgress       bool
	NotifyAuth           bool
	NotifyError          bool
	ProgressNotifyStep   int // milestone % step for progress notifications (0 = off)

	// Global priority list — used as fallback when an account has no
	// per-account whitelist rows.
	GlobalGames []settingsGameRow
	AllGames    []settingsGameRow // pool for the picker; .Selected marks ones in GlobalGames

	// Read-only diagnostics
	Uptime     string
	GoVersion  string
	Goroutines int
	BrowserURL string
	GitCommit  string
	Version    string
	// Read-only SSO status
	OIDC settingsOIDC
}

func (d *settingsDeps) renderTab(w http.ResponseWriter, r *http.Request, active string) {
	ctx := r.Context()
	url, _ := d.s.GlobalDiscordWebhook(ctx)
	avatarURL, _ := d.s.NotifyAvatarURL(ctx)
	days, _ := d.s.LogRetentionDays(ctx)
	level, _ := d.s.LogLevel(ctx)
	tick, _ := d.s.TickIntervalSec(ctx)
	discIv, _ := d.s.DiscoveryIntervalMin(ctx)
	hbSec, _ := d.s.HeartbeatIntervalSec(ctx)
	prio, _ := d.s.PriorityMode(ctx)
	nc, np, na, ne := d.s.NotifyKinds(ctx)
	progStep, _ := d.s.ProgressNotifyStepPct(ctx)

	uptime := ""
	if !d.startedAt.IsZero() {
		uptime = formatUptime(time.Since(d.startedAt))
	}

	flash := d.sm.PopString(ctx, "flash")
	// Build global priority list + game pool. Failures are non-fatal:
	// settings page still renders with empty lists.
	var globalGames, allGames []settingsGameRow
	selected := map[string]bool{}
	if d.q != nil {
		if rows, err := d.q.ListGlobalGames(ctx); err == nil {
			for _, g := range rows {
				globalGames = append(globalGames, settingsGameRow{ID: g.ID, Name: g.Name, Selected: true})
				selected[g.ID] = true
			}
		}
		if all, err := d.q.ListAllGames(ctx); err == nil {
			for _, g := range all {
				allGames = append(allGames, settingsGameRow{ID: g.ID, Name: g.Name, Selected: selected[g.ID]})
			}
		}
	}
	var ssoStatus settingsOIDC
	if d.oidc != nil && d.oidc.Enabled() {
		ssoStatus = settingsOIDC{
			Enabled:       true,
			ProviderName:  d.oidc.Name(),
			Issuer:        d.oidc.Issuer(),
			CallbackURL:   d.oidc.RedirectURL(),
			AllowedEmails: d.oidc.AllowedEmails(),
			AllowedGroups: d.oidc.AllowedGroups(),
		}
	}
	render(w, d.t, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: active,
		Page: settingsPageData{
			GlobalDiscordWebhook: url,
			NotifyAvatarURL:      avatarURL,
			LogRetentionDays:     days,
			LogLevel:             level,
			LogLevelEnv:          d.logLevelEnv,
			TickIntervalSec:      tick,
			DiscoveryIntervalMin: discIv,
			HeartbeatIntervalSec: hbSec,
			PriorityMode:         prio,
			GlobalGames:          globalGames,
			AllGames:             allGames,
			NotifyClaim:          nc,
			NotifyProgress:       np,
			NotifyAuth:           na,
			NotifyError:          ne,
			ProgressNotifyStep:   progStep,
			Uptime:               uptime,
			GoVersion:            runtime.Version(),
			Goroutines:           runtime.NumGoroutine(),
			BrowserURL:           d.browserURL,
			GitCommit:            d.gitCommit,
			Version:              d.version,
			OIDC:                 ssoStatus,
		},
		Flash: flash,
	})
}

func (d *settingsDeps) get(w http.ResponseWriter, r *http.Request) { d.renderTab(w, r, "settings") }
func (d *settingsDeps) getPriority(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "priority")
}
func (d *settingsDeps) getNotifications(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "notifications")
}
func (d *settingsDeps) getSecurity(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "security")
}

// postGeneral saves the General tab: tick/discovery intervals + logging.
func (d *settingsDeps) postGeneral(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if v := r.FormValue("log_retention_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			_ = d.s.SetLogRetentionDays(ctx, n)
		}
	}
	_ = d.s.SetLogLevel(ctx, r.FormValue("log_level"))

	intervalsChanged := false
	if v := r.FormValue("tick_interval_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.TickIntervalSec(ctx); cur != n {
				intervalsChanged = true
			}
			_ = d.s.SetTickIntervalSec(ctx, n)
		}
	}
	if v := r.FormValue("discovery_interval_min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.DiscoveryIntervalMin(ctx); cur != n {
				intervalsChanged = true
			}
			_ = d.s.SetDiscoveryIntervalMin(ctx, n)
		}
	}
	if v := r.FormValue("heartbeat_interval_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.HeartbeatIntervalSec(ctx); cur != n {
				intervalsChanged = true
			}
			_ = d.s.SetHeartbeatIntervalSec(ctx, n)
		}
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	msg := "settings saved"
	if intervalsChanged {
		msg = "settings saved — reload watchers (or restart) to apply the new cadence"
	}
	d.sm.Put(ctx, "flash", msg)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// postNotifications saves the Notifications tab: webhook, avatar, notify kinds.
func (d *settingsDeps) postNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := d.s.SetGlobalDiscordWebhook(ctx, r.FormValue("discord_webhook")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = d.s.SetNotifyAvatarURL(ctx, strings.TrimSpace(r.FormValue("notify_avatar_url")))
	on := func(name string) bool { return r.FormValue(name) == "1" }
	_ = d.s.SetNotifyKinds(ctx, on("notify_claim"), on("notify_progress"), on("notify_auth"), on("notify_error"))
	stepChanged := false
	if v := r.FormValue("progress_notify_step"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			if cur, _ := d.s.ProgressNotifyStepPct(ctx); cur != n {
				stepChanged = true
			}
			_ = d.s.SetProgressNotifyStepPct(ctx, n)
		}
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	msg := "notifications saved"
	if stepChanged {
		// The step is read by each watcher at build time, so it applies on
		// the next scheduler reload, not live like the on/off toggles.
		msg = "notifications saved — reload watchers to apply the progress milestone step"
	}
	d.sm.Put(ctx, "flash", msg)
	http.Redirect(w, r, "/settings/notifications", http.StatusSeeOther)
}

// postPriorityMode saves the Drop Priority tab's mode selector.
func (d *settingsDeps) postPriorityMode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = d.s.SetPriorityMode(ctx, r.FormValue("priority_mode"))
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(ctx, "flash", "priority mode saved")
	http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
}

// notifyTest fires one representative sample event through the live notifier
// so the operator can confirm the webhook + embed look right. Returns a small
// HTMX fragment (not a redirect) reporting success or the error.
func (d *settingsDeps) notifyTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx := r.Context()
	writeResult := func(ok bool, msg string) {
		cls := "ok"
		if !ok {
			cls = "err"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<span class="notify-test-result ` + cls + `">` + htmlEscape(msg) + `</span>`))
	}

	if d.notifier == nil {
		slog.Warn("notify test: no notifier wired", "kind", "notify")
		writeResult(false, "no notifier configured")
		return
	}
	// A claim-shaped sample: rich fields so the rendered embed mirrors a real
	// notification.
	sample := map[string]any{
		"platform": "twitch",
		"game":     "GrubDrops Test",
		"campaign": "Test Campaign",
		"drop":     "Sample Reward",
		"channel":  "grubdrops",
		"cur_min":  60,
		"req_min":  60,
	}
	// Resolve a real target so we never silently no-op into the Noop notifier.
	// Prefer the global webhook (no account field → routes to global). If no
	// global is set, attach the first account that has its own webhook so the
	// router sends there. With neither, report honestly.
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
			slog.Warn("notify test: no webhook configured", "kind", "notify")
			writeResult(false, "no webhook configured — set a global or per-account webhook and Save first")
			return
		}
	}

	slog.Info("notify test firing", "kind", "notify", "target", target)
	// "test" event — the verbosity filter always allows it, so a manual test
	// delivers even when the user has every real notification kind toggled off.
	if err := d.notifier.Notify(ctx, "test", sample); err != nil {
		slog.Warn("notify test failed", "kind", "error", "target", target, "err", err)
		writeResult(false, "failed: "+err.Error())
		return
	}
	slog.Info("notify test sent", "kind", "notify", "target", target)
	writeResult(true, "test sent ✓ — check Discord")
}

// globalGamesAdd handles POST /settings/global-games/add — accepts a
// free-text game name, slugs it, upserts a games row, and appends to
// the global priority list at the next rank slot. Mirrors the
// per-account /accounts/:id/games/add flow so the user can pre-seed
// the global list before any campaign scrape surfaces the game.
func (d *settingsDeps) globalGamesAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if d.q == nil {
		http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
		return
	}
	slug := gameslug.Slug(name)
	if slug == "" {
		http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
		return
	}
	gameID := "g_" + slug
	if err := d.q.UpsertGame(ctx, gen.UpsertGameParams{
		ID: gameID, Name: name, Slug: slug, Priority: 0,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	existing, _ := d.q.ListGlobalGames(ctx)
	rank := int64(len(existing))
	for _, e := range existing {
		if e.ID == gameID {
			rank = e.Rank
			break
		}
	}
	if err := d.q.AddGlobalGame(ctx, gen.AddGlobalGameParams{
		GameID: gameID, Rank: rank,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.applyReload(ctx)
	d.sm.Put(ctx, "flash", "added "+name+" to global priority")
	http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
}

// changePassword handles POST /settings/password — verifies the current
// master password, then sets a new one (bcrypt). Min 8 chars; new + confirm
// must match. Flashes the outcome back to /settings.
func (d *settingsDeps) changePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cur := r.FormValue("current_password")
	nw := r.FormValue("new_password")
	cf := r.FormValue("confirm_password")
	fail := func(msg string) {
		d.sm.Put(ctx, "flash", msg)
		http.Redirect(w, r, "/settings/security", http.StatusSeeOther)
	}
	admin, err := d.q.GetAdmin(ctx)
	if err != nil {
		fail("admin not configured")
		return
	}
	if err := auth.VerifyPassword(admin.PasswordHash, cur); err != nil {
		fail("current password is wrong")
		return
	}
	if len(nw) < 8 {
		fail("new password must be at least 8 characters")
		return
	}
	if nw != cf {
		fail("new passwords do not match")
		return
	}
	hash, err := auth.HashPassword(nw)
	if err != nil {
		fail("could not hash password")
		return
	}
	if err := d.q.UpsertAdmin(ctx, gen.UpsertAdminParams{PasswordHash: hash, CreatedAt: time.Now().Unix()}); err != nil {
		fail("could not save password")
		return
	}
	d.sm.Put(ctx, "flash", "master password changed")
	http.Redirect(w, r, "/settings/security", http.StatusSeeOther)
}

// globalGamesPost handles POST /settings/global-games — replaces the
// global priority list with the form's `game_ids[]` slice. Order is
// rank; idx 0 = highest priority.
func (d *settingsDeps) globalGamesPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if d.q == nil {
		http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
		return
	}
	ids := r.Form["game_ids[]"]
	if err := d.q.ClearGlobalGames(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i, gid := range ids {
		if err := d.q.AddGlobalGame(ctx, gen.AddGlobalGameParams{
			GameID: gid, Rank: int64(i),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.applyReload(ctx)
	d.sm.Put(ctx, "flash", "global priority saved")
	http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
}

// applyReload calls the scheduler reload hook if wired. Logs but
// otherwise swallows errors so a transient reload failure doesn't
// 500 the form submit.
func (d *settingsDeps) applyReload(ctx context.Context) {
	if d.reload == nil {
		return
	}
	if err := d.reload(ctx); err != nil {
		slog.Warn("settings: scheduler reload failed after whitelist change", "err", err)
	}
}
