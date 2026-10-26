package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/justinas/nosurf"

	"github.com/aalejandrofer/grubdrops/internal/auth"
	"github.com/aalejandrofer/grubdrops/internal/auth/oidc"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const (
	oidcStateCookie = "grub_oidc_state"
	minPasswordLen  = 8
)

type bypass struct {
	all, local bool
}

func readBypass() bypass {
	b := bypass{all: envTrue("GRUB_AUTHBYPASS"), local: os.Getenv("GRUB_AUTH_BYPASS_LOCAL") == "1"}
	if b.all {
		slog.Warn("GRUB_AUTHBYPASS is set — ALL authentication is DISABLED; every request is treated as admin.", "kind", "auth")
	}
	return b
}

func (b bypass) allows(r *http.Request) bool {
	return b.all || (b.local && isLoopbackRequest(r))
}

// RequireAdmin answers 401 JSON to requests without an admin session.
//
// GRUB_AUTH_BYPASS_LOCAL=1 lets direct loopback/private requests with no
// X-Forwarded-For through. GRUB_AUTHBYPASS=true disables auth for every
// request; staging/dev only.
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return requireAdmin(sm, readBypass())
}

func requireAdmin(sm *scs.SessionManager, b bypass) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if b.allows(r) || sm.GetBool(r.Context(), "admin_authed") {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, http.StatusUnauthorized, "unauthorized")
		})
	}
}

// isLoopbackRequest is true for a direct request (no X-Forwarded-For) from a
// loopback or private address.
func isLoopbackRequest(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// CSRF wraps the API with nosurf. The failure handler answers 403 JSON.
// nosurf defaults its self-scheme to https, which breaks plain-HTTP
// self-hosts; the scheme is derived from the real request instead.
func CSRF(secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := nosurf.New(next)
		h.SetBaseCookie(http.Cookie{
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
		h.SetIsTLSFunc(func(r *http.Request) bool { return requestIsHTTPS(r, secureCookies) })
		h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slog.Warn("csrf check failed",
				"reason", nosurf.Reason(r),
				"method", r.Method,
				"path", r.URL.Path,
				"origin", r.Header.Get("Origin"),
				"referer", r.Header.Get("Referer"),
				"host", r.Host,
				"treated_as_https", requestIsHTTPS(r, secureCookies),
				"secure_cookies", secureCookies,
			)
			writeErr(w, http.StatusForbidden, "csrf")
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// nosurf also demands Sec-Fetch-Site, Origin or Referer. Browsers
			// always send at least one on unsafe requests, so a request with
			// none is a non-browser client (curl, scripts) and cannot be a
			// cross-site forgery. The double-submit token is still enforced.
			if r.Header.Get("Origin") == "" && r.Header.Get("Referer") == "" && r.Header.Get("Sec-Fetch-Site") == "" {
				r = r.Clone(r.Context())
				r.Header.Set("Sec-Fetch-Site", "same-origin")
			}
			h.ServeHTTP(w, r)
		})
	}
}

// requestIsHTTPS reports whether the user reached the app over HTTPS.
// X-Forwarded-Proto is trusted only when secure cookies are on.
func requestIsHTTPS(r *http.Request, secureCookies bool) bool {
	if r.TLS != nil {
		return true
	}
	if secureCookies {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			first, _, _ := strings.Cut(proto, ",")
			return strings.EqualFold(strings.TrimSpace(first), "https")
		}
		return true
	}
	return false
}

func envTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// adminConfigured reports whether an admin exists. A lookup error is returned
// so callers fail closed.
func adminConfigured(ctx context.Context, q *gen.Queries) (bool, error) {
	return q.AdminExists(ctx)
}

// routesAuth registers the public session, setup, login and logout routes.
func (s *server) routesAuth(r chi.Router) {
	b := s.bypass
	r.Get("/session", func(w http.ResponseWriter, r *http.Request) {
		configured, err := adminConfigured(r.Context(), s.d.Q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
		oidcOn := s.d.OIDC != nil && s.d.OIDC.Enabled()
		name := ""
		if s.d.OIDC != nil {
			name = s.d.OIDC.Name()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authed":       b.allows(r) || s.d.Session.GetBool(r.Context(), "admin_authed"),
			"setup_needed": !configured,
			"oidc":         map[string]any{"enabled": oidcOn, "name": name},
			"csrf_token":   nosurf.Token(r),
			"version":      s.d.Version,
		})
	})
	r.Post("/setup", s.setup)
	r.Post("/login", s.login)
	r.Post("/logout", func(w http.ResponseWriter, r *http.Request) {
		_ = s.d.Session.Destroy(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	configured, err := adminConfigured(r.Context(), s.d.Q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if configured {
		writeErr(w, http.StatusConflict, "conflict")
		return
	}
	if in.Password != in.Confirm || len(in.Password) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if err := s.d.Q.UpsertAdmin(r.Context(), gen.UpsertAdminParams{PasswordHash: hash, CreatedAt: time.Now().Unix()}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := s.d.Session.RenewToken(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.d.Session.Put(r.Context(), "admin_authed", true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	admin, err := s.d.Q.GetAdmin(r.Context())
	if err != nil || auth.VerifyPassword(admin.PasswordHash, in.Password) != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := s.d.Session.RenewToken(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.d.Session.Put(r.Context(), "admin_authed", true)
	w.WriteHeader(http.StatusNoContent)
}

// routesOIDC registers GET /auth/oidc/login and /callback. With SSO off both
// redirect to /.
func (s *server) routesOIDC(r chi.Router) {
	var hs *oidc.HandshakeStore
	if s.d.DB != nil {
		hs = oidc.NewHandshakeStore(s.d.DB)
	}
	enabled := func() bool { return s.d.OIDC != nil && s.d.OIDC.Enabled() && hs != nil }

	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		if !enabled() {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		state, nonce, verifier := oidc.NewState(), oidc.NewState(), oidc.NewVerifier()
		if err := hs.Put(r.Context(), state, nonce, verifier, 5*time.Minute); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: oidcStateCookie, Value: state, Path: "/auth/oidc/", MaxAge: 300,
			HttpOnly: true, Secure: s.d.SecureCookies, SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, s.d.OIDC.AuthCodeURL(state, nonce, oidc.Challenge(verifier)), http.StatusSeeOther)
	})

	r.Get("/callback", func(w http.ResponseWriter, r *http.Request) {
		if !enabled() {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: oidcStateCookie, Path: "/auth/oidc/", MaxAge: -1,
			HttpOnly: true, Secure: s.d.SecureCookies, SameSite: http.SameSiteLaxMode,
		})
		fail := func(reason string, err error) {
			slog.Warn("oidc callback failed", "reason", reason, "err", err)
			http.Redirect(w, r, "/login?login_error=oidc", http.StatusSeeOther)
		}
		cookie, err := r.Cookie(oidcStateCookie)
		queryState := r.URL.Query().Get("state")
		if err != nil || cookie.Value == "" || cookie.Value != queryState {
			fail("session expired", nil)
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			fail("denied by provider", fmt.Errorf("idp error: %s", e))
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			fail("missing code", nil)
			return
		}
		nonce, verifier, err := hs.Take(r.Context(), queryState)
		if err != nil {
			fail("session expired", err)
			return
		}
		claims, err := s.d.OIDC.ExchangeAndVerify(r.Context(), code, verifier, nonce)
		if err != nil {
			fail("sign-in failed", err)
			return
		}
		if err := s.d.OIDC.Authorize(claims); err != nil {
			fail("account not allowed", err)
			return
		}
		if err := s.d.Session.RenewToken(r.Context()); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
		s.d.Session.Put(r.Context(), "admin_authed", true)
		identity := claims.Email
		if identity == "" {
			identity = claims.Subject
		}
		s.d.Session.Put(r.Context(), "auth_identity", identity)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}
