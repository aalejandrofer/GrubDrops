package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"
)

type ctxKey int

const (
	ctxAdminAuthed ctxKey = iota
)

// RequireAdmin redirects unauthenticated users to /login.
//
// If the env var MINER_AUTH_BYPASS_LOCAL=1 is set, requests whose
// X-Forwarded-For chain originates from a loopback address (or that
// have no XFF and connect from loopback themselves) are allowed
// through without a session. Intended for `curl localhost:8080` from
// the homelab host for debugging — leave disabled in normal operation.
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	bypassLocal := os.Getenv("MINER_AUTH_BYPASS_LOCAL") == "1"
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

func isLoopbackRequest(r *http.Request) bool {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// The first entry is the originating client.
		first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if ip := net.ParseIP(first); ip != nil {
			return ip.IsLoopback()
		}
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
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
