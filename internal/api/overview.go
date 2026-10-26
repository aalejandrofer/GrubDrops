package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/runner"
)

func (s *server) routesOverview(r chi.Router) {
	r.Get("/overview", s.getOverview)
	r.Get("/runners", s.listRunners)
	r.Put("/runners/{id}/order", s.putRunnerOrder)
	r.Delete("/runners/{id}/order", s.deleteRunnerOrder)
}

func (s *server) overviewNow() time.Time {
	if s.d.Now != nil {
		return s.d.Now()
	}
	return time.Now()
}

func (s *server) overviewZone() *time.Location {
	if s.d.Zone != nil {
		if z := s.d.Zone(); z != nil {
			return z
		}
	}
	return time.Local
}

type overviewProblem struct {
	Kind        string `json:"kind"`
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
}

func (s *server) getOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accts, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	names := make(map[string]string, len(accts))
	paused := map[string]bool{}
	for _, a := range accts {
		names[a.ID] = a.DisplayName
		if a.Enabled == 0 {
			paused[a.ID] = true
		}
	}

	snaps := s.d.Pool.Snapshots()
	// ready counts healthy accounts that are not paused.
	mining, healthy, ready, anyWork := 0, 0, 0, false
	var lastScan time.Time
	problems := []overviewProblem{}
	for _, sn := range snaps {
		if _, ok := names[sn.AccountID]; !ok {
			continue
		}
		switch sn.State {
		case runner.StateWatching, runner.StateClaiming:
			mining++
		}
		if sn.State == runner.StateNeedsLogin {
			problems = append(problems, overviewProblem{Kind: "needs_login", AccountID: sn.AccountID, AccountName: names[sn.AccountID]})
		} else {
			healthy++
			if !paused[sn.AccountID] {
				ready++
			}
		}
		if sn.Current != nil || len(sn.UpNext) > 0 {
			anyWork = true
		}
		if sn.LastScanAt.After(lastScan) {
			lastScan = sn.LastScanAt
		}
	}
	scans, err := s.d.Scans.List(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	for _, sc := range scans {
		if sc.ScannedAt.After(lastScan) {
			lastScan = sc.ScannedAt
		}
	}

	state := "idle"
	switch {
	case len(accts) == 0:
		state = "no_accounts"
	case s.noChosenGames(r):
		state = "no_games"
	case mining > 0:
		state = "mining"
	case !anyWork && ready > 0:
		state = "nothing_live"
	}

	now := s.overviewNow()
	zone := s.overviewZone()
	local := now.In(zone)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	total, err := s.d.Minutes.Total(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	today, err := s.d.Minutes.Day(ctx, local.Format("2006-01-02"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	claimedToday, err := s.d.Claims.CountSince(ctx, midnight)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	claimedAll, err := s.d.Claims.CountSince(ctx, time.Unix(0, 0))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}

	latest, available := s.d.Version, false
	if s.d.UpdateStatus != nil {
		if avail, l := s.d.UpdateStatus(s.d.Version); avail {
			available, latest = true, l
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": map[string]any{
			"state": state, "accounts_total": len(accts), "accounts_mining": mining,
			"accounts_paused": len(paused),
			"last_scan_at":    rfc3339(lastScan),
		},
		"problems": problems,
		"kpis": map[string]any{
			"minutes_total": total, "minutes_today": today,
			"claimed_today": claimedToday, "claimed_all_time": claimedAll,
			"accounts_healthy": healthy,
		},
		"version": map[string]any{"current": s.d.Version, "latest": latest, "update_available": available},
	})
}

// noChosenGames reports whether the chosen-games list is empty. A list that
// cannot be read counts as non-empty so the overview still renders.
func (s *server) noChosenGames(r *http.Request) bool {
	if s.d.Chosen == nil {
		return false
	}
	g, err := s.d.Chosen.List(r.Context())
	return err == nil && len(g) == 0
}
