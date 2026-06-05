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

	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
	"github.com/aalejandrofer/rust-drops-miner/internal/store"
	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
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

func (d *loginTwitchDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	backend, ok := d.registry.Get(acc.Platform)
	if !ok {
		http.Error(w, "no backend for platform", http.StatusBadRequest)
		return
	}

	ch, err := backend.StartDeviceLogin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	st := &twitchLoginState{challenge: ch, status: "pending", startedAt: time.Now()}
	d.pending.Store(id, st)
	go d.poll(id, backend, st)

	render(w, d.t, "login_twitch.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginPageData{
			AccountID:       id,
			DisplayName:     acc.DisplayName,
			VerificationURL: ch.VerificationURL,
			UserCode:        ch.UserCode,
		},
	})
}

func (d *loginTwitchDeps) poll(accountID string, backend platform.Backend, st *twitchLoginState) {
	interval := st.challenge.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := st.challenge.ExpiresAt
	slog.Info("twitch device-code poll started", "account", accountID, "interval", interval, "deadline", deadline)
	for time.Now().Before(deadline) {
		select {
		case <-d.rootCtx.Done():
			slog.Info("twitch device-code poll cancelled", "account", accountID)
			return
		case <-time.After(interval):
		}
		sess, err := backend.PollDeviceLogin(d.rootCtx, st.challenge)
		if err != nil {
			if strings.Contains(err.Error(), "authorization_pending") {
				slog.Debug("twitch device-code still pending", "account", accountID)
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
	id := chi.URLParam(r, "id")
	v, ok := d.pending.Load(id)
	if !ok {
		renderPartial(w, d.t, "login_twitch_status", "error")
		return
	}
	st := v.(*twitchLoginState)
	st.mu.Lock()
	status := st.status
	st.mu.Unlock()
	renderPartial(w, d.t, "login_twitch_status", status)
}
