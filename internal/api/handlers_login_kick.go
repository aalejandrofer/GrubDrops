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

	kickSessionCookie := r.FormValue("kick_session")
	xsrf := r.FormValue("xsrf_token")
	cfClearance := r.FormValue("cf_clearance")
	sessionToken := r.FormValue("session_token")
	channels := parseKickChannels(r.FormValue("channel"))

	stored := []cookieStored{
		{Name: "kick_session", Value: kickSessionCookie, Domain: "kick.com", Path: "/"},
		{Name: "XSRF-TOKEN", Value: xsrf, Domain: "kick.com", Path: "/"},
	}
	if cfClearance != "" {
		stored = append(stored, cookieStored{Name: "cf_clearance", Value: cfClearance, Domain: "kick.com", Path: "/"})
	}
	if sessionToken != "" {
		// The Sanctum bearer for authed drops calls (progress/claim) — the
		// utls transport extracts it from this cookie.
		stored = append(stored, cookieStored{Name: "session_token", Value: sessionToken, Domain: "kick.com", Path: "/"})
	}

	slog.Info("kick login attempt", "kind", "auth", "account", id, "platform", "kick", "channels", channels, "has_cf_clearance", cfClearance != "", "has_session_token", sessionToken != "", "kick_session_len", len(kickSessionCookie), "xsrf_len", len(xsrf))

	legacyChannel := ""
	if len(channels) > 0 {
		legacyChannel = channels[0]
	}
	internal := kickSessionForStorage{
		Cookies:   stored,
		XSRFToken: xsrf,
		Channel:   legacyChannel, // back-compat: first entry mirrors old single-channel field
		Channels:  channels,
	}
	raw, _ := json.Marshal(internal)
	sess := platform.Session{
		AccountID: id,
		Cookies:   map[string]string{"kick": string(raw)},
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := d.sessions.Put(r.Context(), id, sess); err != nil {
		slog.Error("persist kick session failed", "account", id, "err", err)
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}
	slog.Info("kick session persisted", "account", id, "channels", channels)

	// Verify over the utls transport (no browser sidecar). A transient
	// failure isn't fatal — cookies are already persisted and the watcher
	// retries; we only use the result for the flash message.
	verified := false
	if v, ok := d.registrar.(kickAuthVerifier); ok {
		if err := v.VerifyAuth(r.Context(), sess); err != nil {
			slog.Warn("kick utls verify failed; persisting anyway", "kind", "auth", "account", id, "err", err)
		} else {
			verified = true
			slog.Info("kick utls verified", "kind", "auth", "account", id)
		}
	}

	if d.registrar != nil {
		d.registrar.RegisterChannels(id, channels)
	}

	if d.reload != nil {
		if err := d.reload(r.Context()); err != nil {
			slog.Error("scheduler reload after kick login failed", "account", id, "err", err)
		} else {
			slog.Info("scheduler reloaded after kick login", "account", id)
		}
	}

	flash := "Kick cookies persisted — watcher will verify shortly"
	if verified {
		flash = "Kick session verified ✓"
	}
	d.sm.Put(r.Context(), "flash", flash)
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
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
