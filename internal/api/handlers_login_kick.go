package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	pb "github.com/aalejandrofer/grubdrops/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// KickBrowserClient is the surface the Kick login handler depends on.
// Defined as an interface so tests don't need the full gRPC stack.
type KickBrowserClient interface {
	Authenticate(ctx context.Context, s *pb.KickSession) (*pb.AuthenticateResponse, error)
}

// KickChannelRegistrar is implemented by the kick.Backend so the
// handler can stash the channel selection at login time. RegisterChannels
// replaces the account's entire list; pass nil/empty to unregister.
type KickChannelRegistrar interface {
	RegisterChannels(accountID string, channels []string)
}

// unexported aliases used within the package
type kickBrowserClient = KickBrowserClient
type kickChannelRegistrar = KickChannelRegistrar

type loginKickDeps struct {
	q         *gen.Queries
	t         Renderer
	sm        *scs.SessionManager
	sessions  *store.SessionStore
	browser   kickBrowserClient
	registrar kickChannelRegistrar
	reload    func(context.Context) error
}

type loginKickPageData struct {
	AccountID   string
	DisplayName string
}

func (d *loginKickDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, d.t, "login_kick.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginKickPageData{AccountID: id, DisplayName: acc.DisplayName},
	})
}

func (d *loginKickDeps) post(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	verified, err := d.persistKickSession(r.Context(), id, kickCookieForm{
		KickSession:  r.FormValue("kick_session"),
		XSRF:         r.FormValue("xsrf_token"),
		CFClearance:  r.FormValue("cf_clearance"),
		SessionToken: r.FormValue("session_token"),
		Channels:     parseKickChannels(r.FormValue("channel")),
	})
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}

	flash := "Kick cookies persisted — watcher will verify shortly"
	if verified {
		flash = "Kick session verified ✓"
	}
	d.sm.Put(r.Context(), "flash", flash)
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

// helperIngest is the no-auth cookie upload used by grubdrops-helper. It is
// NOT behind the admin session or CSRF: the unguessable acc_<24hex> ID in the
// path is the only credential (404 on an unknown ID). The helper runs on a
// friend's machine and can't carry an admin cookie, so the random account ID
// is the bearer secret — security by obscurity on 96 bits.
func (d *loginKickDeps) helperIngest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r) // unknown ID — don't reveal whether it could exist
		return
	}
	if acc.Platform != "kick" {
		http.Error(w, "account is not a kick account", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := d.persistKickSession(r.Context(), id, kickCookieForm{
		KickSession:  r.FormValue("kick_session"),
		XSRF:         r.FormValue("xsrf_token"),
		CFClearance:  r.FormValue("cf_clearance"),
		SessionToken: r.FormValue("session_token"),
		Channels:     parseKickChannels(r.FormValue("channel")),
	}); err != nil {
		http.Error(w, "persist session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// kickCookieForm carries the cookie/channel fields a Kick login submits,
// shared by the authed HTML handler and the no-auth helper endpoint.
type kickCookieForm struct {
	KickSession  string
	XSRF         string
	CFClearance  string
	SessionToken string
	Channels     []string
}

// persistKickSession stores the Kick cookies, registers the channel list,
// reloads the scheduler, and best-effort verifies over the utls transport.
// Returns whether verification succeeded (cookies are persisted regardless).
func (d *loginKickDeps) persistKickSession(ctx context.Context, id string, f kickCookieForm) (bool, error) {
	stored := []cookieStored{
		{Name: "kick_session", Value: f.KickSession, Domain: "kick.com", Path: "/"},
		{Name: "XSRF-TOKEN", Value: f.XSRF, Domain: "kick.com", Path: "/"},
	}
	if f.CFClearance != "" {
		stored = append(stored, cookieStored{Name: "cf_clearance", Value: f.CFClearance, Domain: "kick.com", Path: "/"})
	}
	if f.SessionToken != "" {
		// The Sanctum bearer for authed drops calls (progress/claim) — the
		// utls transport extracts it from this cookie.
		stored = append(stored, cookieStored{Name: "session_token", Value: f.SessionToken, Domain: "kick.com", Path: "/"})
	}

	slog.Info("kick login attempt", "kind", "auth", "account", id, "platform", "kick", "channels", f.Channels, "has_cf_clearance", f.CFClearance != "", "has_session_token", f.SessionToken != "", "kick_session_len", len(f.KickSession), "xsrf_len", len(f.XSRF))

	legacyChannel := ""
	if len(f.Channels) > 0 {
		legacyChannel = f.Channels[0]
	}
	internal := kickSessionForStorage{
		Cookies:   stored,
		XSRFToken: f.XSRF,
		Channel:   legacyChannel, // back-compat: first entry mirrors old single-channel field
		Channels:  f.Channels,
	}
	raw, _ := json.Marshal(internal)
	sess := platform.Session{
		AccountID: id,
		Cookies:   map[string]string{"kick": string(raw)},
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := d.sessions.Put(ctx, id, sess); err != nil {
		slog.Error("persist kick session failed", "account", id, "err", err)
		return false, err
	}
	slog.Info("kick session persisted", "account", id, "channels", f.Channels)

	// Verify over the utls transport (no browser sidecar). A transient
	// failure isn't fatal — cookies are already persisted and the watcher
	// retries; we only use the result for the flash message.
	verified := false
	if v, ok := d.registrar.(kickAuthVerifier); ok {
		if err := v.VerifyAuth(ctx, sess); err != nil {
			slog.Warn("kick utls verify failed; persisting anyway", "kind", "auth", "account", id, "err", err)
		} else {
			verified = true
			slog.Info("kick utls verified", "kind", "auth", "account", id)
		}
	}

	if d.registrar != nil {
		d.registrar.RegisterChannels(id, f.Channels)
	}

	if d.reload != nil {
		if err := d.reload(ctx); err != nil {
			slog.Error("scheduler reload after kick login failed", "account", id, "err", err)
		} else {
			slog.Info("scheduler reloaded after kick login", "account", id)
		}
	}
	return verified, nil
}

// kickAuthVerifier lets the login handler verify pasted cookies over the
// kick.Backend's utls transport (no browser sidecar). Satisfied by
// *kick.Backend (which implements platform.AuthChecker).
type kickAuthVerifier interface {
	VerifyAuth(ctx context.Context, s platform.Session) error
}

func (d *loginKickDeps) renderError(w http.ResponseWriter, r *http.Request, id, name, flash string) {
	render(w, d.t, "login_kick.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page:  loginKickPageData{AccountID: id, DisplayName: name},
		Flash: flash,
	})
}

type kickSessionForStorage struct {
	Cookies   []cookieStored `json:"cookies"`
	XSRFToken string         `json:"xsrf_token"`
	// Channel is the legacy single-channel field. New sessions also
	// populate Channels; readers should prefer Channels when non-empty
	// and fall back to Channel for back-compat with stored sessions
	// written before multi-channel support.
	Channel  string   `json:"channel"`
	Channels []string `json:"channels,omitempty"`
	Username string   `json:"username"`
}

// parseKickChannels splits a free-form channel input into a clean list.
// Accepts commas, whitespace, or both as separators so the operator can
// paste "foo,bar baz" or "foo bar baz" — both yield three channels.
func parseKickChannels(raw string) []string {
	if raw == "" {
		return nil
	}
	splitter := func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r', ';':
			return true
		}
		return false
	}
	parts := strings.FieldsFunc(raw, splitter)
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

type cookieStored struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}
