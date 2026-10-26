package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func (s *server) routesAccounts(r chi.Router) {
	r.Get("/accounts", s.listAccounts)
	r.Post("/accounts", s.createAccount)
	r.Patch("/accounts/{id}", s.renameAccount)
	r.Delete("/accounts/{id}", s.deleteAccount)
	r.Post("/accounts/{id}/twitch/device", s.twitchDeviceStart)
	r.Get("/accounts/{id}/twitch/device", s.twitchDeviceStatus)
	r.Post("/accounts/{id}/kick/session", s.kickSession)
}

type accountAuthJSON struct {
	OK        bool    `json:"ok"`
	Detail    string  `json:"detail"`
	CheckedAt *string `json:"checked_at"` // RFC 3339, null when unknown
}

type accountJSON struct {
	ID         string           `json:"id"`
	Platform   string           `json:"platform"`
	Name       string           `json:"name"`
	AvatarURL  string           `json:"avatar_url"`
	State      string           `json:"state"`
	NeedsLogin bool             `json:"needs_login"`
	Auth       *accountAuthJSON `json:"auth"` // null until a check has run
}

// unixRFC3339 formats unix seconds as RFC 3339 UTC, nil for 0.
func unixRFC3339(sec int64) *string {
	if sec == 0 {
		return nil
	}
	s := time.Unix(sec, 0).UTC().Format(time.RFC3339)
	return &s
}

func genAccountID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "acc_" + hex.EncodeToString(b[:])
}

func (s *server) listAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accs, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	states := map[string]string{}
	if s.d.Pool != nil {
		for _, sn := range s.d.Pool.Snapshots() {
			states[sn.AccountID] = string(sn.State)
		}
	}
	out := make([]accountJSON, 0, len(accs))
	for _, a := range accs {
		state := states[a.ID]
		if state == "" {
			state = string(runner.StateIdle)
		}
		_, hasSession, err := s.d.Sessions.Get(ctx, a.ID)
		if err != nil {
			slog.Warn("accounts: session lookup failed", "kind", "error", "account", a.ID, "err", err)
		}
		item := accountJSON{
			ID: a.ID, Platform: a.Platform, Name: a.DisplayName,
			AvatarURL:  avatarSrc(a.Platform, a.AvatarUrl),
			State:      state,
			NeedsLogin: state == string(runner.StateNeedsLogin) || !hasSession,
		}
		if res, ok := authcheck.Load(ctx, s.d.Q, a.ID); ok {
			item.Auth = &accountAuthJSON{OK: res.OK, Detail: res.Msg, CheckedAt: unixRFC3339(res.CheckedAt)}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Platform string `json:"platform"`
		Name     string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	switch body.Platform {
	case "twitch":
		if name == "" {
			name = "Twitch account"
		}
	case "kick":
		if name == "" {
			name = "Kick account"
		}
	default:
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	id := genAccountID()
	now := time.Now().Unix()
	if _, err := s.d.Q.CreateAccount(r.Context(), gen.CreateAccountParams{
		ID: id, Platform: body.Platform, DisplayName: name,
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.accountChanged(id)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *server) renameAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	if err := s.d.Q.UpdateAccountDisplayName(r.Context(), gen.UpdateAccountDisplayNameParams{
		DisplayName: name, UpdatedAt: time.Now().Unix(), ID: id,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	s.cancelTwitchFlow(id)
	if err := s.purgeAccount(r.Context(), id); err != nil {
		slog.Error("account purge failed", "kind", "error", "account", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.AccountRemoved != nil {
		s.d.AccountRemoved(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// account loads an account or writes 404.
func (s *server) account(w http.ResponseWriter, r *http.Request, id string) (gen.Account, bool) {
	acc, err := s.d.Q.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found")
		} else {
			writeErr(w, http.StatusInternalServerError, "internal")
		}
		return gen.Account{}, false
	}
	return acc, true
}

func (s *server) accountChanged(id string) {
	if s.d.AccountChanged != nil {
		s.d.AccountChanged(id)
	}
}

// rootCtx is the long-lived context background login work runs on.
func (s *server) rootCtx() context.Context {
	if s.d.RootCtx != nil {
		return s.d.RootCtx
	}
	return context.Background()
}

// purgeAccount hard-deletes an account and every row that belongs to it in
// one transaction. Children are deleted explicitly so the purge does not rely
// on the foreign_keys pragma. claim_log and watch_minutes carry no account FK
// and are kept: history outlives the account.
func (s *server) purgeAccount(ctx context.Context, id string) error {
	tx, err := s.d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit succeeds
	qtx := s.d.Q.WithTx(tx)

	if err := qtx.DeleteAccountSession(ctx, id); err != nil {
		return err
	}
	if err := qtx.ClearAccountGames(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccountCampaignLinks(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccountCampaignPriorities(ctx, sql.NullString{String: id, Valid: true}); err != nil {
		return err
	}
	if err := qtx.DeleteAccountProgress(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccountClaims(ctx, id); err != nil {
		return err
	}
	if err := qtx.DeleteAccount(ctx, id); err != nil {
		return err
	}
	return tx.Commit()
}
