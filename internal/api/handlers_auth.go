package api

import (
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/rust-drops-miner/internal/auth"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type authDeps struct {
	q  *gen.Queries
	t  Renderer
	sm *scs.SessionManager
}

func (d authDeps) loginGet(w http.ResponseWriter, r *http.Request) {
	if !adminConfigured(r.Context(), d.q) {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if d.sm.GetBool(r.Context(), "admin_authed") {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	render(w, d.t, "login.html", templateData{CSRFToken: csrfToken(r)})
}

func (d authDeps) loginPost(w http.ResponseWriter, r *http.Request) {
	admin, err := d.q.GetAdmin(r.Context())
	if err != nil {
		render(w, d.t, "login.html", templateData{CSRFToken: csrfToken(r), Flash: "admin not configured"})
		return
	}
	if err := auth.VerifyPassword(admin.PasswordHash, r.FormValue("password")); err != nil {
		render(w, d.t, "login.html", templateData{CSRFToken: csrfToken(r), Flash: "wrong password"})
		return
	}
	if err := d.sm.RenewToken(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "admin_authed", true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d authDeps) logoutPost(w http.ResponseWriter, r *http.Request) {
	_ = d.sm.Destroy(r.Context())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
