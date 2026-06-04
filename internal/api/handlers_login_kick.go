package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	pb "github.com/chano-fernandez/rust-drops-miner/internal/auth/browser/gen/browser/v1"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/store"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

// KickBrowserClient is the surface the Kick login handler depends on.
// Defined as an interface so tests don't need the full gRPC stack.
type KickBrowserClient interface {
	Authenticate(ctx context.Context, s *pb.KickSession) (*pb.AuthenticateResponse, error)
}

// KickChannelRegistrar is implemented by the kick.Backend so the
// handler can stash the channel selection at login time.
type KickChannelRegistrar interface {
	RegisterChannel(accountID, channel string)
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
	channel := r.FormValue("channel")

	pbSession := &pb.KickSession{
		Cookies: []*pb.Cookie{
			{Name: "kick_session", Value: kickSessionCookie, Domain: "kick.com", Path: "/"},
			{Name: "XSRF-TOKEN", Value: xsrf, Domain: "kick.com", Path: "/"},
			{Name: "cf_clearance", Value: cfClearance, Domain: "kick.com", Path: "/"},
		},
		XsrfToken: xsrf,
	}

	resp, err := d.browser.Authenticate(r.Context(), pbSession)
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "sidecar rejected session: "+err.Error())
		return
	}

	internal := kickSessionForStorage{
		Cookies: []cookieStored{
			{Name: "kick_session", Value: kickSessionCookie, Domain: "kick.com", Path: "/"},
			{Name: "XSRF-TOKEN", Value: xsrf, Domain: "kick.com", Path: "/"},
			{Name: "cf_clearance", Value: cfClearance, Domain: "kick.com", Path: "/"},
		},
		XSRFToken: xsrf,
		Channel:   channel,
		Username:  resp.Username,
	}
	raw, _ := json.Marshal(internal)
	if err := d.sessions.Put(r.Context(), id, platform.Session{
		Cookies:   map[string]string{"kick": string(raw)},
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); err != nil {
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}

	if d.registrar != nil {
		d.registrar.RegisterChannel(id, channel)
	}

	d.sm.Put(r.Context(), "flash", "Kick session authorized for "+resp.Username+" — click Apply changes")
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
	Channel   string         `json:"channel"`
	Username  string         `json:"username"`
}

type cookieStored struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}
