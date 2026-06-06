package api

import (
	"net/http"
	"sort"
	"time"

	mlog "github.com/aalejandrofer/dropsminer/internal/log"
	"github.com/aalejandrofer/dropsminer/internal/store/gen"
)

// historyDeps owns /history. Pulls claims from the on-disk claims
// table + ring-buffered reward-reaper claims + ring events so the
// page surfaces both persistent + ephemeral history in one feed.
type historyDeps struct {
	q    *gen.Queries
	ring *mlog.Ring
	t    Renderer
}

type historyClaim struct {
	When        string
	Platform    string
	Game        string
	Title       string
	CampaignName string
	Account     string
	Source      string // "drop" (claims table) or "reward" (ring)
}

type historyEvent struct {
	Time    string
	Kind    string
	Color   string
	Message string
	Account string
}

type historyPage struct {
	Claims []historyClaim
	Events []historyEvent
}

func (d *historyDeps) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := historyPage{}

	// Accounts label lookup for the events feed.
	labelByID := map[string]string{}
	if accs, err := d.q.ListAllAccounts(ctx); err == nil {
		for _, a := range accs {
			labelByID[a.ID] = "@" + a.Login
		}
	}

	// On-disk claims (real drop claims with benefit_id).
	if rows, err := d.q.ListRecentClaims(ctx, 200); err == nil {
		for _, row := range rows {
			acc := row.AccountName
			if acc != "" {
				acc = "@" + acc
			}
			page.Claims = append(page.Claims, historyClaim{
				When:         time.Unix(row.ClaimedAt, 0).UTC().Format("2006-01-02 15:04"),
				Platform:     row.Platform,
				Game:         row.Game,
				Title:        row.BenefitName,
				CampaignName: row.CampaignName,
				Account:      acc,
				Source:       "drop",
			})
		}
	}

	// Ring-buffered reward-reaper claims (no benefit_id, so they don't
	// reach the claims table). Walk the ring for kind=claim entries.
	if d.ring != nil {
		for _, l := range d.ring.Snapshot() {
			if l.Kind != "claim" {
				continue
			}
			acc := fieldStr(l.Fields, "account")
			title := fieldStr(l.Fields, "title")
			game := fieldStr(l.Fields, "game")
			// Skip malformed reward entries (would render "reward · — · —").
			// A real reward claim carries both a game and a title.
			if title == "" || game == "" {
				continue
			}
			label := labelByID[acc]
			if label == "" {
				label = acc
			}
			page.Claims = append(page.Claims, historyClaim{
				When:     l.TS.UTC().Format("2006-01-02 15:04"),
				Platform: "twitch", // reward reaper is Twitch-only
				Game:     game,
				Title:    title,
				Account:  label,
				Source:   "reward",
			})
		}
	}

	// Dedupe — the ring can carry the same reward claim more than once
	// (reaper re-fires) and we don't want duplicate rows.
	{
		seen := make(map[string]bool, len(page.Claims))
		deduped := page.Claims[:0]
		for _, c := range page.Claims {
			k := c.Account + "|" + c.Platform + "|" + c.Game + "|" + c.Title + "|" + c.When
			if seen[k] {
				continue
			}
			seen[k] = true
			deduped = append(deduped, c)
		}
		page.Claims = deduped
	}

	// Sort claims newest-first by When (string sort works because of
	// fixed-width ISO format).
	sort.SliceStable(page.Claims, func(i, j int) bool {
		return page.Claims[i].When > page.Claims[j].When
	})
	if len(page.Claims) > 100 {
		page.Claims = page.Claims[:100]
	}

	// Recent activity (everything not just claims). Newest first; cap
	// at 80 rows so the page stays scrollable but bounded.
	if d.ring != nil {
		lines := d.ring.Snapshot()
		for i := len(lines) - 1; i >= 0 && len(page.Events) < 80; i-- {
			l := lines[i]
			kind := l.Kind
			if kind == "" {
				kind = classifyEvent(l.Msg, l.Level)
			}
			acc := fieldStr(l.Fields, "account")
			label := labelByID[acc]
			page.Events = append(page.Events, historyEvent{
				Time:    l.TS.UTC().Format("15:04:05"),
				Kind:    kind,
				Color:   colorForKind(kind, l.Level),
				Message: l.Msg,
				Account: label,
			})
		}
	}

	render(w, d.t, "history.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "history",
		Page: page,
	})
}
