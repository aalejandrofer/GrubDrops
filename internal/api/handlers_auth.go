package api

import (
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type authDeps struct {
	q                *gen.Queries
	t                Renderer
	sm               *scs.SessionManager
	oidcEnabled      bool
	oidcProviderName string
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
	flash := d.sm.PopString(r.Context(), "flash")
	render(w, r, d.t, "login.html", templateData{
		CSRFToken:        csrfToken(r),
		Flash:            flash,
		OIDCEnabled:      d.oidcEnabled,
		OIDCProviderName: d.oidcProviderName,
	})
}

func (d authDeps) loginPost(w http.ResponseWriter, r *http.Request) {
	admin, err := d.q.GetAdmin(r.Context())
	if err != nil {
		render(w, r, d.t, "login.html", templateData{CSRFToken: csrfToken(r), Flash: "flash.admin_not_configured"})
		return
	}
	if err := auth.VerifyPassword(admin.PasswordHash, r.FormValue("password")); err != nil {
		render(w, r, d.t, "login.html", templateData{CSRFToken: csrfToken(r), Flash: "flash.wrong_password"})
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
