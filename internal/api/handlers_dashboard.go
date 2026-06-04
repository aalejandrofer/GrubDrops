package api

import (
	"net/http"

	"github.com/chano-fernandez/rust-drops-miner/internal/scheduler"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

type dashboardDeps struct {
	q   *gen.Queries
	t   Renderer
	sch *scheduler.Scheduler
}

type dashCard struct {
	ID, Platform, DisplayName, State string
	Enabled                          bool
}

func (d dashboardDeps) collect(r *http.Request) []dashCard {
	accs, err := d.q.ListEnabledAccounts(r.Context())
	if err != nil {
		return nil
	}
	stateByID := map[string]string{}
	for _, s := range d.sch.Snapshot() {
		stateByID[s.AccountID] = s.State
	}
	cards := make([]dashCard, 0, len(accs))
	for _, a := range accs {
		st, ok := stateByID[a.ID]
		if !ok {
			st = "stopped"
		}
		cards = append(cards, dashCard{
			ID: a.ID, Platform: a.Platform, DisplayName: a.DisplayName,
			State: st, Enabled: a.Enabled == 1,
		})
	}
	return cards
}

func (d dashboardDeps) page(w http.ResponseWriter, r *http.Request) {
	render(w, d.t, "dashboard.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: d.collect(r),
	})
}

func (d dashboardDeps) cards(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, d.t, "dashboard_cards", d.collect(r))
}
