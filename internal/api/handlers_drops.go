package api

import (
	"net/http"
	"time"

	"github.com/aalejandrofer/rust-drops-miner/internal/store/gen"
)

type dropsDeps struct {
	q *gen.Queries
	t Renderer
}

type dropsRow struct {
	ClaimedAtHuman string
	Platform       string
	Game           string
	CampaignName   string
	BenefitName    string
	AccountName    string
}

func (d *dropsDeps) list(w http.ResponseWriter, r *http.Request) {
	rows, err := d.q.ListRecentClaims(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]dropsRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, dropsRow{
			ClaimedAtHuman: time.Unix(row.ClaimedAt, 0).UTC().Format("2006-01-02 15:04:05"),
			Platform:       row.Platform,
			Game:           row.Game,
			CampaignName:   row.CampaignName,
			BenefitName:    row.BenefitName,
			AccountName:    row.AccountName,
		})
	}
	render(w, d.t, "drops.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: out,
	})
}
