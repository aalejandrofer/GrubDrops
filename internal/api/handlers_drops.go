package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

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
	CampaignID   string // empty for claim-source rows that lack a campaign
	When         string
	Platform     string
	Game         string
	CampaignName string
	BenefitName  string
	AccountName  string
	Kind         string // "drop" | "reward"
}

// dropsPage is what the template sees: the active tab, the three counts
// (so the tab strip can show totals), and the rows to render in the
// table body. UnlistedRows are campaigns discovered (any tab) whose game
// is NOT on any whitelist — rendered in a parallel "discoverable but
// not whitelisted" table below the main one.
type dropsPage struct {
	Tab          dropTab
	PastCount    int
	CurrentCount int
	UpcomingCount int
	Rows         []dropsRow
	UnlistedRows []dropsRow // campaigns whose Game is not on any account whitelist
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

	pastRows, currentRows, upcomingRows, unlistedRows, err := d.collectAll(r.Context(), allow, hasWhitelist, now, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	page := dropsPage{
		Tab:           tab,
		PastCount:     len(pastRows),
		CurrentCount:  len(currentRows),
		UpcomingCount: len(upcomingRows),
		UnlistedRows:  unlistedRows,
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
) (past, current, upcoming, unlisted []dropsRow, err error) {
	// Past: campaigns ended before now.
	pastCamps, err := d.q.ListPastCampaigns(ctx, gen.ListPastCampaignsParams{EndsAt: now, Limit: limit})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	past = make([]dropsRow, 0, len(pastCamps))
	for _, c := range pastCamps {
		row := dropsRow{
			CampaignID:   c.ID,
			When:         time.Unix(c.EndsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
			Kind:         c.Kind,
		}
		if passesWhitelist(allow, hasWhitelist, c.Game) {
			past = append(past, row)
		} else if hasWhitelist {
			unlisted = append(unlisted, row)
		}
	}

	// Past — also union in claim history so claimed drops are visible
	// even if (for any reason) the campaign row was missing or evicted.
	// Each claim row becomes its own dropsRow with the BenefitName +
	// account populated; we de-dupe by (campaign_id, benefit_id) so the
	// claim view supersedes the bare-campaign view.
	claims, err := d.q.ListRecentClaims(ctx, limit)
	if err != nil {
		return nil, nil, nil, nil, err
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
		return nil, nil, nil, nil, err
	}
	current = make([]dropsRow, 0, len(currentCamps))
	for _, c := range currentCamps {
		row := dropsRow{
			CampaignID:   c.ID,
			When:         time.Unix(c.EndsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
			Kind:         c.Kind,
		}
		if passesWhitelist(allow, hasWhitelist, c.Game) {
			current = append(current, row)
		} else if hasWhitelist {
			unlisted = append(unlisted, row)
		}
	}

	// Upcoming: starts_at > now.
	upcomingCamps, err := d.q.ListUpcomingCampaigns(ctx, gen.ListUpcomingCampaignsParams{
		StartsAt: now, Limit: limit,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	upcoming = make([]dropsRow, 0, len(upcomingCamps))
	for _, c := range upcomingCamps {
		row := dropsRow{
			CampaignID:   c.ID,
			When:         time.Unix(c.StartsAt, 0).UTC().Format("2006-01-02 15:04"),
			Platform:     c.Platform,
			Game:         c.Game,
			CampaignName: c.Name,
			Kind:         c.Kind,
		}
		if passesWhitelist(allow, hasWhitelist, c.Game) {
			upcoming = append(upcoming, row)
		} else if hasWhitelist {
			unlisted = append(unlisted, row)
		}
	}

	// Dedupe unlisted by (Platform, Game, CampaignName).
	if len(unlisted) > 1 {
		seen := make(map[string]bool, len(unlisted))
		out := unlisted[:0]
		for _, r := range unlisted {
			k := r.Platform + "|" + r.Game + "|" + r.CampaignName
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, r)
		}
		unlisted = out
	}

	return past, current, upcoming, unlisted, nil
}

// items returns the benefits + summary for a single campaign, rendered
// as the HTML partial that hx-get loads into a row's expanded section.
type campaignDetailRow struct {
	ID              string
	Platform        string
	Game            string
	CampaignName    string
	Kind            string
	When            string
	Status          string
	Benefits        []campaignBenefitRow
}

type campaignBenefitRow struct {
	Name            string
	RequiredMinutes int64
	ImageURL        string
}

func (d *dropsDeps) items(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	camp, err := d.q.GetCampaign(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bens, _ := d.q.ListBenefitsForCampaign(r.Context(), id)
	detail := campaignDetailRow{
		ID:           camp.ID,
		Platform:     camp.Platform,
		Game:         camp.Game,
		CampaignName: camp.Name,
		Kind:         camp.Kind,
		Status:       camp.Status,
	}
	if camp.EndsAt > 0 {
		detail.When = time.Unix(camp.EndsAt, 0).UTC().Format("2006-01-02 15:04 UTC")
	}
	for _, b := range bens {
		detail.Benefits = append(detail.Benefits, campaignBenefitRow{
			Name:            b.Name,
			RequiredMinutes: b.RequiredMinutes,
			ImageURL:        b.ImageUrl,
		})
	}
	renderPartial(w, d.t, "drops_campaign_items", detail)
}
