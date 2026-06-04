package api

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"
)

type ctxKey int

const (
	ctxAdminAuthed ctxKey = iota
)

// RequireAdmin redirects unauthenticated users to /login.
func RequireAdmin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
