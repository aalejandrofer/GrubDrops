package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

type accountsDeps struct {
	q  *gen.Queries
	t  Renderer
	sm *scs.SessionManager
}

func (d accountsDeps) list(w http.ResponseWriter, r *http.Request) {
	rows, err := d.q.ListAllAccounts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flash := d.sm.PopString(r.Context(), "flash")
	render(w, d.t, "accounts_list.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: rows, Flash: flash,
	})
}

func (d accountsDeps) newGet(w http.ResponseWriter, r *http.Request) {
	render(w, d.t, "accounts_new.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
	})
}

func (d accountsDeps) newPost(w http.ResponseWriter, r *http.Request) {
	platform := r.FormValue("platform")
	login := r.FormValue("login")
	display := r.FormValue("display_name")
	if platform == "" || login == "" {
		render(w, d.t, "accounts_new.html", templateData{
			AuthedAdmin: true, CSRFToken: csrfToken(r),
			Flash: "platform and login required",
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
			Flash: err.Error(),
		})
		return
	}
	d.sm.Put(r.Context(), "flash", "account added — click Apply changes to start mining")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d accountsDeps) detail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, d.t, "accounts_detail.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: row,
	})
}

func (d accountsDeps) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	display := r.FormValue("display_name")
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
