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
	// status fields surfaced read-only on the settings page
	startedAt   time.Time
	logLevelEnv string
	browserURL  string
	gitCommit   string
	version     string
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

type settingsPageData struct {
	GlobalDiscordWebhook string
	LogRetentionDays     int
	LogLevel             string // empty = use env default
	LogLevelEnv          string
	TickIntervalMs       int
	DiscoveryIntervalSec int
	PriorityMode         string // "ordered" | "ending_soonest"
	NotifyClaim          bool
	NotifyProgress       bool
	NotifyAuth           bool
	NotifyError          bool

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
}

func (d *settingsDeps) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	url, _ := d.s.GlobalDiscordWebhook(ctx)
	days, _ := d.s.LogRetentionDays(ctx)
	level, _ := d.s.LogLevel(ctx)
	tick, _ := d.s.TickIntervalMs(ctx)
	discIv, _ := d.s.DiscoveryIntervalSec(ctx)
	prio, _ := d.s.PriorityMode(ctx)
	nc, np, na, ne := d.s.NotifyKinds(ctx)

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
	render(w, d.t, "settings.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "settings",
		Page: settingsPageData{
			GlobalDiscordWebhook: url,
			LogRetentionDays:     days,
			LogLevel:             level,
			LogLevelEnv:          d.logLevelEnv,
			TickIntervalMs:       tick,
			DiscoveryIntervalSec: discIv,
			PriorityMode:         prio,
			GlobalGames:          globalGames,
			AllGames:             allGames,
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
	_ = d.s.SetPriorityMode(ctx, r.FormValue("priority_mode"))
	on := func(name string) bool { return r.FormValue(name) == "1" }
	_ = d.s.SetNotifyKinds(ctx, on("notify_claim"), on("notify_progress"), on("notify_auth"), on("notify_error"))

	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(ctx, "flash", "settings saved — restart container to apply tick/discovery intervals")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// globalGamesAdd handles POST /settings/global-games/add — accepts a
// free-text game name, slugs it, upserts a games row, and appends to
// the global priority list at the next rank slot. Mirrors the
// per-account /accounts/:id/games/add flow so the user can pre-seed
// the global list before any campaign scrape surfaces the game.
func (d *settingsDeps) globalGamesAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if d.q == nil {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	slug := slugifyGame(name)
	if slug == "" {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
