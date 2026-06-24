package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type loginTwitchDeps struct {
	q        *gen.Queries
	t        Renderer
	sm       *scs.SessionManager
	sessions *store.SessionStore
	registry *platform.Registry
	reload   func(context.Context) error
	rootCtx  context.Context

	pending sync.Map // accountID -> *twitchLoginState
}

type twitchLoginState struct {
	mu        sync.Mutex
	challenge platform.DeviceChallenge
	status    string // "pending" | "done" | "expired" | "error"
	startedAt time.Time
	// cancel stops this state's poll goroutine. Calling get() again for
	// the same account supersedes the prior attempt — without this the
	// old poll keeps hammering a stale device_code (which Twitch answers
	// with authorization_pending until expiry) and spams the auth log.
	cancel context.CancelFunc
}

type loginPageData struct {
	AccountID       string
	DisplayName     string
	VerificationURL string
	UserCode        string
}

func newLoginTwitchDeps(d Deps, rootCtx context.Context) *loginTwitchDeps {
	return &loginTwitchDeps{
		q:        d.Q,
		t:        d.Templates,
		sm:       d.Session,
		sessions: d.Sessions,
		registry: d.Registry,
		reload:   d.Reload,
		rootCtx:  rootCtx,
	}
}

// startDevice supersedes any in-flight poll, starts a device challenge,
// stores state, and spawns the poll goroutine. Returns the page data or an
// (errCode, httpStatus). errCode "" means success.
func (d *loginTwitchDeps) startDevice(r *http.Request, id string) (loginPageData, string, int) {
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		return loginPageData{}, "not_found", http.StatusNotFound
	}
	backend, ok := d.registry.Get(acc.Platform)
	if !ok {
		return loginPageData{}, "no_backend", http.StatusBadRequest
	}
	if v, ok := d.pending.Load(id); ok {
		if prev, ok := v.(*twitchLoginState); ok && prev.cancel != nil {
			prev.cancel()
		}
	}
	ch, err := backend.StartDeviceLogin(r.Context())
	if err != nil {
		return loginPageData{}, "device_start", http.StatusBadGateway
	}
	pollCtx, cancel := context.WithCancel(d.rootCtx)
	st := &twitchLoginState{challenge: ch, status: "pending", startedAt: time.Now(), cancel: cancel}
	d.pending.Store(id, st)
	go d.poll(pollCtx, id, backend, st)
	return loginPageData{AccountID: id, DisplayName: acc.DisplayName, VerificationURL: ch.VerificationURL, UserCode: ch.UserCode}, "", http.StatusOK
}

// statusFor returns the current poll status, "error" when no state exists.
func (d *loginTwitchDeps) statusFor(id string) string {
	v, ok := d.pending.Load(id)
	if !ok {
		return "error"
	}
	st := v.(*twitchLoginState)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.status
}

func (d *loginTwitchDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	page, code, status := d.startDevice(r, id)
	if code != "" {
		switch code {
		case "not_found":
			http.NotFound(w, r)
		case "no_backend":
			http.Error(w, i18n.T(i18n.DetectLang(r), "error.no_backend"), status)
		default:
			http.Error(w, code, status)
		}
		return
	}
	render(w, r, d.t, "login_twitch.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: page,
	})
}

func (d *loginTwitchDeps) apiDeviceStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	page, code, status := d.startDevice(r, id)
	if code != "" {
		writeAPIError(w, status, code, code)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"user_code": page.UserCode, "verification_url": page.VerificationURL})
}

func (d *loginTwitchDeps) apiDevicePoll(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": d.statusFor(chi.URLParam(r, "id"))})
}

func (d *loginTwitchDeps) poll(ctx context.Context, accountID string, backend platform.Backend, st *twitchLoginState) {
	interval := st.challenge.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := st.challenge.ExpiresAt
	slog.Info("twitch device-code poll started", "account", accountID, "interval", interval, "deadline", deadline)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			slog.Info("twitch device-code poll cancelled", "account", accountID)
			return
		case <-time.After(interval):
		}
		sess, err := backend.PollDeviceLogin(ctx, st.challenge)
		if err != nil {
			if strings.Contains(err.Error(), "authorization_pending") {
				// Expected between user-approval polls — don't log per
				// tick (it floods the auth event stream).
				continue
			}
			slog.Error("twitch device-code poll failed", "kind", "auth", "account", accountID, "platform", "twitch", "err", err)
			st.mu.Lock()
			st.status = "error"
			st.mu.Unlock()
			return
		}
		slog.Info("twitch device-code authorized", "kind", "auth", "account", accountID, "platform", "twitch", "expires_at", sess.ExpiresAt)
		if err := d.sessions.Put(d.rootCtx, accountID, sess); err != nil {
			slog.Error("persist twitch session failed", "account", accountID, "err", err)
			st.mu.Lock()
			st.status = "error"
			st.mu.Unlock()
			return
		}
		slog.Info("twitch session persisted", "account", accountID)
		// Backfill the avatar now so it shows immediately rather than
		// waiting for the next auth-health sweep. Best-effort.
		sess.AccountID = accountID
		fetchAndStoreAvatar(d.rootCtx, d.q, backend, accountID, sess)
		// Tell the browser-routed backend (if active) to discard its
		// cached "authed" flag + PubSub client for this account so the
		// next sidecar call re-installs the fresh Android-issued
		// cookies. Without this the sidecar tab keeps serving GQL with
		// the stale web-issued auth-token and integrity stays failed.
		type authInvalidator interface{ InvalidateAuth(string) }
		if inv, ok := backend.(authInvalidator); ok {
			inv.InvalidateAuth(accountID)
			slog.Info("twitch backend auth cache invalidated", "account", accountID)
		}
		if d.reload != nil {
			if err := d.reload(d.rootCtx); err != nil {
				slog.Error("scheduler reload after twitch login failed", "account", accountID, "err", err)
			} else {
				slog.Info("scheduler reloaded after twitch login", "account", accountID)
			}
		}
		st.mu.Lock()
		st.status = "done"
		st.mu.Unlock()
		return
	}
	slog.Warn("twitch device-code expired before user authorized", "kind", "auth", "account", accountID, "platform", "twitch")
	st.mu.Lock()
	st.status = "expired"
	st.mu.Unlock()
}

func (d *loginTwitchDeps) status(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, r, d.t, "login_twitch_status", d.statusFor(chi.URLParam(r, "id")))
}
