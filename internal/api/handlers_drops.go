package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

type dropsDeps struct {
	q *gen.Queries
	t Renderer
}

// dropTab is one of "past" | "current" | "upcoming". The /drops page
// renders a tab strip and only one table at a time. HTMX swaps just the
// table body when the user clicks a tab.
type dropTab string

const (
	tabPast     dropTab = "past"
	tabCurrent  dropTab = "current"
	tabUpcoming dropTab = "upcoming"
)

// dropsRow is the unified row type for all three tabs. The "When" column
// has different meanings per tab:
//   - past:     ended at (or claimed_at for rows that came from a claim)
//   - current:  ends at
//   - upcoming: starts at
//
// BenefitName is only populated for past-claim rows; for campaign-only
// rows we show the campaign name in the Drop column and leave BenefitName
// empty.
type dropsRow struct {
	When         string
	Platform     string
	Game         string
	CampaignName string
	BenefitName  string
	AccountName  string
}

// dropsPage is what the template sees: the active tab, the three counts
// (so the tab strip can show totals), and the rows to render in the
// table body.
type dropsPage struct {
	Tab          dropTab
	PastCount    int
	CurrentCount int
	UpcomingCount int
	Rows         []dropsRow
}

// allowedGamesUnion returns the union of every enabled account's game
// whitelist, keyed by lowercased name AND slug. The /drops page uses
// this to ensure non-whitelisted campaigns NEVER appear regardless of
// status. Returns (nil, true) when at least one row was found — empty
// whitelist means "show nothing". Returns (nil, false) when there are
// no account_games rows at all — caller treats this as "no whitelist
// configured" and shows everything (legacy behaviour, used before the
// operator picks games).
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

// passesWhitelist returns true if game is on the whitelist union (or the
// whitelist is unconfigured, in which case everything passes).
func passesWhitelist(allow map[string]struct{}, hasWhitelist bool, game string) bool {
	if !hasWhitelist {
		return true
	}
	_, ok := allow[strings.ToLower(game)]
	return ok
}

func (d *dropsDeps) list(w http.ResponseWriter, r *http.Request) {
	tab := dropTab(r.URL.Query().Get("tab"))
	switch tab {
	case tabPast, tabCurrent, tabUpcoming:
	default:
		tab = tabCurrent
	}

	allow, hasWhitelist := allowedGamesUnion(r.Context(), d.q)
	now := time.Now().Unix()
	const limit = 200

	pastRows, currentRows, upcomingRows, err := d.collectAll(r.Context(), allow, hasWhitelist, now, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	page := dropsPage{
		Tab:           tab,
		PastCount:     len(pastRows),
		CurrentCount:  len(currentRows),
		UpcomingCount: len(upcomingRows),
	}
	switch tab {
	case tabPast:
		page.Rows = pastRows
	case tabCurrent:
		page.Rows = currentRows
	case tabUpcoming:
		page.Rows = upcomingRows
	}

	// HTMX partial — used when the user clicks a tab. We just swap the
	// table body so the page chrome stays put.
	if r.Header.Get("HX-Request") == "true" {
		renderPartial(w, d.t, "drops_table", page)
		return
	}
	render(w, d.t, "drops.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "drops",
		Page: page,
	})
}

// collectAll runs all three queries (past, current, upcoming) and returns
// rows filtered by the whitelist. PAST also unions in claim history so
// that drops the operator has already claimed appear even if the campaign
// row was evicted.
func (d *dropsDeps) collectAll(
	ctx context.Context,
	allow map[string]struct{}, hasWhitelist bool,
	now int64, limit int64,
) (past, current, upcoming []dropsRow, err error) {
	// Past: campaigns ended before now.
	pastCamps, err := d.q.ListPastCampaigns(ctx, gen.ListPastCampaignsParams{EndsAt: now, Limit: limit})
	if err != nil {
		return nil, nil, nil, err
	}
	past = make([]dropsRow, 0, len(pastCamps))
	for _, c := range pastCamps {
		if !passesWhitelist(allow, hasWhitelist, c.Game) {
			continue
		}
		past = append(past, dropsRow{
			When:         time.Unix(c.EndsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
		})
	}

	// Past — also union in claim history so claimed drops are visible
	// even if (for any reason) the campaign row was missing or evicted.
	// Each claim row becomes its own dropsRow with the BenefitName +
	// account populated; we de-dupe by (campaign_id, benefit_id) so the
	// claim view supersedes the bare-campaign view.
	claims, err := d.q.ListRecentClaims(ctx, limit)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, row := range claims {
		if !passesWhitelist(allow, hasWhitelist, row.Game) {
			continue
		}
		past = append(past, dropsRow{
			When:         time.Unix(row.ClaimedAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     row.Platform,
			Game:         row.Game,
			CampaignName: row.CampaignName,
			BenefitName:  row.BenefitName,
			AccountName:  row.AccountName,
		})
	}

	// Current: starts_at <= now < ends_at.
	currentCamps, err := d.q.ListCurrentCampaigns(ctx, gen.ListCurrentCampaignsParams{
		StartsAt: now, EndsAt: now, Limit: limit,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	current = make([]dropsRow, 0, len(currentCamps))
	for _, c := range currentCamps {
		if !passesWhitelist(allow, hasWhitelist, c.Game) {
			continue
		}
		current = append(current, dropsRow{
			When:         time.Unix(c.EndsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
		})
	}

	// Upcoming: starts_at > now.
	upcomingCamps, err := d.q.ListUpcomingCampaigns(ctx, gen.ListUpcomingCampaignsParams{
		StartsAt: now, Limit: limit,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	upcoming = make([]dropsRow, 0, len(upcomingCamps))
	for _, c := range upcomingCamps {
		if !passesWhitelist(allow, hasWhitelist, c.Game) {
			continue
		}
		upcoming = append(upcoming, dropsRow{
			When:         time.Unix(c.StartsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
		})
	}

	return past, current, upcoming, nil
}
