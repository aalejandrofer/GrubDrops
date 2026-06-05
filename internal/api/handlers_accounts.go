package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/rust-drops-miner/internal/scheduler"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type accountsDeps struct {
	q   *gen.Queries
	t   Renderer
	sm  *scs.SessionManager
	sch *scheduler.Scheduler
}

type accountRow struct {
	gen.Account
	State     string // raw scheduler state: watching, claiming, …, needs_auth, stopped
	StateTier string // ui colour bucket: "green" | "orange" | "grey"
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
		enriched = append(enriched, accountRow{
			Account:   a,
			State:     st,
			StateTier: tierForState(a.Enabled == 1, st),
		})
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
	login := r.FormValue("login")
	display := r.FormValue("display_name")
	if platform == "" || login == "" {
		render(w, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: "platform and login required", Active: "accounts",
		})
		return
	}
	if display == "" {
		display = login
	}
	id := genID()
	now := time.Now().Unix()
	if _, err := d.q.CreateAccount(r.Context(), gen.CreateAccountParams{
		ID: id, Platform: platform, Login: login, DisplayName: display,
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
	d.sm.Put(r.Context(), "flash", "games saved — click Apply changes to reload watchers")
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
	d.sm.Put(r.Context(), "flash", "saved — click Apply changes to reload watchers")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d accountsDeps) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.q.DeleteAccount(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "flash", "deleted — click Apply changes to reload watchers")
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
