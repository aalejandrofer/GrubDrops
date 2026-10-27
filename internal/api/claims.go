package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

const (
	claimsDefaultLimit = 10
	claimsMaxLimit     = 100
	claimsMaxQuery     = 100
)

type claimItem struct {
	AccountID    string `json:"account_id"`
	AccountName  string `json:"account_name"`
	Platform     string `json:"platform"`
	Game         string `json:"game"`
	CampaignID   string `json:"campaign_id"`
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
		Game: e.Game, CampaignID: e.CampaignID, CampaignName: e.CampaignName, BenefitID: e.BenefitID,
		BenefitName: e.BenefitName, ImageURL: e.ImageURL, Code: e.Code, Source: e.Source,
		ClaimedAt: e.ClaimedAt.UTC().Format(time.RFC3339),
	}
}

func (s *server) routesClaims(r chi.Router) {
	r.Get("/claims", s.getClaims)
	r.Post("/claims", s.postClaim)
	r.Delete("/claims/{account}/{benefit}", s.deleteClaim)
}

// queuedTarget finds a drop in the account's current runner plan: the drop
// it mines, Up next or Blocked.
func queuedTarget(snaps []runner.Snapshot, accountID, benefitID string) (plan.Target, bool) {
	for _, sn := range snaps {
		if sn.AccountID != accountID {
			continue
		}
		if sn.Current != nil && sn.Current.Benefit.ID == benefitID {
			return *sn.Current, true
		}
		for _, t := range sn.UpNext {
			if t.Benefit.ID == benefitID {
				return t, true
			}
		}
		for _, b := range sn.Blocked {
			if b.Target.Benefit.ID == benefitID {
				return b.Target, true
			}
		}
	}
	return plan.Target{}, false
}

// pathParam is a route value with any percent-escapes decoded. chi matches on
// the raw path, so an escaped ID arrives still escaped.
func pathParam(r *http.Request, key string) string {
	v := chi.URLParam(r, key)
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

// postClaim marks a planned drop collected by hand, so the runner stops
// mining it. Only drops in the account's plan can be marked.
func (s *server) postClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string `json:"account_id"`
		BenefitID string `json:"benefit_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	a, ok := s.account(w, r, body.AccountID)
	if !ok {
		return
	}
	if s.d.Pool == nil {
		writeErr(w, http.StatusConflict, "not_running")
		return
	}
	t, ok := queuedTarget(s.d.Pool.Snapshots(), a.ID, body.BenefitID)
	if !ok {
		writeErr(w, http.StatusNotFound, "not_queued")
		return
	}
	e := store.ClaimEntry{
		AccountID: a.ID, Platform: a.Platform, Game: t.Campaign.Game,
		CampaignID: t.Campaign.ID, CampaignName: t.Campaign.Name,
		BenefitID: t.Benefit.ID, BenefitName: t.Benefit.Name, ImageURL: t.Benefit.ImageURL,
		Source: store.ClaimSourceManual, ClaimedAt: s.overviewNow(),
	}
	if err := s.d.Claims.Record(r.Context(), e); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	s.accountChanged(a.ID)
	item := ClaimJSON(e, store.AccountName(a))
	if s.d.Hub != nil {
		s.d.Hub.Publish(Event{Name: EventClaim, Data: item})
	}
	writeJSON(w, http.StatusCreated, item)
}

// deleteClaim marks a drop uncollected: it forgets the claim and the
// runner's skip, so the account may mine the drop again. A drop the platform
// shows as claimed comes back on the next scan. The reload rebuilds the
// runner, which reads its skips from the store again.
func (s *server) deleteClaim(w http.ResponseWriter, r *http.Request) {
	acct, benefit := pathParam(r, "account"), pathParam(r, "benefit")
	claimed, err := s.d.Claims.Delete(r.Context(), acct, benefit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	skipped, err := store.NewSkips(s.d.Q).Unskip(r.Context(), acct, benefit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	if !claimed && !skipped {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	s.accountChanged(acct)
	if s.d.Hub != nil {
		// Refreshes the overview counts and the runners in every open tab.
		s.d.Hub.Publish(Event{Name: EventAccounts, Data: struct{}{}})
	}
	w.WriteHeader(http.StatusNoContent)
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

// claimQuery trims the search text and caps it at claimsMaxQuery runes.
func claimQuery(v string) string {
	v = strings.TrimSpace(v)
	if r := []rune(v); len(r) > claimsMaxQuery {
		v = strings.TrimSpace(string(r[:claimsMaxQuery]))
	}
	return v
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
		Game: q.Get("game"), AccountID: q.Get("account"), Query: claimQuery(q.Get("q")), Limit: limit, Offset: offset,
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
		names[a.ID] = store.AccountName(a)
		accounts = append(accounts, claimAccount{ID: a.ID, Name: store.AccountName(a)})
	}
	items := make([]claimItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, ClaimJSON(e, names[e.AccountID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": items, "games": games, "accounts": accounts})
}
