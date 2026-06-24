package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
)

type ctxKey int

const (
	ctxAdminAuthed ctxKey = iota
)

// RequireAdmin redirects unauthenticated users to /login.
//
// If the env var GRUB_AUTH_BYPASS_LOCAL=1 is set, requests whose
// X-Forwarded-For chain originates from a loopback address (or that
// have no XFF and connect from loopback themselves) are allowed
// through without a session. Intended for `curl localhost:8080` from
// the homelab host for debugging — leave disabled in normal operation.
//
// GRUB_AUTHBYPASS=true is a BIGGER hammer: it disables auth for EVERY
// request (no login at all). Intended only for staging/dev where the app
// is otherwise unreachable behind a proxy. NEVER set it in production.
// adminAllowed reports whether the request should be treated as an
// authenticated admin, honoring the bypass flags. Shared by RequireAdmin
// (redirect on false) and RequireAdminAPI (401 JSON on false) so the two
// cannot drift.
func adminAllowed(r *http.Request, sm *scs.SessionManager, bypassAll, bypassLocal bool) bool {
	if bypassAll || (bypassLocal && isLoopbackRequest(r)) {
		return true
	}
	return sm.GetBool(r.Context(), "admin_authed")
}

func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	bypassLocal := os.Getenv("GRUB_AUTH_BYPASS_LOCAL") == "1"
	bypassAll := envTrue("GRUB_AUTHBYPASS")
	if bypassAll {
		slog.Warn("GRUB_AUTHBYPASS is set — ALL authentication is DISABLED; every request is treated as admin.", "kind", "auth")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !adminAllowed(r, sm, bypassAll, bypassLocal) {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), ctxAdminAuthed, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdminAPI is the JSON-API counterpart of RequireAdmin: on an
// unauthenticated request it returns 401 with a JSON error envelope
// instead of a 302 redirect to /login, so a browser fetch() can detect
// session expiry rather than silently receiving the login HTML.
func RequireAdminAPI(sm *scs.SessionManager) func(http.Handler) http.Handler {
	bypassLocal := os.Getenv("GRUB_AUTH_BYPASS_LOCAL") == "1"
	bypassAll := envTrue("GRUB_AUTHBYPASS")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !adminAllowed(r, sm, bypassAll, bypassLocal) {
				writeAPIError(w, http.StatusUnauthorized, "unauthorized", "login required")
				return
			}
			ctx := context.WithValue(r.Context(), ctxAdminAuthed, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isLoopbackRequest distinguishes "direct localhost curl on the host"
// from "request that came through Traefik / from the LAN".
//
//   - Direct host curl: no X-Forwarded-For; RemoteAddr is the docker
//     bridge gateway (172.17.0.1 etc) because docker-proxy forwards
//     127.0.0.1:8080 traffic in via the bridge.
//   - Traefik traffic: X-Forwarded-For contains the original client
//     IP (LAN or public). Even loopback LAN clients reach us via
//     Traefik, so an XFF header always implies "external".
//
// Therefore we only accept "no XFF + private RemoteAddr" as local.
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

// CSRF wraps mutating endpoints. Get-only handlers do not need it but
// nosurf gracefully passes them through.
//
// secureCookies mirrors GRUB_SECURE_COOKIES and tells us whether the app is
// reached over HTTPS. It matters because nosurf's same-origin check compares
// the request's Origin/Referer scheme against a "self" origin it builds from
// r.Host. nosurf v1.2 defaults that self-scheme to https unconditionally
// (isTLS == true), so a plain-HTTP self-host (http://pi:8080, the default
// config) gets a self-origin of https://pi:8080, which never matches the
// browser-sent http://pi:8080 Origin/Referer — every POST then fails with
// "CSRF token invalid". We instead derive the scheme from the actual request
// so it matches what the browser used. This does NOT weaken CSRF: the
// same-origin requirement and the masked-token cookie/form comparison are
// still fully enforced; we only stop misreporting our own scheme.
func CSRF(secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := nosurf.New(next)
		h.SetIsTLSFunc(func(r *http.Request) bool { return requestIsHTTPS(r, secureCookies) })
		h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reason := nosurf.Reason(r)
			https := requestIsHTTPS(r, secureCookies)
			slog.Warn("csrf check failed",
				"reason", reason,
				"method", r.Method,
				"path", r.URL.Path,
				"origin", r.Header.Get("Origin"),
				"referer", r.Header.Get("Referer"),
				"host", r.Host,
				"x_forwarded_proto", r.Header.Get("X-Forwarded-Proto"),
				"treated_as_https", https,
				"secure_cookies", secureCookies,
			)
			// Surface the most likely self-host misconfiguration so the next
			// person can diagnose it without reading the source.
			lang := i18n.DetectLang(r)
			hint := i18n.T(lang, "error.csrf_reload")
			if !secureCookies && https {
				hint = i18n.T(lang, "error.csrf_https_no_secure")
			} else if secureCookies && !https {
				hint = i18n.T(lang, "error.csrf_secure_http")
			}
			http.Error(w, i18n.T(lang, "error.csrf_invalid")+" — "+hint, http.StatusForbidden)
		}))
		return h
	}
}

// requestIsHTTPS reports whether the user reached the app over HTTPS, for the
// purpose of nosurf's same-origin scheme comparison. We trust an explicit
// X-Forwarded-Proto only when secure cookies are enabled (i.e. the operator
// has declared an HTTPS deployment, typically a TLS-terminating reverse
// proxy); otherwise an attacker who can reach the app directly could spoof
// the header. With secure cookies off (the plain-HTTP default) we report the
// real transport, so a http:// self-host gets a matching http:// self-origin.
func requestIsHTTPS(r *http.Request, secureCookies bool) bool {
	if r.TLS != nil {
		return true
	}
	if secureCookies {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			return strings.EqualFold(strings.TrimSpace(firstField(proto)), "https")
		}
		// Operator declared HTTPS but the proxy sent no proto header; assume
		// the declared posture so the scheme check still lines up.
		return true
	}
	return false
}

// firstField returns the first comma-separated token (X-Forwarded-Proto may be
// a list when chained through multiple proxies, e.g. "https, http").
func firstField(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[:i]
	}
	return s
}

// envTrue reports whether an env var is set to a truthy value
// (1/true/yes/on, case-insensitive).
func envTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func csrfToken(r *http.Request) string {
	return nosurf.Token(r)
}

func isAdminAuthed(r *http.Request) bool {
	v, _ := r.Context().Value(ctxAdminAuthed).(bool)
	return v
}
