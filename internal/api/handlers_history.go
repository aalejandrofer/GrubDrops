package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
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
	When         string
	Platform     string
	Game         string
	Title        string
	CampaignName string
	Account      string
	Source       string // "drop" (claims table) or "reward" (ring)
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
			labelByID[a.ID] = a.DisplayName
		}
	}

	// On-disk claims (real drop claims with benefit_id).
	if rows, err := d.q.ListRecentClaims(ctx, 500); err == nil {
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

	// Ring-buffered reward claims (no benefit_id, so they don't reach
	// the claims table). Walk the ring for kind=claim entries.
	if d.ring != nil {
		page.Claims = append(page.Claims, rewardClaimsFromRing(d.ring.Snapshot(), labelByID)...)
	}

	// Dedupe — a single reward claim is double-emitted by the watcher
	// (multi-reward sweep + benefit-complete flow) and the sweep can
	// also re-fire, so collapse to one row.
	page.Claims = dedupeClaims(page.Claims)

	// Sort claims newest-first by When (string sort works because of
	// fixed-width ISO format).
	sort.SliceStable(page.Claims, func(i, j int) bool {
		return page.Claims[i].When > page.Claims[j].When
	})
	if len(page.Claims) > 300 {
		page.Claims = page.Claims[:300]
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

// rewardClaimsFromRing turns kind=claim ring entries into reward
// history rows. The watcher double-emits a single claim: the
// multi-reward sweep carries a "title" but no game, while the
// benefit-complete flow carries "benefit_name" but no "title". Only
// the title-bearing entry is a renderable reward row — the other would
// render "reward · — · —", so we drop any entry without a non-empty
// title. A legitimate Kick reward has no game, so game is NOT required.
func rewardClaimsFromRing(lines []mlog.LogLine, labelByID map[string]string) []historyClaim {
	var out []historyClaim
	for _, l := range lines {
		if l.Kind != "claim" {
			continue
		}
		title := fieldClean(l.Fields, "title")
		if title == "" {
			// No real title (missing/blank/"—") — malformed, skip.
			continue
		}
		game := fieldClean(l.Fields, "game")
		acc := fieldClean(l.Fields, "account")
		label := labelByID[acc]
		if label == "" {
			label = acc
		}
		out = append(out, historyClaim{
			When:     l.TS.UTC().Format("2006-01-02 15:04"),
			Platform: "twitch", // reward reaper is Twitch-only
			Game:     game,
			Title:    title,
			Account:  label,
			Source:   "reward",
		})
	}
	return out
}

// dedupeClaims collapses duplicate claim rows. The key intentionally
// omits game (a Kick reward emit may or may not carry one) so the
// sweep + benefit-complete double-emit and reaper re-fires collapse to
// a single row.
func dedupeClaims(claims []historyClaim) []historyClaim {
	seen := make(map[string]bool, len(claims))
	deduped := claims[:0]
	for _, c := range claims {
		k := c.Account + "|" + c.Platform + "|" + c.Title + "|" + c.CampaignName + "|" + c.When
		if seen[k] {
			continue
		}
		seen[k] = true
		deduped = append(deduped, c)
	}
	return deduped
}

// fieldClean reads a string field and normalises the "missing" cases
// (absent key, blank, or the "—" sentinel fieldStr emits) to "". It
// lets callers test for a genuinely present value with `== ""`.
func fieldClean(f map[string]any, k string) string {
	if v, ok := f[k]; ok {
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "—" {
			return ""
		}
		return s
	}
	return ""
}
