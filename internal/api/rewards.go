package api

import (
	"cmp"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

// rewardCodeJSON is one reward code in GET /api/reward-codes. Code is a
// secret: it leaves the server only on this admin-only route.
type rewardCodeJSON struct {
	ID          int64   `json:"id"`
	AccountID   string  `json:"account_id"`
	AccountName string  `json:"account_name"`
	Name        string  `json:"name"`
	Game        string  `json:"game"`
	Code        string  `json:"code"`
	RedeemURL   string  `json:"redeem_url"`
	ExpiresAt   *string `json:"expires_at"`
	ReceivedAt  *string `json:"received_at"`
	FirstSeen   *string `json:"first_seen"`
}

func (s *server) routesRewards(r chi.Router) {
	r.Get("/reward-codes", s.getRewardCodes)
	r.Put("/reward-codes/{id}", s.putRewardCode)
}

// getRewardCodes lists the codes still to redeem: with a code, neither
// redeemed nor dismissed, and not expired. Newest first.
func (s *server) getRewardCodes(w http.ResponseWriter, r *http.Request) {
	if s.d.RewardCodes == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ctx := r.Context()
	rows, err := s.d.RewardCodes.List(ctx, true)
	if err != nil {
		slog.Warn("api: list reward codes failed", "kind", "error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	names := map[string]string{}
	if accts, err := s.d.Q.ListAllAccounts(ctx); err == nil {
		for _, a := range accts {
			names[a.ID] = store.AccountName(a)
		}
	}
	now := s.overviewNow()
	out := []rewardCodeJSON{}
	for _, c := range rows {
		if c.Code == "" || (!c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)) {
			continue
		}
		out = append(out, rewardCodeJSON{
			ID: c.ID, AccountID: c.AccountID, AccountName: cmp.Or(names[c.AccountID], c.AccountID),
			Name: c.Name, Game: c.Game, Code: c.Code, RedeemURL: c.RedeemURL,
			ExpiresAt: rfc3339(c.ExpiresAt), ReceivedAt: rfc3339(c.ReceivedAt), FirstSeen: rfc3339(c.FirstSeen),
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"codes": out})
}

// putRewardCode marks a code redeemed or dismissed: body
// {"state":"redeemed"} or {"state":"dismissed"}. Either hides the code and
// stops its reminder.
func (s *server) putRewardCode(w http.ResponseWriter, r *http.Request) {
	if s.d.RewardCodes == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	now := s.overviewNow()
	var ok bool
	switch body.State {
	case "redeemed":
		ok, err = s.d.RewardCodes.SetRedeemed(ctx, id, now)
	case "dismissed":
		ok, err = s.d.RewardCodes.SetDismissed(ctx, id, now)
	default:
		writeErr(w, http.StatusBadRequest, "bad_state")
		return
	}
	if err != nil {
		slog.Warn("api: save reward code failed", "kind", "error", "id", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
