package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/plan"
	"github.com/aalejandrofer/grubdrops/internal/runner"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type targetDTO struct {
	Game            string  `json:"game"`
	Platform        string  `json:"platform"`
	CampaignID      string  `json:"campaign_id"`
	CampaignName    string  `json:"campaign_name"`
	BenefitID       string  `json:"benefit_id"`
	BenefitName     string  `json:"benefit_name"`
	ImageURL        string  `json:"image_url"`
	RequiredMinutes int     `json:"required_minutes"`
	Minutes         int     `json:"minutes"`
	EndsAt          *string `json:"ends_at"`
}

type blockedDTO struct {
	Target targetDTO `json:"target"`
	Reason string    `json:"reason"`
}

type runnerDTO struct {
	AccountID   string       `json:"account_id"`
	AccountName string       `json:"account_name"`
	Platform    string       `json:"platform"`
	AvatarURL   string       `json:"avatar_url"`
	State       string       `json:"state"`
	Current     *targetDTO   `json:"current"`
	Channel     string       `json:"channel"`
	ViewerCount int          `json:"viewer_count"`
	Minutes     int          `json:"minutes"`
	StartedAt   *string      `json:"started_at"`
	LastScanAt  *string      `json:"last_scan_at"`
	UpNext      []targetDTO  `json:"up_next"`
	Blocked     []blockedDTO `json:"blocked"`
	OrderCustom bool         `json:"order_custom"`
}

// rfc3339 formats t in UTC, or nil for the zero time.
func rfc3339(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// TargetJSON converts a plan target to its API shape.
func TargetJSON(t plan.Target) targetDTO {
	return targetDTO{
		Game: t.Campaign.Game, Platform: t.Campaign.Platform,
		CampaignID: t.Campaign.ID, CampaignName: t.Campaign.Name,
		BenefitID: t.Benefit.ID, BenefitName: t.Benefit.Name, ImageURL: t.Benefit.ImageURL,
		RequiredMinutes: t.Benefit.RequiredMinutes, Minutes: t.Minutes,
		EndsAt: rfc3339(t.Campaign.EndsAt),
	}
}

// RunnerJSON converts a runner snapshot to its API shape. custom says a
// session order is set; the pool has already applied it to s.UpNext.
func RunnerJSON(s runner.Snapshot, a gen.Account, custom bool) runnerDTO {
	up := s.UpNext
	d := runnerDTO{
		AccountID: s.AccountID, AccountName: store.AccountName(a), Platform: s.Platform,
		AvatarURL: avatarSrc(a.Platform, a.AvatarUrl), State: string(s.State), Channel: s.Channel,
		ViewerCount: s.ViewerCount, Minutes: s.MinutesWatched,
		StartedAt: rfc3339(s.StartedAt), LastScanAt: rfc3339(s.LastScanAt),
		UpNext: make([]targetDTO, 0, len(up)), Blocked: make([]blockedDTO, 0, len(s.Blocked)),
		OrderCustom: custom,
	}
	if s.Current != nil {
		c := TargetJSON(*s.Current)
		d.Current = &c
	}
	for _, t := range up {
		d.UpNext = append(d.UpNext, TargetJSON(t))
	}
	for _, b := range s.Blocked {
		d.Blocked = append(d.Blocked, blockedDTO{Target: TargetJSON(b.Target), Reason: string(b.Reason)})
	}
	return d
}

// sessionOrder returns the account's session order, nil when none is set.
func (s *server) sessionOrder(accountID string) []string {
	f := s.d.Pool.OrderFunc(accountID)
	if f == nil {
		return nil
	}
	return f()
}

func (s *server) listRunners(w http.ResponseWriter, r *http.Request) {
	accts, err := s.d.Q.ListAllAccounts(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	snaps := make(map[string]runner.Snapshot)
	for _, sn := range s.d.Pool.Snapshots() {
		snaps[sn.AccountID] = sn
	}
	out := make([]runnerDTO, 0, len(snaps))
	for _, a := range accts {
		sn, ok := snaps[a.ID]
		if !ok {
			continue
		}
		order := s.sessionOrder(a.ID)
		out = append(out, RunnerJSON(sn, a, len(order) > 0))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) putRunnerOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		BenefitIDs []string `json:"benefit_ids"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	if body.BenefitIDs == nil {
		body.BenefitIDs = []string{}
	}
	s.d.Pool.SetOrder(id, body.BenefitIDs)
	w.WriteHeader(http.StatusNoContent)
}

// postRunnerMineNow switches the account to a queued Up next target now.
func (s *server) postRunnerMineNow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		BenefitID string `json:"benefit_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.BenefitID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	switch err := s.d.Pool.MineNow(id, body.BenefitID); {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, runner.ErrNotQueued):
		writeErr(w, http.StatusNotFound, "not_queued")
	case errors.Is(err, runner.ErrNotRunning):
		writeErr(w, http.StatusConflict, "not_running")
	default:
		writeErr(w, http.StatusInternalServerError, "internal")
	}
}

func (s *server) deleteRunnerOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := s.account(w, r, id); !ok {
		return
	}
	s.d.Pool.ResetOrder(id)
	w.WriteHeader(http.StatusNoContent)
}
