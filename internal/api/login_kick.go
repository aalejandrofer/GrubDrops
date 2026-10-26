package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// kickCookieForm carries the cookie/channel fields a Kick login submits.
type kickCookieForm struct {
	KickSession  string
	XSRF         string
	CFClearance  string
	SessionToken string
	Channels     []string
}

type cookieStored struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

type kickSessionForStorage struct {
	Cookies   []cookieStored `json:"cookies"`
	XSRFToken string         `json:"xsrf_token"`
	// Channel mirrors Channels[0] for sessions read by older code.
	Channel  string   `json:"channel"`
	Channels []string `json:"channels,omitempty"`
	Username string   `json:"username"`
}

// kickAuthVerifier verifies stored cookies over the kick backend's utls
// transport (no browser sidecar). Satisfied by *kick.Backend.
type kickAuthVerifier interface {
	VerifyAuth(ctx context.Context, s platform.Session) error
}

func (s *server) kickSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, ok := s.account(w, r, id)
	if !ok {
		return
	}
	if acc.Platform != "kick" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	var body struct {
		CookiesTxt string   `json:"cookies_txt"`
		Channels   []string `json:"channels"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	form, err := kickCookiesFromNetscape(body.CookiesTxt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request", "message": err.Error()})
		return
	}
	form.Channels = cleanKickChannels(body.Channels)
	verified, err := s.persistKickSession(r.Context(), id, form)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"verified": verified})
}

// persistKickSession stores the Kick cookies, verifies them best-effort over
// the utls transport, backfills the avatar when verified, registers the
// channel list and announces the change. Cookies persist regardless of the
// verify result.
func (s *server) persistKickSession(ctx context.Context, id string, f kickCookieForm) (bool, error) {
	stored := []cookieStored{
		{Name: "kick_session", Value: f.KickSession, Domain: "kick.com", Path: "/"},
		{Name: "XSRF-TOKEN", Value: f.XSRF, Domain: "kick.com", Path: "/"},
	}
	if f.CFClearance != "" {
		stored = append(stored, cookieStored{Name: "cf_clearance", Value: f.CFClearance, Domain: "kick.com", Path: "/"})
	}
	if f.SessionToken != "" {
		stored = append(stored, cookieStored{Name: "session_token", Value: f.SessionToken, Domain: "kick.com", Path: "/"})
	}
	slog.Info("kick login attempt", "kind", "auth", "account", id, "platform", "kick", "channels", f.Channels, "has_cf_clearance", f.CFClearance != "", "has_session_token", f.SessionToken != "")

	first := ""
	if len(f.Channels) > 0 {
		first = f.Channels[0]
	}
	raw, _ := json.Marshal(kickSessionForStorage{
		Cookies: stored, XSRFToken: f.XSRF, Channel: first, Channels: f.Channels,
	})
	sess := platform.Session{
		AccountID: id,
		Cookies:   map[string]string{"kick": string(raw)},
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.d.Sessions.Put(ctx, id, sess); err != nil {
		slog.Error("persist kick session failed", "kind", "error", "account", id, "err", err)
		return false, err
	}

	verified := false
	if v, ok := s.d.Registrar.(kickAuthVerifier); ok {
		if err := v.VerifyAuth(ctx, sess); err != nil {
			slog.Warn("kick utls verify failed; persisting anyway", "kind", "auth", "account", id, "err", err)
		} else {
			verified = true
			slog.Info("kick utls verified", "kind", "auth", "account", id)
		}
	}
	if verified {
		if b, ok := s.d.Registrar.(platform.Backend); ok {
			fetchAndStoreAvatar(s.rootCtx(), s.d.Q, b, id, sess)
		}
	}
	if s.d.Registrar != nil {
		s.d.Registrar.RegisterChannels(id, f.Channels)
	}
	s.accountChanged(id)
	s.rerunSkippedCanary(ctx, "kick")
	return verified, nil
}

// cleanKickChannels trims, drops empties and de-duplicates case-insensitively.
func cleanKickChannels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		key := strings.ToLower(c)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}
