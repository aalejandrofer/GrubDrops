package api

import (
	"context"
	"net"
	"net/http"
	"os"

	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"
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
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	bypassLocal := os.Getenv("GRUB_AUTH_BYPASS_LOCAL") == "1"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bypassLocal && isLoopbackRequest(r) {
				ctx := context.WithValue(r.Context(), ctxAdminAuthed, true)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			authed := sm.GetBool(r.Context(), "admin_authed")
			if !authed {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
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
func CSRF(next http.Handler) http.Handler {
	h := nosurf.New(next)
	h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "CSRF token invalid", http.StatusForbidden)
	}))
	return h
}

func csrfToken(r *http.Request) string {
	return nosurf.Token(r)
}

func isAdminAuthed(r *http.Request) bool {
	v, _ := r.Context().Value(ctxAdminAuthed).(bool)
	return v
}
