package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// twitchFlow is one in-flight device login. Status is "pending", "done",
// "expired" or "error".
type twitchFlow struct {
	mu        sync.Mutex
	status    string
	challenge platform.DeviceChallenge
	ctx       context.Context
	cancel    context.CancelFunc
}

func (f *twitchFlow) set(status string) {
	f.mu.Lock()
	f.status = status
	f.mu.Unlock()
}

func (f *twitchFlow) get() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// cancelTwitchFlow stops and forgets the account's device flow, if any.
func (s *server) cancelTwitchFlow(id string) {
	if v, ok := s.twitch.LoadAndDelete(id); ok {
		v.(*twitchFlow).cancel()
	}
}

func (s *server) twitchDeviceStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, ok := s.account(w, r, id)
	if !ok {
		return
	}
	backend, ok := s.d.Registry.Get(acc.Platform)
	if !ok || acc.Platform != "twitch" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	ch, err := backend.StartDeviceLogin(r.Context())
	if err != nil {
		slog.Warn("twitch device start failed", "kind", "auth", "account", id, "err", err)
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}
	// A new start supersedes the old flow so a stale device_code stops polling.
	ctx, cancel := context.WithCancel(s.rootCtx())
	fl := &twitchFlow{status: "pending", challenge: ch, ctx: ctx, cancel: cancel}
	if old, loaded := s.twitch.Swap(id, fl); loaded {
		old.(*twitchFlow).cancel()
	}
	go s.pollTwitch(ctx, id, backend, fl)
	writeJSON(w, http.StatusOK, map[string]string{
		"verification_url": ch.VerificationURL,
		"user_code":        ch.UserCode,
		"expires_at":       ch.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *server) twitchDeviceStatus(w http.ResponseWriter, r *http.Request) {
	v, ok := s.twitch.Load(chi.URLParam(r, "id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": v.(*twitchFlow).get()})
}

func (s *server) pollTwitch(ctx context.Context, id string, backend platform.Backend, fl *twitchFlow) {
	defer fl.cancel()
	interval := fl.challenge.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := fl.challenge.ExpiresAt
	slog.Info("twitch device-code poll started", "kind", "auth", "account", id, "interval", interval, "deadline", deadline)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			slog.Info("twitch device-code poll cancelled", "kind", "auth", "account", id)
			return
		case <-time.After(interval):
		}
		sess, err := backend.PollDeviceLogin(ctx, fl.challenge)
		if err != nil {
			if strings.Contains(err.Error(), "authorization_pending") {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			slog.Error("twitch device-code poll failed", "kind", "auth", "account", id, "platform", "twitch", "err", err)
			fl.set("error")
			return
		}
		if ctx.Err() != nil { // superseded or account deleted: store nothing
			return
		}
		if err := s.d.Sessions.Put(ctx, id, sess); err != nil {
			slog.Error("persist twitch session failed", "kind", "error", "account", id, "err", err)
			fl.set("error")
			return
		}
		if ctx.Err() != nil {
			return
		}
		slog.Info("twitch device-code authorized", "kind", "auth", "account", id, "platform", "twitch")
		sess.AccountID = id
		fetchAndStoreAvatar(ctx, s.d.Q, backend, id, sess)
		// Drop the browser-routed backend's cached auth so the next call
		// installs the fresh Android-issued cookies.
		if inv, ok := backend.(interface{ InvalidateAuth(string) }); ok {
			inv.InvalidateAuth(id)
		}
		s.accountChanged(id)
		s.rerunSkippedCanary(ctx, "twitch")
		fl.set("done")
		return
	}
	slog.Warn("twitch device-code expired before user authorized", "kind", "auth", "account", id, "platform", "twitch")
	fl.set("expired")
}
