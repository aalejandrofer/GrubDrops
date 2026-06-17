package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/i18n"
)

const oidcStateCookie = "grub_oidc_state"

// oidcDeps wires the OIDC handlers. When Provider is nil or disabled, the
// routes redirect to /login (feature off).
type oidcDeps struct {
	p      *oidc.Provider
	hs     *oidc.HandshakeStore
	sm     *scs.SessionManager
	secure bool // SameSite=Lax transient cookie Secure flag (mirrors SecureCookies)
}

func (d oidcDeps) enabled() bool { return d.p != nil && d.p.Enabled() }

// loginRedirect (GET /auth/oidc/login) starts the auth-code flow.
func (d oidcDeps) loginRedirect(w http.ResponseWriter, r *http.Request) {
	if !d.enabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	state := oidc.NewState()
	nonce := oidc.NewState()
	verifier := oidc.NewVerifier()

	if err := d.hs.Put(r.Context(), state, nonce, verifier, 5*time.Minute); err != nil {
		http.Error(w, i18n.T(i18n.DetectLang(r), "error.internal"), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     "/auth/oidc/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   d.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, d.p.AuthCodeURL(state, nonce, oidc.Challenge(verifier)), http.StatusSeeOther)
}

// callback (GET /auth/oidc/callback) completes the flow.
func (d oidcDeps) callback(w http.ResponseWriter, r *http.Request) {
	if !d.enabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	d.clearStateCookie(w)

	cookie, err := r.Cookie(oidcStateCookie)
	queryState := r.URL.Query().Get("state")
	lang := i18n.DetectLang(r)
	if err != nil || cookie.Value == "" || cookie.Value != queryState {
		d.fail(w, r, i18n.T(lang, "oidc.session_expired"), nil)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		d.fail(w, r, i18n.T(lang, "oidc.denied_by_provider"), fmt.Errorf("idp error: %s", e))
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		d.fail(w, r, i18n.T(lang, "oidc.auth_failed"), nil)
		return
	}
	nonce, verifier, err := d.hs.Take(r.Context(), queryState)
	if err != nil {
		d.fail(w, r, i18n.T(lang, "oidc.session_expired"), err)
		return
	}
	claims, err := d.p.ExchangeAndVerify(r.Context(), code, verifier, nonce)
	if err != nil {
		d.fail(w, r, i18n.T(lang, "oidc.signin_failed"), err)
		return
	}
	if err := d.p.Authorize(claims); err != nil {
		d.fail(w, r, i18n.T(lang, "oidc.account_not_allowed"), err)
		return
	}
	if err := d.sm.RenewToken(r.Context()); err != nil {
		http.Error(w, i18n.T(i18n.DetectLang(r), "error.internal"), http.StatusInternalServerError)
		return
	}
	d.sm.Put(r.Context(), "admin_authed", true)
	identity := claims.Email
	if identity == "" {
		identity = claims.Subject
	}
	d.sm.Put(r.Context(), "auth_identity", identity)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d oidcDeps) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/auth/oidc/",
		MaxAge: -1, HttpOnly: true, Secure: d.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (d oidcDeps) fail(w http.ResponseWriter, r *http.Request, msg string, err error) {
	slog.Warn("oidc callback failed", "reason", msg, "err", err)
	d.sm.Put(r.Context(), "flash", msg)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
