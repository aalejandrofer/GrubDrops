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
	"github.com/aalejandrofer/grubdrops/internal/scheduler"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type accountsDeps struct {
	q             *gen.Queries
	db            *sql.DB
	t             Renderer
	sm            *scs.SessionManager
	sch           *scheduler.Scheduler
	reload        func(context.Context) error
	authCheck     func(context.Context)         // auth-health sweep (manual trigger)
	reloadAccount func(context.Context, string) // targeted single-account reload
}

// applyReload calls the scheduler reload hook if wired, swallowing
// errors with a log. Used by every per-account whitelist mutation
// so the watcher picks up new closures without manual Apply.
func (d accountsDeps) applyReload(ctx context.Context) {
	if d.reload == nil {
		return
	}
	if err := d.reload(ctx); err != nil {
		slog.Warn("accounts: scheduler reload failed after whitelist change", "err", err)
	}
}

type accountRow struct {
	gen.Account
	State     string // raw scheduler state: watching, claiming, …, needs_auth, stopped
	StateTier string // ui colour bucket: "green" | "orange" | "grey"
	// Auth-health (from the periodic sweep / manual check). AuthChecked
	// is false when no probe has run yet.
	AuthChecked bool
	AuthOK      bool
	AuthMsg     string
	AuthWhen    string // human "x ago" / timestamp
}

// checkAuth runs the auth-health sweep on demand, then bounces back to
// the accounts list with a flash. Runs inline (few accounts); the sweep
// has a per-account timeout.
func (d accountsDeps) checkAuth(w http.ResponseWriter, r *http.Request) {
	if d.authCheck != nil {
		d.authCheck(r.Context())
		d.sm.Put(r.Context(), "flash", "auth check complete")
	} else {
		d.sm.Put(r.Context(), "flash", "auth check unavailable")
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
			Account:   a,
			State:     st,
			StateTier: tierForState(a.Enabled == 1, st),
		}
		if res, ok := authcheck.Load(r.Context(), d.q, a.ID); ok {
			row.AuthChecked = true
			row.AuthOK = res.OK
			row.AuthMsg = res.Msg
			row.AuthWhen = time.Unix(res.CheckedAt, 0).UTC().Format("2006-01-02 15:04")
		}
		enriched = append(enriched, row)
	}
	flash := d.sm.PopString(r.Context(), "flash")
	render(w, d.t, "accounts_list.html", templateData{
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
	render(w, d.t, "accounts_new.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "accounts",
	})
}

func (d accountsDeps) newPost(w http.ResponseWriter, r *http.Request) {
	platform := r.FormValue("platform")
	display := strings.TrimSpace(r.FormValue("display_name"))
	if platform == "" || display == "" {
		render(w, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: "platform and display name required", Active: "accounts",
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
		render(w, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: err.Error(), Active: "accounts",
		})
		return
	}
	if platform == "twitch" || platform == "kick" {
		http.Redirect(w, r, "/accounts/"+id+"/login", http.StatusSeeOther)
		return
	}
	d.sm.Put(r.Context(), "flash", "account added — click Apply changes to start mining")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

type accountDetailPage struct {
	Account       gen.Account
	AllGames      []gameRow
	SelectedGames []gameRow // ordered by rank
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

	render(w, d.t, "accounts_detail.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page:   accountDetailPage{Account: row, AllGames: allRows, SelectedGames: selected},
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
	gameID := "g_" + slug
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
	d.sm.Put(r.Context(), "flash", "added "+name)
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
	d.sm.Put(r.Context(), "flash", "account whitelist cleared — now follows global priority")
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
	d.sm.Put(r.Context(), "flash", "games saved")
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
	if d.reloadAccount != nil {
		d.reloadAccount(r.Context(), id)
	}
	d.sm.Put(r.Context(), "flash", "saved — this account reloaded")
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
	d.sm.Put(r.Context(), "flash", "deleted")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
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
