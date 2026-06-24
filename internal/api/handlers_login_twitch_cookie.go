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

// importCookie parses cookie bytes (pickle or netscape), persists the session,
// and runs the side effects. Returns (verified, errCode). errCode "" = success.
func (d *loginTwitchCookieDeps) importCookie(ctx context.Context, id string, data []byte) (bool, string) {
	var authToken string
	if cookies, perr := twitch.ParsePickleCookies(data); perr == nil {
		authToken = cookies["auth-token"]
	} else if tok, nerr := twitchAuthTokenFromNetscape(string(data)); nerr == nil {
		authToken = tok
	} else {
		slog.Error("parse cookie file failed (pickle + netscape)", "account", id, "pickle_err", perr, "netscape_err", nerr)
		return false, "parse_pickle"
	}
	if authToken == "" {
		return false, "no_auth_token"
	}
	sess := platform.Session{
		AccessToken: authToken,
		Cookies:     twitch.SynthCookieBlob(authToken),
		ExpiresAt:   time.Now().Add(60 * 24 * time.Hour),
	}
	backend, _ := d.registry.Get("twitch")
	verified := false
	if backend != nil {
		if v, ok := backend.(interface {
			VerifyAuth(context.Context, platform.Session) error
		}); ok {
			if err := v.VerifyAuth(ctx, sess); err == nil {
				verified = true
				slog.Info("twitch cookie verified", "kind", "auth", "account", id)
			} else {
				slog.Warn("twitch cookie verify failed; persisting anyway", "kind", "auth", "account", id, "err", err)
			}
		}
	}
	if err := d.sessions.Put(d.rootCtx, id, sess); err != nil {
		slog.Error("persist twitch session failed", "account", id, "err", err)
		return false, "persist"
	}
	slog.Info("twitch session persisted from cookie file", "account", id)
	sess.AccountID = id
	if backend != nil {
		fetchAndStoreAvatar(d.rootCtx, d.q, backend, id, sess)
	}
	type authInvalidator interface{ InvalidateAuth(string) }
	if inv, ok := backend.(authInvalidator); ok {
		inv.InvalidateAuth(id)
		slog.Info("twitch backend auth cache invalidated", "account", id)
	}
	if d.reload != nil {
		if err := d.reload(d.rootCtx); err != nil {
			slog.Error("scheduler reload after twitch login failed", "account", id, "err", err)
		} else {
			slog.Info("scheduler reloaded after twitch login", "account", id)
		}
	}
	return verified, ""
}

func (d *loginTwitchCookieDeps) post(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Limit body to 1MB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Parse multipart form
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.parse_form")
		return
	}

	// Migration from DevilXD / rangermix TwitchDropsMiner: the user uploads
	// their TDM cookies.jar (a Python pickle of an http.cookies.SimpleCookie).
	// TDM mints its auth-token under the same Android client_id GrubDrops uses,
	// so the token is integrity-exempt and works for mining. We try pickle
	// first, falling back to Netscape cookies.txt for flexibility.
	file, _, ferr := r.FormFile("cookie_file")
	if ferr != nil {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.no_file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error.read_file")
		return
	}

	_, code := d.importCookie(r.Context(), id, data)
	if code != "" {
		d.renderError(w, r, id, acc.DisplayName, "login_twitch_cookie.error."+code)
		return
	}

	// Redirect to accounts page
	d.sm.Put(r.Context(), "flash", "flash.twitch_cookie_imported")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d *loginTwitchCookieDeps) apiCookieImport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeAPIError(w, http.StatusBadRequest, "parse_form", "could not parse form")
		return
	}
	file, _, ferr := r.FormFile("cookie_file")
	if ferr != nil {
		writeAPIError(w, http.StatusBadRequest, "no_file", "no cookie file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "read_file", "could not read file")
		return
	}
	verified, code := d.importCookie(r.Context(), id, data)
	if code != "" {
		status := http.StatusBadRequest
		if code == "persist" {
			status = http.StatusInternalServerError
		}
		writeAPIError(w, status, code, code)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verified": verified})
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
