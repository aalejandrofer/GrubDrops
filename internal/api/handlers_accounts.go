package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type accountsDeps struct {
	loc           *time.Location // timezone for displayed times
	q             *gen.Queries
	db            *sql.DB
	t             Renderer
	sm            *scs.SessionManager
	sch           *scheduler.Scheduler
	reload        func(context.Context) error
	authCheck     func(context.Context)         // auth-health sweep (manual trigger)
	reloadAccount func(context.Context, string) // targeted single-account reload
	// rootCtx is the process root context. The per-account reload button
	// hands this (NOT the request context) to reloadAccount so the rebuilt
	// watcher is rooted in a long-lived context — a request context is
	// cancelled the instant the handler returns, which previously tore the
	// freshly-rebuilt watcher down (the v1.0.1 Kick re-login stall).
	rootCtx context.Context
}

// applyReload calls the scheduler reload hook if wired, swallowing
// errors with a log. Used by every per-account whitelist mutation
// so the watcher picks up new closures without manual Apply.
// Uses rootCtx (not the request context) so the reload survives
// the HTTP redirect that follows.
func (d accountsDeps) applyReload(ctx context.Context) {
	if d.reload == nil {
		return
	}
	if err := d.reload(d.rootCtx); err != nil {
		slog.Warn("accounts: scheduler reload failed after whitelist change", "err", err)
	}
}

type accountRow struct {
	gen.Account
	State      string // raw scheduler state: watching, claiming, …, needs_auth, stopped
	StateTier  string // ui colour bucket: "green" | "orange" | "grey"
	StateLabel string // i18n key for state pill text
	// Auth-health (from the periodic sweep / manual check). AuthChecked
	// is false when no probe has run yet.
	AuthChecked bool
	AuthOK      bool
	AuthMsg     string
	AuthWhen    string // human "x ago" / timestamp
	// AvatarURL is the resolved <img> src (direct for Twitch, /img/kick
	// proxied for Kick); "" -> letter circle fallback.
	AvatarURL      string
	AccountInitial string
}

// checkAuth runs the auth-health sweep on demand, then bounces back to
// the accounts list with a flash. Runs inline (few accounts); the sweep
// has a per-account timeout.
func (d accountsDeps) checkAuth(w http.ResponseWriter, r *http.Request) {
	if d.authCheck != nil {
		d.authCheck(r.Context())
		d.sm.Put(r.Context(), "flash", "flash.auth_check_complete")
	} else {
		d.sm.Put(r.Context(), "flash", "flash.auth_check_unavailable")
	}
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d accountsDeps) list(w http.ResponseWriter, r *http.Request) {
	rows, err := d.q.ListAllAccounts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stateByID := map[string]string{}
	if d.sch != nil {
		for _, s := range d.sch.Snapshot() {
			stateByID[s.AccountID] = s.State
		}
	}
	enriched := make([]accountRow, 0, len(rows))
	for _, a := range rows {
		st := stateByID[a.ID]
		if st == "" {
			st = "stopped"
		}
		row := accountRow{
			Account:        a,
			State:          st,
			StateTier:      tierForState(a.Enabled == 1, st),
			StateLabel:     stateLabel(st),
			AvatarURL:      avatarSrc(a.Platform, a.AvatarUrl),
			AccountInitial: initial(a.DisplayName),
		}
		if res, ok := authcheck.Load(r.Context(), d.q, a.ID); ok {
			row.AuthChecked = true
			row.AuthOK = res.OK
			row.AuthMsg = res.Msg
			row.AuthWhen = time.Unix(res.CheckedAt, 0).In(d.loc).Format("2006-01-02 15:04 MST")
		}
		enriched = append(enriched, row)
	}
	flash := d.sm.PopString(r.Context(), "flash")
	render(w, r, d.t, "accounts_list.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: enriched, Flash: flash, Active: "accounts",
	})
}

func tierForState(enabled bool, state string) string {
	if !enabled {
		return "grey"
	}
	switch state {
	case "watching", "claiming", "pick_stream", "pick_campaign", "idle":
		return "green"
	case "needs_auth", "stopped", "sleeping":
		return "orange"
	}
	return "orange"
}

func (d accountsDeps) newGet(w http.ResponseWriter, r *http.Request) {
	render(w, r, d.t, "accounts_new.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "accounts",
	})
}

func (d accountsDeps) newPost(w http.ResponseWriter, r *http.Request) {
	platform := r.FormValue("platform")
	display := strings.TrimSpace(r.FormValue("display_name"))
	if platform == "" || display == "" {
		render(w, r, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: "flash.platform_name_required", Active: "accounts",
		})
		return
	}
	id := genID()
	now := time.Now().Unix()
	if _, err := d.q.CreateAccount(r.Context(), gen.CreateAccountParams{
		ID: id, Platform: platform, DisplayName: display,
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		render(w, r, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: err.Error(), Active: "accounts",
		})
		return
	}
	if platform == "twitch" || platform == "kick" {
		http.Redirect(w, r, "/accounts/"+id+"/login", http.StatusSeeOther)
		return
	}
	d.sm.Put(r.Context(), "flash", "flash.account_added")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

type accountDetailPage struct {
	Account       gen.Account
	AllGames      []gameRow
	SelectedGames []gameRow // ordered by rank
	Channels      []string  // per-account channel whitelist, ordered by rank
	// ForceChannels are permanent channel-points channels (watched 24/7
	// when idle); ForceWatchEnabled is the per-account toggle.
	ForceChannels     []string
	ForceWatchEnabled bool
}

type gameRow struct {
	ID       string
	Name     string
	Slug     string
	Selected bool
	Rank     int
}

func (d accountsDeps) detail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	all, _ := d.q.ListAllGames(r.Context())
	picked, _ := d.q.ListAccountGames(r.Context(), id)

	rankByID := make(map[string]int, len(picked))
	for _, p := range picked {
		rankByID[p.ID] = int(p.Rank)
	}

	allRows := make([]gameRow, 0, len(all))
	for _, g := range all {
		r, ok := rankByID[g.ID]
		allRows = append(allRows, gameRow{
			ID: g.ID, Name: g.Name, Slug: g.Slug,
			Selected: ok, Rank: r,
		})
	}
	selected := make([]gameRow, 0, len(picked))
	for _, p := range picked {
		selected = append(selected, gameRow{ID: p.ID, Name: p.Name, Slug: p.Slug, Selected: true, Rank: int(p.Rank)})
	}

	var channels []string
	if chRows, err := d.q.ListAccountChannels(r.Context(), id); err == nil {
		for _, rch := range chRows {
			channels = append(channels, rch.Channel)
		}
	}

	var forceChannels []string
	if fcRows, err := d.q.ListForceChannels(r.Context(), id); err == nil {
		for _, rch := range fcRows {
			forceChannels = append(forceChannels, rch.Channel)
		}
	}
	forceEnabled := false
	if v, err := d.q.GetSettingString(r.Context(), ForceWatchEnabledKey(id)); err == nil && string(v) == "1" {
		forceEnabled = true
	}

	render(w, r, d.t, "accounts_detail.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Flash: d.sm.PopString(r.Context(), "flash"),
		Page: accountDetailPage{
			Account: row, AllGames: allRows, SelectedGames: selected, Channels: channels,
			ForceChannels: forceChannels, ForceWatchEnabled: forceEnabled,
		},
		Active: "accounts",
	})
}

// addGame handles POST /accounts/:id/games/add — upserts a game row
// from a free-text name (so the user can whitelist a game BEFORE any
// scrape has surfaced it) and appends it to the account's whitelist
// at the end of the rank order. ID = "g_" + slug for determinism so
// the same name in two browsers maps to the same row.
func (d accountsDeps) addGame(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
		return
	}
	slug := gameslug.Slug(name)
	if slug == "" {
		http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
		return
	}
	// Use the canonical id scheme (gameslug.ID, '-'→'_') so this matches the
	// row discovery already inserted for the same game. Building "g_"+slug here
	// keeps hyphens, producing a different id for multi-word games — the upsert
	// then misses ON CONFLICT(id) and trips the UNIQUE slug constraint.
	gameID := gameslug.ID(name)
	if err := d.q.UpsertGame(r.Context(), gen.UpsertGameParams{
		ID: gameID, Name: name, Slug: slug, Priority: 0,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Append at end of rank — read current ranks first to pick the
	// next slot. Idempotent: re-adding the same game just bumps it to
	// the end.
	existing, _ := d.q.ListAccountGames(r.Context(), id)
	rank := int64(len(existing))
	for _, e := range existing {
		if e.ID == gameID {
			rank = e.Rank // keep its current rank if already present
			break
		}
	}
	if err := d.q.AddAccountGame(r.Context(), gen.AddAccountGameParams{
		AccountID: id, GameID: gameID, Rank: rank,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// No auto-reload: whitelist/priority/account edits take effect on the
	// next manual "Apply changes" (or the next discovery tick for /drops).
	// Avoids tearing down + respinning every watcher on each small save.
	d.sm.Put(r.Context(), "flash", i18n.T(i18n.DetectLang(r), "flash.game_added"))
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// useGlobal handles POST /accounts/:id/games/use-global — clears the
// per-account whitelist so the watcher falls back to the global
// priority list (loadAccountWhitelist branches on len==0). Idempotent.
func (d accountsDeps) useGlobal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := d.q.ClearAccountGames(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// No auto-reload: whitelist/priority/account edits take effect on the
	// next manual "Apply changes" (or the next discovery tick for /drops).
	// Avoids tearing down + respinning every watcher on each small save.
	d.sm.Put(r.Context(), "flash", "flash.account_whitelist_cleared")
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// games handles POST /accounts/:id/games — rewrites the per-account
// whitelist from the form's `game_ids[]` field. Order matters: position
// in the slice becomes the rank.
func (d accountsDeps) games(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ids := r.Form["game_ids[]"]
	if err := d.q.ClearAccountGames(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i, gid := range ids {
		if err := d.q.AddAccountGame(r.Context(), gen.AddAccountGameParams{
			AccountID: id, GameID: gid, Rank: int64(i),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// No auto-reload: whitelist/priority/account edits take effect on the
	// next manual "Apply changes" (or the next discovery tick for /drops).
	// Avoids tearing down + respinning every watcher on each small save.
	d.sm.Put(r.Context(), "flash", "flash.games_saved")
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

func (d accountsDeps) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	display := r.FormValue("display_name")
	webhook := r.FormValue("webhook_url")
	enabled := int64(0)
	if r.FormValue("enabled") == "1" {
		enabled = 1
	}
	now := time.Now().Unix()
	if display != "" {
		if err := d.q.UpdateAccountDisplayName(r.Context(), gen.UpdateAccountDisplayNameParams{
			DisplayName: display, UpdatedAt: now, ID: id,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := d.q.SetAccountEnabled(r.Context(), gen.SetAccountEnabledParams{
		Enabled: enabled, UpdatedAt: now, ID: id,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := d.q.UpdateAccountWebhook(r.Context(), gen.UpdateAccountWebhookParams{
		WebhookUrl: sql.NullString{String: webhook, Valid: webhook != ""},
		UpdatedAt:  now,
		ID:         id,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Targeted reload: an account edit (enable/disable, label, webhook)
	// restarts ONLY this account's watcher — the rest of the roster keeps
	// running. (Whitelist/priority saves still defer to the manual Apply.)
	// Must run under the long-lived root context, NOT r.Context(): the
	// request context cancels the instant we redirect below, and a reload
	// kicked off on a dying context tears the watcher down without rebuilding
	// it — leaving a just-disabled account still mining until a manual reload.
	if d.reloadAccount != nil {
		ctx := d.rootCtx
		if ctx == nil {
			ctx = context.Background()
		}
		d.reloadAccount(ctx, id)
	}
	d.sm.Put(r.Context(), "flash", "flash.account_saved")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

// purgeAccount hard-deletes an account and every row that belongs to it,
// inside one transaction. The schema declares ON DELETE CASCADE on every
// account child, but cascade only fires when foreign_keys enforcement is on
// for the live connection — historically a deleted account's session and
// related rows survived and the account kept being loaded and scheduled on
// boot. Deleting the children explicitly (then the account row) makes the
// purge correct regardless of the FK pragma state. There is no soft-delete
// column in the schema, so this matches the existing hard-delete design.
func (d accountsDeps) purgeAccount(ctx context.Context, id string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit succeeds
	qtx := d.q.WithTx(tx)

	if err := qtx.DeleteAccountSession(ctx, id); err != nil {
		return err
	}
	if err := qtx.ClearAccountGames(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccountCampaignLinks(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccountCampaignPriorities(ctx, sql.NullString{String: id, Valid: true}); err != nil {
		return err
	}
	if err := qtx.DeleteAccountProgress(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccountClaims(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccount(ctx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (d accountsDeps) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.purgeAccount(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// No auto-reload: whitelist/priority/account edits take effect on the
	// next manual "Apply changes" (or the next discovery tick for /drops).
	// Avoids tearing down + respinning every watcher on each small save.
	d.sm.Put(r.Context(), "flash", "flash.deleted")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

// reload restarts a SINGLE account's watcher on demand (the per-account
// reload arrow on the dashboard "Currently mining" cards). It hands the
// process ROOT context to reloadAccount — never r.Context() — because the
// request context is cancelled the instant this handler returns, and the
// scheduler roots the rebuilt watcher in the long-lived base context derived
// from this root. Using the request context here would tear the freshly-spun
// watcher down on handler return (the v1.0.1 Kick re-login stall). The rest
// of the roster keeps running untouched.
func (d accountsDeps) reloadOne(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if d.reloadAccount == nil {
		http.Error(w, i18n.T(i18n.DetectLang(r), "error.reload_unavailable"), http.StatusServiceUnavailable)
		return
	}
	ctx := d.rootCtx
	if ctx == nil {
		ctx = context.Background()
	}
	d.reloadAccount(ctx, id)

	name := acc.DisplayName
	if name == "" {
		name = acc.ID
	}
	d.sm.Put(r.Context(), "flash", i18n.T(i18n.DetectLang(r), "flash.account_reloaded"))
	// Mirror /accounts/apply: land the user back where they clicked from
	// (dashboard or /accounts) rather than always bouncing to /accounts.
	target := applyRedirectTarget(r)
	// The button is an HTMX hx-post; HTMX swallows a 303 (it follows the
	// redirect and swaps the body into the target element). Emit HX-Redirect
	// so the browser does a full navigation instead — the flash renders on
	// the freshly-loaded page, matching the global Reload form's UX.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// toggleEnabled flips a single account's enabled flag (the enable/disable
// button on the accounts list) and fires a targeted watcher reload so the
// change takes effect immediately: disable stops that account mining, enable
// starts it, and the rest of the roster keeps running. Mirrors reloadOne's
// HTMX contract — emit HX-Redirect for a full navigation so the flash renders
// — and uses the long-lived root context for the reload (NOT r.Context(),
// which cancels on redirect and would tear the rebuilt watcher down).
func (d accountsDeps) toggleEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	enabled := int64(1)
	if acc.Enabled == 1 {
		enabled = 0
	}
	if err := d.q.SetAccountEnabled(r.Context(), gen.SetAccountEnabledParams{
		Enabled: enabled, UpdatedAt: time.Now().Unix(), ID: id,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if d.reloadAccount != nil {
		ctx := d.rootCtx
		if ctx == nil {
			ctx = context.Background()
		}
		d.reloadAccount(ctx, id)
	}
	if enabled == 1 {
		d.sm.Put(r.Context(), "flash", "flash.account_enabled")
	} else {
		d.sm.Put(r.Context(), "flash", "flash.account_disabled")
	}
	target := applyRedirectTarget(r)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// addChannel handles POST /accounts/:id/channels/add — opts the account
// into a channel so null-game drops on that channel get mined.
func (d accountsDeps) addChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	ch := strings.ToLower(strings.TrimSpace(r.FormValue("channel")))
	if ch != "" {
		if err := d.q.AddAccountChannel(r.Context(), gen.AddAccountChannelParams{
			AccountID: id, Channel: ch, Rank: 0,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.applyReload(r.Context())
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// removeChannel handles POST /accounts/:id/channels/remove.
func (d accountsDeps) removeChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	ch := strings.ToLower(strings.TrimSpace(r.FormValue("channel")))
	if ch != "" {
		if err := d.q.RemoveAccountChannel(r.Context(), gen.RemoveAccountChannelParams{
			AccountID: id, Channel: ch,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.applyReload(r.Context())
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// ForceWatchEnabledKey is the per-account KV flag toggling channel-points
// force-watch (watch a configured channel 24/7 when idle). Shared with the
// watcher's force-watch source in cmd/miner.
func ForceWatchEnabledKey(accountID string) string { return "force_watch:" + accountID }

// addForceChannel handles POST /accounts/:id/force-channels/add — adds a
// permanent channel-points channel (watched 24/7 when idle).
func (d accountsDeps) addForceChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	ch := strings.ToLower(strings.TrimSpace(r.FormValue("channel")))
	if ch != "" {
		now := time.Now().Unix()
		if err := d.q.AddForceChannel(r.Context(), gen.AddForceChannelParams{
			AccountID: id, Channel: ch, Rank: 0, CreatedAt: now,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.applyReload(r.Context())
		d.sm.Put(r.Context(), "flash", "flash.force_channel_added")
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// removeForceChannel handles POST /accounts/:id/force-channels/remove.
func (d accountsDeps) removeForceChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	ch := strings.ToLower(strings.TrimSpace(r.FormValue("channel")))
	if ch != "" {
		if err := d.q.RemoveForceChannel(r.Context(), gen.RemoveForceChannelParams{
			AccountID: id, Channel: ch,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.applyReload(r.Context())
		d.sm.Put(r.Context(), "flash", "flash.force_channel_removed")
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// forceChannelsReorder handles POST /accounts/:id/force-channels — rewrites
// the force-watch channel list from the ordered channel[] field. Position in
// the slice becomes the rank (rank 0 = the channel watched first when idle).
func (d accountsDeps) forceChannelsReorder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := d.q.ClearForceChannels(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now().Unix()
	rank := 0
	seen := map[string]struct{}{}
	for _, raw := range r.Form["channel"] {
		ch := strings.ToLower(strings.TrimSpace(raw))
		if ch == "" {
			continue
		}
		if _, dup := seen[ch]; dup {
			continue
		}
		seen[ch] = struct{}{}
		if err := d.q.AddForceChannel(r.Context(), gen.AddForceChannelParams{
			AccountID: id, Channel: ch, Rank: int64(rank), CreatedAt: now,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rank++
	}
	d.applyReload(r.Context())
	d.sm.Put(r.Context(), "flash", "flash.force_saved")
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// forceWatchToggle handles POST /accounts/:id/force-watch — flips the
// channel-points 24/7 idle-mining toggle for the account.
func (d accountsDeps) forceWatchToggle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	val := []byte("")
	enabled := r.FormValue("enabled") == "1"
	if enabled {
		val = []byte("1")
	}
	if err := d.q.UpsertSettingString(r.Context(), gen.UpsertSettingStringParams{
		Key: ForceWatchEnabledKey(id), Value: val,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.applyReload(r.Context())
	if enabled {
		d.sm.Put(r.Context(), "flash", "flash.force_watch_enabled")
	} else {
		d.sm.Put(r.Context(), "flash", "flash.force_watch_disabled")
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

func genID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "acc_" + hex.EncodeToString(b[:])
}

// Reloader is implemented by main; the apply endpoint calls Reload to
// rebuild watchers without restarting the process.
type Reloader interface {
	Reload(ctx context.Context) error
}
