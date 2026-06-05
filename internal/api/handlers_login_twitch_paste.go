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

	pb "github.com/aalejandrofer/rust-drops-miner/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/store"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

// TwitchBrowserClient is the surface the Twitch cookie-paste handler
// needs from the sidecar (`*browser.Client`). Defined as an interface
// so tests can stub it out.
type TwitchBrowserClient interface {
	TwitchAuthenticate(ctx context.Context, accountID string, s *pb.TwitchSession) (*pb.TwitchAuthenticateResponse, error)
}

type loginTwitchPasteDeps struct {
	q        *gen.Queries
	t        Renderer
	sm       *scs.SessionManager
	sessions *store.SessionStore
	browser  TwitchBrowserClient
	reload   func(context.Context) error
}

type loginTwitchPasteData struct {
	AccountID   string
	DisplayName string
}

func (d *loginTwitchPasteDeps) get(w http.ResponseWriter, r *http.Request) {
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
	render(w, d.t, "login_twitch_paste.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginTwitchPasteData{AccountID: id, DisplayName: acc.DisplayName},
	})
}

func (d *loginTwitchPasteDeps) post(w http.ResponseWriter, r *http.Request) {
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

	cookies := parseTwitchCookieForm(r)
	if len(cookies) == 0 || !hasCookie(cookies, "auth-token") {
		d.renderError(w, r, id, acc.DisplayName, "auth-token cookie is required")
		return
	}

	sess := &pb.TwitchSession{Cookies: cookies}
	slog.Info("twitch paste login attempt", "account", id, "cookie_count", len(cookies))

	// Try to verify via the sidecar, but DO NOT block persistence on
	// failure — the most common cause of failure (PerimeterX shimming
	// fetch in the headless tab) is transient and recoverable. The
	// watcher will retry the auth flow on the next reload; until then
	// the dashboard surfaces "needs_auth" so the operator sees it.
	var username, userID string
	resp, vErr := d.browser.TwitchAuthenticate(r.Context(), id, sess)
	if vErr != nil {
		slog.Warn("twitch sidecar verify failed; persisting cookies anyway", "account", id, "err", vErr)
	} else {
		username, userID = resp.Username, resp.UserId
		slog.Info("twitch sidecar verified", "account", id, "username", username, "user_id", userID)
	}

	stored := twitchPastedSession{
		Cookies:  pbToStored(cookies),
		Username: username,
		UserID:   userID,
	}
	raw, _ := json.Marshal(stored)
	if err := d.sessions.Put(r.Context(), id, platform.Session{
		Cookies:   map[string]string{"twitch": string(raw)},
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}); err != nil {
		slog.Error("persist twitch session failed", "account", id, "err", err)
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}
	slog.Info("twitch session persisted", "account", id)

	if d.reload != nil {
		if err := d.reload(r.Context()); err != nil {
			slog.Error("scheduler reload after twitch paste failed", "account", id, "err", err)
		} else {
			slog.Info("scheduler reloaded after twitch paste", "account", id)
		}
	}

	flash := "Twitch cookies persisted"
	if username != "" {
		flash = "Twitch session authorized for " + username
	} else {
		flash += " — sidecar could not verify yet (likely PerimeterX); watcher will retry on next reload"
	}
	d.sm.Put(r.Context(), "flash", flash)
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (d *loginTwitchPasteDeps) renderError(w http.ResponseWriter, r *http.Request, id, name, flash string) {
	render(w, d.t, "login_twitch_paste.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page:  loginTwitchPasteData{AccountID: id, DisplayName: name},
		Flash: flash,
	})
}

// parseTwitchCookieForm extracts the cookie set the sidecar needs.
// Accepts named text fields for the well-known cookies plus an
// optional "raw" textarea where the user can paste DevTools' "name=value;
// name=value" line (or one cookie per line).
func parseTwitchCookieForm(r *http.Request) []*pb.Cookie {
	known := []string{"auth-token", "persistent", "server_session_id", "twilight-user", "unique_id", "login", "name"}
	out := make([]*pb.Cookie, 0, len(known))
	seen := map[string]bool{}
	for _, k := range known {
		v := strings.TrimSpace(r.FormValue(k))
		if v == "" {
			continue
		}
		out = append(out, &pb.Cookie{Name: k, Value: v, Domain: ".twitch.tv", Path: "/"})
		seen[k] = true
	}
	if raw := strings.TrimSpace(r.FormValue("raw")); raw != "" {
		for _, line := range strings.Split(strings.ReplaceAll(raw, ";", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			eq := strings.IndexByte(line, '=')
			if eq <= 0 {
				continue
			}
			name := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])
			if name == "" || val == "" || seen[name] {
				continue
			}
			out = append(out, &pb.Cookie{Name: name, Value: val, Domain: ".twitch.tv", Path: "/"})
			seen[name] = true
		}
	}
	return out
}

func hasCookie(cs []*pb.Cookie, name string) bool {
	for _, c := range cs {
		if c.Name == name {
			return true
		}
	}
	return false
}

type twitchPastedSession struct {
	Cookies  []cookieStored `json:"cookies"`
	Username string         `json:"username"`
	UserID   string         `json:"user_id"`
}

func pbToStored(cs []*pb.Cookie) []cookieStored {
	out := make([]cookieStored, 0, len(cs))
	for _, c := range cs {
		out = append(out, cookieStored{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path})
	}
	return out
}
