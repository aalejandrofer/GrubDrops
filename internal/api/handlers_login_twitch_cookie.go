package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type loginTwitchCookieDeps struct {
	q        *gen.Queries
	t        Renderer
	sm       *scs.SessionManager
	sessions *store.SessionStore
	registry *platform.Registry
	reload   func(context.Context) error
	rootCtx  context.Context
}

type loginTwitchCookiePageData struct {
	AccountID   string
	DisplayName string
	Flash       string
}

func newLoginTwitchCookieDeps(d Deps, rootCtx context.Context) *loginTwitchCookieDeps {
	return &loginTwitchCookieDeps{
		q:        d.Q,
		t:        d.Templates,
		sm:       d.Session,
		sessions: d.Sessions,
		registry: d.Registry,
		reload:   d.Reload,
		rootCtx:  rootCtx,
	}
}

func (d *loginTwitchCookieDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, d.t, "login_twitch_cookie.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginTwitchCookiePageData{
			AccountID:   id,
			DisplayName: acc.DisplayName,
		},
	})
}

func (d *loginTwitchCookieDeps) post(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Limit file size to 1MB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Parse multipart form
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.parse_form")
		return
	}

	// Get uploaded file
	file, _, err := r.FormFile("cookie_file")
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.no_file")
		return
	}
	defer file.Close()

	// Read file content
	data, err := io.ReadAll(file)
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.read_file")
		return
	}

	// Parse pickle cookies
	cookies, err := twitch.ParsePickleCookies(data)
	if err != nil {
		slog.Error("parse pickle cookies failed", "account", id, "err", err)
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.parse_pickle")
		return
	}

	// Check for auth-token
	authToken, ok := cookies["auth-token"]
	if !ok || authToken == "" {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.no_auth_token")
		return
	}

	// Create session from cookies
	sess := platform.Session{
		AccessToken: authToken,
		Cookies:     twitch.SynthCookieBlob(authToken),
		ExpiresAt:   time.Now().Add(60 * 24 * time.Hour), // 60 days
	}

	// Try to verify the token
	backend, ok := d.registry.Get("twitch")
	if ok {
		if v, ok := backend.(interface {
			VerifyAuth(context.Context, platform.Session) error
		}); ok {
			if err := v.VerifyAuth(r.Context(), sess); err != nil {
				slog.Warn("twitch cookie verify failed; persisting anyway", "kind", "auth", "account", id, "err", err)
				// Continue anyway - the token might still work for some operations
			} else {
				slog.Info("twitch cookie verified", "kind", "auth", "account", id)
			}
		}
	}

	// Save session
	if err := d.sessions.Put(d.rootCtx, id, sess); err != nil {
		slog.Error("persist twitch session failed", "account", id, "err", err)
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.persist")
		return
	}
	slog.Info("twitch session persisted from cookie file", "account", id)

	// Backfill avatar
	sess.AccountID = id
	fetchAndStoreAvatar(d.rootCtx, d.q, backend, id, sess)

	// Invalidate auth cache
	type authInvalidator interface{ InvalidateAuth(string) }
	if inv, ok := backend.(authInvalidator); ok {
		inv.InvalidateAuth(id)
		slog.Info("twitch backend auth cache invalidated", "account", id)
	}

	// Reload scheduler
	if d.reload != nil {
		if err := d.reload(d.rootCtx); err != nil {
			slog.Error("scheduler reload after twitch login failed", "account", id, "err", err)
		} else {
			slog.Info("scheduler reloaded after twitch login", "account", id)
		}
	}

	// Redirect to accounts page
	d.sm.Put(r.Context(), "flash", "flash.twitch_cookie_imported")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d *loginTwitchCookieDeps) renderError(w http.ResponseWriter, r *http.Request, id, name, flash string) {
	render(w, r, d.t, "login_twitch_cookie.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginTwitchCookiePageData{
			AccountID:   id,
			DisplayName: name,
			Flash:       flash,
		},
	})
}
