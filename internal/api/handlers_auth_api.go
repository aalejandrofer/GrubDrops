package api

import (
	"encoding/json"
	"net/http"

	"github.com/aalejandrofer/grubdrops/internal/auth"
)

// apiLogin handles POST /api/login. It is a public route (no admin session
// required) so the SPA can authenticate before a session exists.
// The verify logic mirrors loginPost exactly.
func (d authDeps) apiLogin(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	admin, err := d.q.GetAdmin(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "admin_not_configured", "no admin configured")
		return
	}
	if err := auth.VerifyPassword(admin.PasswordHash, b.Password); err != nil {
		writeAPIError(w, http.StatusBadRequest, "wrong_password", "incorrect password")
		return
	}
	d.sm.Put(r.Context(), "admin_authed", true)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// authInfoDeps carries the OIDC configuration needed by apiAuthInfo.
type authInfoDeps struct {
	oidcEnabled   bool
	oidcProvider  string
	secureCookies bool
}

// apiAuthInfo handles GET /api/auth/info. Public — no session required.
// Returns OIDC availability so the SPA can decide whether to show the SSO
// button. Also sets the csrftoken cookie (readable by JS) so the SPA can
// bootstrap its CSRF state from this preflight call.
func apiAuthInfo(info authInfoDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Expose the masked CSRF token as a JS-readable cookie so the SPA
		// can send it back on mutating requests (mirrors spaIndex behaviour).
		http.SetCookie(w, &http.Cookie{
			Name:     "csrftoken",
			Value:    csrfToken(r),
			Path:     "/",
			HttpOnly: false,
			Secure:   info.secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"oidc_enabled":  info.oidcEnabled,
			"oidc_provider": info.oidcProvider,
		})
	}
}
