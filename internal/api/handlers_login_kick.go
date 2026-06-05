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

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
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
	if d.browser == nil {
		http.Error(w, "browser sidecar not configured (set MINER_BROWSER_URL)", http.StatusServiceUnavailable)
		return
	}
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
	if d.browser == nil {
		http.Error(w, "browser sidecar not configured (set MINER_BROWSER_URL)", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	kickSessionCookie := r.FormValue("kick_session")
	xsrf := r.FormValue("xsrf_token")
	cfClearance := r.FormValue("cf_clearance")
	channels := parseKickChannels(r.FormValue("channel"))

	cookies := []*pb.Cookie{
		{Name: "kick_session", Value: kickSessionCookie, Domain: "kick.com", Path: "/"},
		{Name: "XSRF-TOKEN", Value: xsrf, Domain: "kick.com", Path: "/"},
	}
	stored := []cookieStored{
		{Name: "kick_session", Value: kickSessionCookie, Domain: "kick.com", Path: "/"},
		{Name: "XSRF-TOKEN", Value: xsrf, Domain: "kick.com", Path: "/"},
	}
	if cfClearance != "" {
		cookies = append(cookies, &pb.Cookie{Name: "cf_clearance", Value: cfClearance, Domain: "kick.com", Path: "/"})
		stored = append(stored, cookieStored{Name: "cf_clearance", Value: cfClearance, Domain: "kick.com", Path: "/"})
	}

	pbSession := &pb.KickSession{Cookies: cookies, XsrfToken: xsrf}
	slog.Info("kick login attempt", "kind", "auth", "account", id, "platform", "kick", "channels", channels, "cookie_count", len(cookies), "has_cf_clearance", cfClearance != "", "kick_session_len", len(kickSessionCookie), "xsrf_len", len(xsrf))

	// Same pattern as the Twitch paste handler: persist cookies even
	// when the sidecar can't verify them right now. Verification can
	// fail transiently (Cloudflare interstitial, missing channel, JS
	// challenge needs a moment) — we don't want to throw away the
	// operator's paste because of a flaky probe. The watcher retries
	// on its own clock and surfaces needs_auth if cookies really are
	// invalid.
	var username string
	resp, vErr := d.browser.Authenticate(r.Context(), pbSession)
	if vErr != nil {
		slog.Warn("kick sidecar verify failed; persisting cookies anyway", "kind", "auth", "account", id, "platform", "kick", "err", vErr)
	} else {
		username = resp.Username
		slog.Info("kick sidecar verified", "kind", "auth", "account", id, "platform", "kick", "username", username)
	}

	legacyChannel := ""
	if len(channels) > 0 {
		legacyChannel = channels[0]
	}
	internal := kickSessionForStorage{
		Cookies:   stored,
		XSRFToken: xsrf,
		Channel:   legacyChannel, // back-compat: first entry mirrors old single-channel field
		Channels:  channels,
		Username:  username,
	}
	raw, _ := json.Marshal(internal)
	if err := d.sessions.Put(r.Context(), id, platform.Session{
		Cookies:   map[string]string{"kick": string(raw)},
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); err != nil {
		slog.Error("persist kick session failed", "account", id, "err", err)
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}
	slog.Info("kick session persisted", "account", id, "channels", channels, "username", username)

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

	flash := "Kick cookies persisted"
	if username != "" {
		flash = "Kick session authorized for " + username
	} else {
		flash += " — sidecar could not verify yet; watcher will retry"
	}
	d.sm.Put(r.Context(), "flash", flash)
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
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
