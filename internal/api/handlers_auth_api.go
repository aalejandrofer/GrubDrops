package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
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

// apiSetup handles POST /api/setup. Public — creates the admin account during
// first-run setup. Returns 409 if an admin already exists (prevents re-creation).
// Mirrors the logic in setupDeps.post exactly, adapted for the JSON API.
func (d setupDeps) apiSetup(w http.ResponseWriter, r *http.Request) {
	if exists, _ := d.q.AdminExists(r.Context()); exists {
		writeAPIError(w, http.StatusConflict, "admin_configured", "admin already configured")
		return
	}
	var b struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if len(b.Password) < 8 {
		writeAPIError(w, http.StatusBadRequest, "password_short", "password too short")
		return
	}
	if b.Password != b.Confirm {
		writeAPIError(w, http.StatusBadRequest, "passwords_mismatch", "passwords do not match")
		return
	}
	hash, err := auth.HashPassword(b.Password)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := d.q.UpsertAdmin(r.Context(), gen.UpsertAdminParams{
		PasswordHash: hash,
		CreatedAt:    time.Now().Unix(),
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	d.sm.Put(r.Context(), "admin_authed", true)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// authInfoDeps carries the OIDC configuration and queries needed by apiAuthInfo.
type authInfoDeps struct {
	oidcEnabled   bool
	oidcProvider  string
	secureCookies bool
	q             *gen.Queries
}

// apiAuthInfo handles GET /api/auth/info. Public — no session required.
// Returns OIDC availability so the SPA can decide whether to show the SSO
// button. Also sets the csrftoken cookie (readable by JS) so the SPA can
// bootstrap its CSRF state from this preflight call.
// Includes admin_exists so the SPA can redirect to /setup on first run.
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
		adminExists := false
		if info.q != nil {
			adminExists, _ = info.q.AdminExists(r.Context())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"oidc_enabled":  info.oidcEnabled,
			"oidc_provider": info.oidcProvider,
			"admin_exists":  adminExists,
		})
	}
}
