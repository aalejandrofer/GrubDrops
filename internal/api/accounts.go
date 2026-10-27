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
	r.Post("/accounts/{id}/check-login", s.postAccountCheckLogin)
	r.Post("/accounts/{id}/restart", s.postAccountRestart)
	r.Get("/accounts/{id}/games", s.getAccountGames)
	r.Put("/accounts/{id}/games", s.putAccountGames)
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
	Enabled      bool             `json:"enabled"`        // false: paused, the runner idles
	OwnGames     bool             `json:"own_games"`      // true: mines its own game list
	OwnGameCount int              `json:"own_game_count"` // games on the own list, 0 while own_games is off
	Auth         *accountAuthJSON `json:"auth"`           // null until a check has run
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
			OwnGames:   a.OwnGames != 0,
		}
		if a.OwnGames != 0 && s.d.AccountGames != nil {
			if list, err := s.d.AccountGames.List(ctx, a.ID); err == nil {
				item.OwnGameCount = len(list)
			} else {
				slog.Warn("accounts: own game list lookup failed", "kind", "error", "account", a.ID, "err", err)
			}
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

type accountGameJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// accountGamesJSON is an account's own game list. Games is kept while Own is
// off, so turning it on again brings the list back.
type accountGamesJSON struct {
	Own   bool              `json:"own"`
	Games []accountGameJSON `json:"games"`
}

func (s *server) accountGamesView(ctx context.Context, a gen.Account) (accountGamesJSON, error) {
	list, err := s.d.AccountGames.List(ctx, a.ID)
	if err != nil {
		return accountGamesJSON{}, err
	}
	out := accountGamesJSON{Own: a.OwnGames != 0, Games: make([]accountGameJSON, 0, len(list))}
	for _, g := range list {
		out.Games = append(out.Games, accountGameJSON{ID: g.ID, Name: g.Name})
	}
	return out, nil
}

func (s *server) getAccountGames(w http.ResponseWriter, r *http.Request) {
	if s.d.AccountGames == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	a, ok := s.account(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	v, err := s.accountGamesView(r.Context(), a)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// maxAccountGames caps one account's list.
const maxAccountGames = 100

// putAccountGames sets whether the account mines its own list and, when
// games is sent, replaces that list in rank order. A game with an id must be
// in the games table; one with only a name is added to it, like the Games
// page does, but not to the global list.
func (s *server) putAccountGames(w http.ResponseWriter, r *http.Request) {
	if s.d.AccountGames == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	var in struct {
		Own   bool              `json:"own"`
		Games []accountGameJSON `json:"games"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	a, ok := s.account(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if len(in.Games) > maxAccountGames {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	var ids []string
	if in.Games != nil {
		all, err := s.d.Q.ListAllGames(ctx)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
		known := make(map[string]bool, len(all))
		for _, g := range all {
			known[g.ID] = true
		}
		ids = make([]string, 0, len(in.Games))
		var add []accountGameJSON
		for _, g := range in.Games {
			if g.ID != "" {
				if !known[g.ID] {
					writeErr(w, http.StatusBadRequest, "bad_request")
					return
				}
				ids = append(ids, g.ID)
				continue
			}
			id, ok := gameRowID(g.Name)
			if !ok {
				writeErr(w, http.StatusBadRequest, "bad_request")
				return
			}
			id, exists := matchGameRow(all, id, g.Name)
			if !exists && !known[id] {
				add = append(add, accountGameJSON{ID: id, Name: g.Name})
				known[id] = true
			}
			ids = append(ids, id)
		}
		for _, g := range add {
			if err := s.insertGame(ctx, g.ID, g.Name); err != nil {
				writeErr(w, http.StatusInternalServerError, "internal")
				return
			}
		}
	}
	if err := s.d.AccountGames.Set(ctx, a.ID, in.Own, ids); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	a.OwnGames = 0
	if in.Own {
		a.OwnGames = 1
	}
	v, err := s.accountGamesView(ctx, a)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.accountChanged(a.ID)
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventAccounts, Data: struct{}{}})
	}
	writeJSON(w, http.StatusOK, v)
}

// postAccountCheckLogin runs the account's auth probe now and returns the result.
func (s *server) postAccountCheckLogin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	if s.d.CheckLogin == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	res, err := s.d.CheckLogin(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventAccounts, Data: struct{}{}})
	}
	writeJSON(w, http.StatusOK, accountAuthJSON{OK: res.OK, Detail: res.Msg, CheckedAt: checkedAt(res.CheckedAt)})
}

// postAccountRestart rebuilds the account's runner from its stored row.
func (s *server) postAccountRestart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	s.accountChanged(id)
	w.WriteHeader(http.StatusNoContent)
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
