package api

import (
	"context"
	"net/http"
	"strings"
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

// allowedGamesUnion returns the union of every enabled account's game
// whitelist, keyed by lowercased name AND slug. Used to filter claim
// history so the /drops page only shows drops for games the operator
// actually opted into. Returns (nil, true) when at least one row was
// found — empty whitelist means "show nothing". Returns (nil, false)
// when there are no account_games rows at all — caller treats this as
// "no whitelist configured" and shows everything (legacy behaviour).
func allowedGamesUnion(ctx context.Context, q *gen.Queries) (map[string]struct{}, bool) {
	accs, err := q.ListAllAccounts(ctx)
	if err != nil {
		return nil, false
	}
	out := map[string]struct{}{}
	anyRow := false
	for _, a := range accs {
		rows, err := q.ListAccountGames(ctx, a.ID)
		if err != nil {
			continue
		}
		for _, r := range rows {
			anyRow = true
			out[strings.ToLower(r.Name)] = struct{}{}
			out[strings.ToLower(r.Slug)] = struct{}{}
		}
	}
	if !anyRow {
		return nil, false
	}
	return out, true
}

func (d *dropsDeps) list(w http.ResponseWriter, r *http.Request) {
	rows, err := d.q.ListRecentClaims(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allow, hasWhitelist := allowedGamesUnion(r.Context(), d.q)
	out := make([]dropsRow, 0, len(rows))
	for _, row := range rows {
		if hasWhitelist {
			if _, ok := allow[strings.ToLower(row.Game)]; !ok {
				continue
			}
		}
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
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "drops",
		Page: out,
	})
}
