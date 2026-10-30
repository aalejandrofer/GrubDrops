package api

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// twitchAndroidLoginer turns a pasted Twitch redirect URL or bare token into
// an Android session plus the user's login. Satisfied by *twitch.Backend and
// *twitch.BrowserBackend.
type twitchAndroidLoginer interface {
	AndroidLogin(ctx context.Context, raw string) (platform.Session, string, error)
}

func (s *server) twitchAndroidURL(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"login_url": twitch.AndroidLoginURL()})
}

// twitchAndroidLogin stores the session from a pasted Android redirect URL,
// or from jar: a base64 TwitchDropsMiner cookies.jar. With account_id it signs that account in again. Without it, the account
// whose session already belongs to the same Twitch user is updated, else a
// new account named after the login is created.
func (s *server) twitchAndroidLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL       string `json:"url"`
		Jar       string `json:"jar"`
		AccountID string `json:"account_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	raw := body.URL
	if body.Jar != "" {
		b, err := base64.StdEncoding.DecodeString(body.Jar)
		if err == nil {
			raw, err = twitch.TokenFromTDMJar(b)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "tdm_no_token")
			return
		}
	}
	ctx := r.Context()
	backend, ok := s.d.Registry.Get("twitch")
	loginer, ok2 := backend.(twitchAndroidLoginer)
	if !ok || !ok2 {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	id := strings.TrimSpace(body.AccountID)
	if id != "" {
		acc, ok := s.account(w, r, id)
		if !ok {
			return
		}
		if acc.Platform != "twitch" {
			writeErr(w, http.StatusBadRequest, "bad_request")
			return
		}
	}

	sess, login, err := loginer.AndroidLogin(ctx, raw)
	switch {
	case errors.Is(err, twitch.ErrNoAccessToken):
		writeErr(w, http.StatusBadRequest, "android_no_token")
		return
	case errors.Is(err, twitch.ErrWrongClient):
		writeErr(w, http.StatusBadRequest, "android_wrong_client")
		return
	case errors.Is(err, twitch.ErrTokenInvalid):
		writeErr(w, http.StatusBadRequest, "android_invalid_token")
		return
	case err != nil:
		// Errors from the twitch package never carry the token.
		slog.Warn("twitch android login failed", "kind", "auth", "err", err)
		writeErr(w, http.StatusBadGateway, "upstream")
		return
	}

	created := false
	if id == "" {
		s.accountCreateMu.Lock()
		id = s.twitchAccountForUser(ctx, sess.UserID)
		if id == "" {
			available, err := s.twitchAccountCapacity(ctx)
			if err != nil {
				s.accountCreateMu.Unlock()
				writeErr(w, http.StatusInternalServerError, "internal")
				return
			}
			if !available {
				s.accountCreateMu.Unlock()
				writeErr(w, http.StatusBadRequest, "account_limit")
				return
			}
			name := login
			if name == "" {
				name = "Twitch account"
			}
			id = genAccountID()
			now := time.Now().Unix()
			if _, err := s.d.Q.CreateAccount(ctx, gen.CreateAccountParams{
				ID: id, Platform: "twitch", DisplayName: name,
				Status: "idle", FingerprintJson: "{}", Enabled: 1,
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				s.accountCreateMu.Unlock()
				writeErr(w, http.StatusInternalServerError, "internal")
				return
			}
			created = true
		}
		s.accountCreateMu.Unlock()
	}
	// A device flow still polling for this account would overwrite the session.
	s.cancelTwitchFlow(id)
	if err := s.d.Sessions.Put(ctx, id, sess); err != nil {
		slog.Error("persist twitch session failed", "kind", "error", "account", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.forgetPointBalances(id)
	slog.Info("twitch android login stored", "kind", "auth", "account", id, "platform", "twitch", "created", created)
	sess.AccountID = id
	fetchAndStoreAvatar(ctx, s.d.Q, backend, id, sess)
	if inv, ok := backend.(interface{ InvalidateAuth(string) }); ok {
		inv.InvalidateAuth(id)
	}
	s.accountChanged(id)
	s.rerunSkippedCanary(ctx, "twitch")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "login": login, "created": created})
}

// twitchAccountForUser returns the Twitch account whose stored session
// belongs to userID, or "" when none does.
func (s *server) twitchAccountForUser(ctx context.Context, userID string) string {
	if userID == "" {
		return ""
	}
	accs, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		slog.Warn("twitch android login: list accounts failed", "kind", "error", "err", err)
		return ""
	}
	for _, a := range accs {
		if a.Platform != "twitch" {
			continue
		}
		sess, ok, err := s.d.Sessions.Get(ctx, a.ID)
		if err == nil && ok && sess.UserID == userID {
			return a.ID
		}
	}
	return ""
}
