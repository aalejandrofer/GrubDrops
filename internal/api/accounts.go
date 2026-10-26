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
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func (s *server) routesAccounts(r chi.Router) {
	r.Get("/accounts", s.listAccounts)
	r.Post("/accounts", s.createAccount)
	r.Put("/accounts/order", s.putAccountsOrder)
	r.Patch("/accounts/{id}", s.renameAccount)
	r.Delete("/accounts/{id}", s.deleteAccount)
	r.Put("/accounts/{id}/enabled", s.setAccountEnabled)
	r.Post("/accounts/{id}/twitch/device", s.twitchDeviceStart)
	r.Get("/accounts/{id}/twitch/device", s.twitchDeviceStatus)
	r.Post("/accounts/{id}/kick/session", s.kickSession)
	r.Post("/accounts/{id}/recheck-links", s.postAccountRecheckLinks)
	r.Get("/login/twitch/android", s.twitchAndroidURL)
	r.Post("/login/twitch/android", s.twitchAndroidLogin)
}

type accountAuthJSON struct {
	OK        bool    `json:"ok"`
	Detail    string  `json:"detail"`
	CheckedAt *string `json:"checked_at"` // RFC 3339, null when unknown
}

type accountJSON struct {
	ID           string           `json:"id"`
	Platform     string           `json:"platform"`
	Name         string           `json:"name"`          // custom name, else the platform name
	PlatformName string           `json:"platform_name"` // the platform login
	AvatarURL    string           `json:"avatar_url"`
	State        string           `json:"state"`
	NeedsLogin   bool             `json:"needs_login"`
	Enabled      bool             `json:"enabled"` // false: paused, the runner idles
	Auth         *accountAuthJSON `json:"auth"`    // null until a check has run
}

// checkedAt is nil when no check has run.
func checkedAt(sec int64) *string {
	if sec == 0 {
		return nil
	}
	return rfc3339(time.Unix(sec, 0))
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
			ID: a.ID, Platform: a.Platform, Name: store.AccountName(a), PlatformName: a.DisplayName,
			AvatarURL:  avatarSrc(a.Platform, a.AvatarUrl),
			State:      state,
			NeedsLogin: state == string(runner.StateNeedsLogin) || !hasSession,
			Enabled:    a.Enabled != 0,
		}
		if res, ok := authcheck.Load(ctx, s.d.Q, a.ID); ok {
			item.Auth = &accountAuthJSON{OK: res.OK, Detail: res.Msg, CheckedAt: checkedAt(res.CheckedAt)}
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
	// Empty clears the custom name: the account shows its platform name again.
	custom := strings.TrimSpace(body.Name)
	if utf8.RuneCountInString(custom) > store.AccountNameMax {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	acc, ok := s.account(w, r, id)
	if !ok {
		return
	}
	if err := s.d.Q.UpdateAccountCustomName(r.Context(), gen.UpdateAccountCustomNameParams{
		CustomName: custom, UpdatedAt: time.Now().Unix(), ID: id,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	acc.CustomName = custom
	if s.d.AccountRenamed != nil {
		s.d.AccountRenamed(id, store.AccountName(acc))
	}
	// Not accountChanged: a rename must not reload the runner, only tell clients.
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventAccounts, Data: struct{}{}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// putAccountsOrder sets the display order of every account. ids must list each
// account exactly once; anything unknown, repeated or missing is a 400. Runners
// are not reloaded: mining stays parallel, the order only shapes lists.
func (s *server) putAccountsOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	accts, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if body.IDs == nil || len(body.IDs) != len(accts) {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	known := make(map[string]bool, len(accts))
	for _, a := range accts {
		known[a.ID] = true
	}
	for _, id := range body.IDs {
		if !known[id] {
			writeErr(w, http.StatusBadRequest, "bad_request")
			return
		}
		delete(known, id) // a repeat finds it gone
	}
	tx, err := s.d.DB.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit succeeds
	qtx := s.d.Q.WithTx(tx)
	for i, id := range body.IDs {
		if err := qtx.SetAccountSortOrder(ctx, gen.SetAccountSortOrderParams{SortOrder: int64(i + 1), ID: id}); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventAccounts, Data: struct{}{}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// setAccountEnabled pauses or resumes mining for one account. The runner is
// rebuilt from the row, so a paused account idles and a resumed one mines.
func (s *server) setAccountEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	acc, ok := s.account(w, r, id)
	if !ok {
		return
	}
	var v int64
	if *body.Enabled {
		v = 1
	}
	if err := s.d.Q.SetAccountEnabled(r.Context(), gen.SetAccountEnabledParams{
		Enabled: v, UpdatedAt: time.Now().Unix(), ID: id,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.accountChanged(id)
	if acc.Enabled == 0 && v == 1 {
		s.rerunSkippedCanary(r.Context(), acc.Platform)
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
