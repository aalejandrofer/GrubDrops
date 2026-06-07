package api

import (
	"context"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type setupDeps struct {
	q  *gen.Queries
	t  Renderer
	sm *scs.SessionManager
}

func (d setupDeps) get(w http.ResponseWriter, r *http.Request) {
	exists, err := d.q.AdminExists(r.Context())
	if err == nil && exists {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	render(w, d.t, "setup.html", templateData{
		AuthedAdmin: false,
		CSRFToken:   csrfToken(r),
	})
}

func (d setupDeps) post(w http.ResponseWriter, r *http.Request) {
	exists, err := d.q.AdminExists(r.Context())
	if err == nil && exists {
		http.Error(w, "admin already configured", http.StatusConflict)
		return
	}
	pw := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if pw != confirm {
		render(w, d.t, "setup.html", templateData{
			AuthedAdmin: false,
			CSRFToken:   csrfToken(r),
			Flash:       "passwords do not match",
		})
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		render(w, d.t, "setup.html", templateData{
			AuthedAdmin: false,
			CSRFToken:   csrfToken(r),
			Flash:       err.Error(),
		})
		return
	}
	if err := d.q.UpsertAdmin(r.Context(), gen.UpsertAdminParams{
		PasswordHash: hash,
		CreatedAt:    time.Now().Unix(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := d.sm.RenewToken(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "admin_authed", true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func adminConfigured(ctx context.Context, q *gen.Queries) bool {
	exists, err := q.AdminExists(ctx)
	return err == nil && exists
}
