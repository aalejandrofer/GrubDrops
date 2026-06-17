package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/canary"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/netutil"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type settingsDeps struct {
	loc      *time.Location // timezone for displayed times
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
	// runCanary triggers an immediate canary RunOnce. Nil disables the
	// "Run now" button.
	runCanary func(context.Context) error
	// status fields surfaced read-only on the settings page
	startedAt   time.Time
	logLevelEnv string
	browserURL  string
	sidecars    func() []string
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

// canaryView carries one platform's last accrual-canary result for the
// Health tab template. Configured=false means no channel is set for that
// platform; OK/Detail/When are only meaningful when Configured=true AND a
// result has been stored (When != "").
type canaryView struct {
	Configured bool
	OK         bool
	Detail     string
	When       string // "5m ago" / "" when no result stored yet
}

type settingsPageData struct {
	GlobalDiscordWebhook string
	NotifyAvatarURL      string
	LogRetentionDays     int
	LogLevel             string // empty = use env default
	LogLevelEnv          string
	TickIntervalSec      int
	DiscoveryIntervalMin int
	PriorityMode         string // "ordered" | "ending_soonest"
	KickWatchMode        string // "browser" | "ws" (experimental)
	NotifyClaim          bool
	NotifyProgress       bool
	NotifyAuth           bool
	NotifyError          bool
	NotifyCanary         bool
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
	Sidecars   []string // per-account Kick sidecar addresses
	GitCommit  string
	Version    string
	// Read-only SSO status
	OIDC settingsOIDC

	// Accrual-canary results + config (Health tab)
	CanaryTwitch        canaryView
	CanaryKick          canaryView
	CanaryTwitchChannel string
	CanaryKickChannel   string
	CanaryIntervalSec   int
	// CanaryPanelAutoRefresh, when true, adds a one-shot hx-trigger on the
	// canary panel so the browser re-fetches the panel ~8s after a run-now.
	CanaryPanelAutoRefresh bool

	// Proxy settings
	ProxyURL     string
	ProxyEnabled bool
}

func (d *settingsDeps) renderTab(w http.ResponseWriter, r *http.Request, active string) {
	lang := i18n.DetectLang(r)
	ctx := r.Context()
	url, _ := d.s.GlobalDiscordWebhook(ctx)
	avatarURL, _ := d.s.NotifyAvatarURL(ctx)
	days, _ := d.s.LogRetentionDays(ctx)
	level, _ := d.s.LogLevel(ctx)
	tick, _ := d.s.TickIntervalSec(ctx)
	discIv, _ := d.s.DiscoveryIntervalMin(ctx)
	prio, _ := d.s.PriorityMode(ctx)
	kickWatch, _ := d.s.KickWatchMode(ctx)
	var sidecars []string
	if d.sidecars != nil {
		sidecars = d.sidecars()
	}
	nc, np, na, ne, ncan := d.s.NotifyKinds(ctx)
	progStep, _ := d.s.ProgressNotifyStepPct(ctx)

	uptime := ""
	if !d.startedAt.IsZero() {
		uptime = formatUptime(time.Since(d.startedAt), lang)
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

	// Accrual-canary: read channel settings + stored results for the Health tab.
	canaryTwitchCh, _ := d.s.CanaryTwitchChannel(ctx)
	canaryKickCh, _ := d.s.CanaryKickChannel(ctx)
	canaryIntervalSec, _ := d.s.CanaryIntervalSec(ctx)
	canaryTwitchView := buildCanaryView(ctx, d.q, "twitch", canaryTwitchCh, lang)
	canaryKickView := buildCanaryView(ctx, d.q, "kick", canaryKickCh, lang)

	proxyURL, _ := d.s.ProxyURL(ctx)
	proxyEnabled, _ := d.s.ProxyEnabled(ctx)

	render(w, r, d.t, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: active,
		Page: settingsPageData{
			GlobalDiscordWebhook: url,
			NotifyAvatarURL:      avatarURL,
			LogRetentionDays:     days,
			LogLevel:             level,
			LogLevelEnv:          d.logLevelEnv,
			TickIntervalSec:      tick,
			DiscoveryIntervalMin: discIv,
			PriorityMode:         prio,
			KickWatchMode:        kickWatch,
			GlobalGames:          globalGames,
			AllGames:             allGames,
			NotifyClaim:          nc,
			NotifyProgress:       np,
			NotifyAuth:           na,
			NotifyError:          ne,
			NotifyCanary:         ncan,
			ProgressNotifyStep:   progStep,
			Uptime:               uptime,
			GoVersion:            runtime.Version(),
			Goroutines:           runtime.NumGoroutine(),
			BrowserURL:           d.browserURL,
			Sidecars:             sidecars,
			GitCommit:            d.gitCommit,
			Version:              d.version,
			OIDC:                 ssoStatus,
			CanaryTwitch:         canaryTwitchView,
			CanaryKick:           canaryKickView,
			CanaryTwitchChannel:  canaryTwitchCh,
			CanaryKickChannel:    canaryKickCh,
			CanaryIntervalSec:    canaryIntervalSec,
			ProxyURL:             proxyURL,
			ProxyEnabled:         proxyEnabled,
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
func (d *settingsDeps) getExperimental(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "experimental")
}
func (d *settingsDeps) getHealth(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "health")
}

// saveErr writes a 500 and reports true when err is non-nil, so a save
// handler can abort BEFORE flashing "saved". Without this, settings writes
// that fail (e.g. a DB error) were swallowed and the UI falsely reported
// success — the user's "Save successful but nothing persisted" bug.
func saveErr(w http.ResponseWriter, err error) bool {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	return false
}

// postGeneral saves the General tab: tick/discovery intervals + logging.
func (d *settingsDeps) postGeneral(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if v := r.FormValue("log_retention_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if saveErr(w, d.s.SetLogRetentionDays(ctx, n)) {
				return
			}
		}
	}
	if saveErr(w, d.s.SetLogLevel(ctx, r.FormValue("log_level"))) {
		return
	}

	intervalsChanged := false
	if v := r.FormValue("tick_interval_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.TickIntervalSec(ctx); cur != n {
				intervalsChanged = true
			}
			if saveErr(w, d.s.SetTickIntervalSec(ctx, n)) {
				return
			}
		}
	}
	if v := r.FormValue("discovery_interval_min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.DiscoveryIntervalMin(ctx); cur != n {
				intervalsChanged = true
			}
			if saveErr(w, d.s.SetDiscoveryIntervalMin(ctx, n)) {
				return
			}
		}
	}
	// heartbeat_interval_sec intentionally not accepted: HeartbeatInterval is
	// locked to 60s (Twitch credits 1 min per beacon; >60s under-credits).
	if d.onUpdate != nil {
		d.onUpdate()
	}
	msg := "flash.settings_saved"
	if intervalsChanged {
		msg = "flash.settings_saved_reload"
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
	if saveErr(w, d.s.SetNotifyAvatarURL(ctx, strings.TrimSpace(r.FormValue("notify_avatar_url")))) {
		return
	}
	on := func(name string) bool { return r.FormValue(name) == "1" }
	if saveErr(w, d.s.SetNotifyKinds(ctx, on("notify_claim"), on("notify_progress"), on("notify_auth"), on("notify_error"), on("notify_canary"))) {
		return
	}
	stepChanged := false
	if v := r.FormValue("progress_notify_step"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			if cur, _ := d.s.ProgressNotifyStepPct(ctx); cur != n {
				stepChanged = true
			}
			if saveErr(w, d.s.SetProgressNotifyStepPct(ctx, n)) {
				return
			}
		}
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	msg := "flash.notifications_saved"
	if stepChanged {
		// The step is read by each watcher at build time, so it applies on
		// the next scheduler reload, not live like the on/off toggles.
		msg = "flash.notifications_saved_reload"
	}
	d.sm.Put(ctx, "flash", msg)
	http.Redirect(w, r, "/settings/notifications", http.StatusSeeOther)
}

// postPriorityMode saves the Drop Priority tab's mode selector.
func (d *settingsDeps) postPriorityMode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if saveErr(w, d.s.SetPriorityMode(ctx, r.FormValue("priority_mode"))) {
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(ctx, "flash", "flash.priority_mode_saved")
	http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
}

// postExperimental saves the Experimental tab: the Kick watch path toggle.
// The mode is read by the miner at startup, so it applies on the next
// scheduler reload / restart, not live.
func (d *settingsDeps) postExperimental(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mode := store.KickWatchModeBrowser
	switch r.FormValue("kick_watch_mode") {
	case store.KickWatchModeWS:
		mode = store.KickWatchModeWS
	case store.KickWatchModeAuto:
		mode = store.KickWatchModeAuto
	}
	changed := false
	if cur, _ := d.s.KickWatchMode(ctx); cur != mode {
		changed = true
	}
	if saveErr(w, d.s.SetKickWatchMode(ctx, mode)) {
		return
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	msg := "flash.experimental_saved"
	if changed {
		msg = "flash.kick_watch_saved"
	}
	d.sm.Put(ctx, "flash", msg)
	http.Redirect(w, r, "/settings/experimental", http.StatusSeeOther)
}

// notifyTest fires one representative sample event through the live notifier
// so the operator can confirm the webhook + embed look right. Returns a small
// HTMX fragment (not a redirect) reporting success or the error.
func (d *settingsDeps) notifyTest(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLang(r)
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
		writeResult(false, i18n.T(lang, "notify.no_notifier"))
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
			writeResult(false, i18n.T(lang, "notify.no_webhook"))
			return
		}
	}

	slog.Info("notify test firing", "kind", "notify", "target", target)
	// "test" event — the verbosity filter always allows it, so a manual test
	// delivers even when the user has every real notification kind toggled off.
	if err := d.notifier.Notify(ctx, "test", sample); err != nil {
		slog.Warn("notify test failed", "kind", "error", "target", target, "err", err)
		writeResult(false, i18n.T(lang, "notify.test_failed")+": "+err.Error())
		return
	}
	slog.Info("notify test sent", "kind", "notify", "target", target)
	writeResult(true, i18n.T(lang, "notify.test_sent"))
}

// globalGamesAdd handles POST /settings/global-games/add — accepts a
// free-text game name, slugs it, upserts a games row, and appends to
// the global priority list at the next rank slot. Mirrors the
// per-account /accounts/:id/games/add flow so the user can pre-seed
// the global list before any campaign scrape surfaces the game.
func (d *settingsDeps) globalGamesAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := i18n.DetectLang(r)
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
	// Canonical id (gameslug.ID, '-'→'_') so it matches discovery's row; plain
	// "g_"+slug keeps hyphens and collides on the UNIQUE slug for multi-word games.
	gameID := gameslug.ID(name)
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
	d.sm.Put(ctx, "flash", i18n.T(lang, "flash.added_to_global_priority"))
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
		fail("flash.admin_not_configured")
		return
	}
	if err := auth.VerifyPassword(admin.PasswordHash, cur); err != nil {
		fail("flash.current_password_wrong")
		return
	}
	if len(nw) < 8 {
		fail("flash.new_password_short")
		return
	}
	if nw != cf {
		fail("flash.new_passwords_mismatch")
		return
	}
	hash, err := auth.HashPassword(nw)
	if err != nil {
		fail("flash.could_not_hash")
		return
	}
	if err := d.q.UpsertAdmin(ctx, gen.UpsertAdminParams{PasswordHash: hash, CreatedAt: time.Now().Unix()}); err != nil {
		fail("flash.could_not_save")
		return
	}
	d.sm.Put(ctx, "flash", "flash.master_password_changed")
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
	d.sm.Put(ctx, "flash", "flash.global_priority_saved")
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

// buildCanaryView loads a stored canary result and builds the template view
// for one platform. channel="" → Configured=false; no stored result →
// Configured=true with empty When.
func buildCanaryView(ctx context.Context, q *gen.Queries, platform, channel, lang string) canaryView {
	if channel == "" {
		return canaryView{Configured: false}
	}
	if q == nil {
		return canaryView{Configured: true}
	}
	res, ok, _ := canary.LoadResult(ctx, q, platform)
	if !ok {
		return canaryView{Configured: true}
	}
	when := ""
	if !res.CheckedAt.IsZero() {
		when = formatRelative(time.Since(res.CheckedAt), lang)
	}
	return canaryView{
		Configured: true,
		OK:         res.OK,
		Detail:     res.Detail,
		When:       when,
	}
}

// formatRelative returns a human "5m ago" / "2h ago" string for a duration.
func formatRelative(d time.Duration, lang string) string {
	if d < 0 {
		d = 0
	}
	ago := i18n.T(lang, "time.ago")
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds %s", int(d.Seconds()), ago)
	case d < time.Hour:
		return fmt.Sprintf("%dm %s", int(d.Minutes()), ago)
	default:
		return fmt.Sprintf("%dh %s", int(d.Hours()), ago)
	}
}

// canarySave handles POST /settings/canary — saves the three canary settings.
func (d *settingsDeps) canarySave(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLang(r)
	ctx := r.Context()
	if saveErr(w, d.s.SetCanaryTwitchChannel(ctx, r.FormValue("canary_twitch_channel"))) {
		return
	}
	if saveErr(w, d.s.SetCanaryKickChannel(ctx, r.FormValue("canary_kick_channel"))) {
		return
	}
	if v := r.FormValue("canary_interval_sec"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, i18n.T(lang, "error.invalid_interval"), http.StatusBadRequest)
			return
		}
		if saveErr(w, d.s.SetCanaryIntervalSec(ctx, n)) {
			return
		}
	}
	d.sm.Put(ctx, "flash", "flash.canary_saved")
	http.Redirect(w, r, "/settings/health", http.StatusSeeOther)
}

// canaryRun handles POST /settings/canary/run — launches the canary in a
// background goroutine (detached from the request context so navigating away
// does not cancel the in-flight probe/save) and immediately returns the
// current stored results. An hx-trigger="load delay:8s" on the returned panel
// causes a single follow-up GET /settings/health/canary-panel that picks up
// the completed result.
func (d *settingsDeps) canaryRun(w http.ResponseWriter, r *http.Request) {
	if d.runCanary != nil {
		// Detach from the request context: use a bounded background context so
		// the probe completes and persists regardless of the HTTP connection
		// lifecycle (proxy timeout, user navigation). 90s is enough for two
		// Twitch beacons at 5s + the Kick window (45s) with headroom.
		runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		go func() {
			defer cancel()
			if err := d.runCanary(runCtx); err != nil {
				slog.Warn("canary run-now failed", "err", err)
			}
		}()
	}
	// Return current (pre-run) results immediately. The panel carries an
	// hx-trigger="load delay:8s" so the browser re-fetches it once after the
	// background run has had a chance to complete.
	d.renderCanaryPanel(w, r, true /* addAutoRefresh */)
}

// canaryPanel handles GET /settings/health/canary-panel — returns the
// canary_panel partial rendered from the latest stored results. Used by the
// auto-refresh triggered after a run-now.
func (d *settingsDeps) canaryPanel(w http.ResponseWriter, r *http.Request) {
	d.renderCanaryPanel(w, r, false /* no further auto-refresh */)
}

// renderCanaryPanel renders the canary_panel partial. When addAutoRefresh is
// true the panel gets hx-trigger="load delay:8s" so the browser polls once
// after a background run-now finishes.
func (d *settingsDeps) renderCanaryPanel(w http.ResponseWriter, r *http.Request, addAutoRefresh bool) {
	lang := i18n.DetectLang(r)
	ctx := r.Context()
	twitchCh, _ := d.s.CanaryTwitchChannel(ctx)
	kickCh, _ := d.s.CanaryKickChannel(ctx)
	intervalSec, _ := d.s.CanaryIntervalSec(ctx)
	page := settingsPageData{
		CanaryTwitch:           buildCanaryView(ctx, d.q, "twitch", twitchCh, lang),
		CanaryKick:             buildCanaryView(ctx, d.q, "kick", kickCh, lang),
		CanaryTwitchChannel:    twitchCh,
		CanaryKickChannel:      kickCh,
		CanaryIntervalSec:      intervalSec,
		CanaryPanelAutoRefresh: addAutoRefresh,
	}
	renderPartial(w, r, d.t, "canary_panel", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "health",
		Page: page,
	})
}

// getProxy handles GET /settings/proxy — renders the Proxy settings tab.
func (d *settingsDeps) getProxy(w http.ResponseWriter, r *http.Request) {
	d.renderTab(w, r, "proxy")
}

// postProxy handles POST /settings/proxy — saves proxy settings.
func (d *settingsDeps) postProxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := i18n.DetectLang(r)
	enabled := r.FormValue("proxy_enabled") == "1"
	proxyURL := strings.TrimSpace(r.FormValue("proxy_url"))

	// Validate proxy URL scheme if not empty
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5") {
			d.sm.Put(ctx, "flash", i18n.T(lang, "flash.proxy_invalid_url"))
			http.Redirect(w, r, "/settings/proxy", http.StatusSeeOther)
			return
		}
	}

	if saveErr(w, d.s.SetProxyEnabled(ctx, enabled)) {
		return
	}
	if saveErr(w, d.s.SetProxyURL(ctx, proxyURL)) {
		return
	}
	d.sm.Put(ctx, "flash", "flash.proxy_saved")
	http.Redirect(w, r, "/settings/proxy", http.StatusSeeOther)
}

// proxyTest handles POST /settings/proxy/test — tests the proxy connection.
// Returns an HTMX fragment with the test result.
func (d *settingsDeps) proxyTest(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLang(r)
	ctx := r.Context()
	proxyURL, _ := d.s.ProxyURL(ctx)
	proxyEnabled, _ := d.s.ProxyEnabled(ctx)

	if !proxyEnabled || proxyURL == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<span class="proxy-test-result" style="color:var(--red)">✗ %s</span>`, html.EscapeString(i18n.T(lang, "proxy.test_not_configured")))
		return
	}

	// Build transport with proxy (using the same logic as runtime)
	transport := netutil.NewTransport(proxyURL)
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	// Test by fetching a known endpoint
	resp, err := client.Get("https://api.ipify.org?format=json")
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<span class="proxy-test-result" style="color:var(--red)">✗ %s: %s</span>`, html.EscapeString(i18n.T(lang, "proxy.test_fail")), html.EscapeString(err.Error()))
		return
	}
	defer resp.Body.Close()

	var result struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<span class="proxy-test-result" style="color:var(--red)">✗ %s</span>`, html.EscapeString(i18n.T(lang, "proxy.test_fail")))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span class="proxy-test-result" style="color:var(--green)">✓ %s: %s</span>`, html.EscapeString(i18n.T(lang, "proxy.test_ok")), html.EscapeString(result.IP))
}
