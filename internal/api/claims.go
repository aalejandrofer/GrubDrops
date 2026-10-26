package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/store"
)

const (
	claimsDefaultLimit = 10
	claimsMaxLimit     = 100
)

type claimItem struct {
	AccountID    string `json:"account_id"`
	AccountName  string `json:"account_name"`
	Platform     string `json:"platform"`
	Game         string `json:"game"`
	CampaignName string `json:"campaign_name"`
	BenefitID    string `json:"benefit_id"`
	BenefitName  string `json:"benefit_name"`
	ImageURL     string `json:"image_url"`
	Code         string `json:"code"`
	Source       string `json:"source"`
	ClaimedAt    string `json:"claimed_at"`
}

type claimAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ClaimJSON converts a claim log entry to its API shape.
func ClaimJSON(e store.ClaimEntry, accountName string) claimItem {
	return claimItem{
		AccountID: e.AccountID, AccountName: accountName, Platform: e.Platform,
		Game: e.Game, CampaignName: e.CampaignName, BenefitID: e.BenefitID,
		BenefitName: e.BenefitName, ImageURL: e.ImageURL, Code: e.Code, Source: e.Source,
		ClaimedAt: e.ClaimedAt.UTC().Format(time.RFC3339),
	}
}

func (s *server) routesClaims(r chi.Router) {
	r.Get("/claims", s.getClaims)
}

// queryInt reads a non-negative integer query value. A missing value gives
// def; anything else that is not a non-negative integer reports false.
func queryInt(r *http.Request, key string, def int) (int, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (s *server) getClaims(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryInt(r, "limit", claimsDefaultLimit)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	offset, ok := queryInt(r, "offset", 0)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if limit > claimsMaxLimit {
		limit = claimsMaxLimit
	}
	ctx := r.Context()
	q := r.URL.Query()
	entries, total, err := s.d.Claims.Page(ctx, store.ClaimFilter{
		Game: q.Get("game"), AccountID: q.Get("account"), Limit: limit, Offset: offset,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	games, err := s.d.Claims.Games(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	accts, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	names := make(map[string]string, len(accts))
	accounts := make([]claimAccount, 0, len(accts))
	for _, a := range accts {
		names[a.ID] = a.DisplayName
		accounts = append(accounts, claimAccount{ID: a.ID, Name: a.DisplayName})
	}
	items := make([]claimItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, ClaimJSON(e, names[e.AccountID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": items, "games": games, "accounts": accounts})
}
