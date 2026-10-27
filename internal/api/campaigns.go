package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/store"
)

type campaignCellJSON struct {
	AccountID   string  `json:"account_id"`
	AccountName string  `json:"account_name"`
	Platform    string  `json:"platform"`
	State       string  `json:"state"`
	Reason      string  `json:"reason"`
	Minutes     int     `json:"minutes"`
	ClaimedAt   *string `json:"claimed_at"`
	// Queued: the drop is in the account's runner plan (current, Up next
	// or Blocked), so POST /api/claims accepts it.
	Queued bool `json:"queued"`
}

type campaignDropJSON struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	ImageURL        string             `json:"image_url"`
	RequiredMinutes int                `json:"required_minutes"`
	Accounts        []campaignCellJSON `json:"accounts"`
}

type campaignDetailJSON struct {
	ID        string             `json:"id"`
	Platform  string             `json:"platform"`
	Game      string             `json:"game"`
	Name      string             `json:"name"`
	BoxArtURL string             `json:"box_art_url"`
	StartsAt  *string            `json:"starts_at"`
	EndsAt    *string            `json:"ends_at"`
	Live      bool               `json:"live"`
	LinkURL   string             `json:"link_url"`
	Drops     []campaignDropJSON `json:"drops"`
}

func (s *server) routesCampaigns(r chi.Router) {
	r.Get("/campaigns/{platform}/{id}", s.getCampaign)
}

// getCampaign shows one campaign with every drop and each account's state on
// it. The board's latest scans supply the live view; the claim log marks
// claims and stands in alone once no scan sees the campaign.
func (s *server) getCampaign(w http.ResponseWriter, r *http.Request) {
	pf, id := chi.URLParam(r, "platform"), chi.URLParam(r, "id")
	if pf != "twitch" && pf != "kick" {
		writeErr(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	var claims []store.ClaimEntry
	if s.d.Claims != nil {
		c, err := s.d.Claims.ForCampaign(ctx, pf, id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal")
			return
		}
		claims = c
	}
	var (
		detail collect.CampaignDetail
		live   bool
	)
	if s.d.Board != nil {
		detail, live = s.d.Board.Campaign(pf, id)
	}
	if !live && len(claims) == 0 {
		writeErr(w, http.StatusNotFound, "not_found")
		return
	}
	accts, err := s.d.Q.ListAllAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal")
		return
	}
	names := make(map[string]string, len(accts))
	platforms := make(map[string]string, len(accts))
	for _, a := range accts {
		names[a.ID] = store.AccountName(a)
		platforms[a.ID] = a.Platform
	}

	out := campaignDetailJSON{ID: id, Platform: pf, Live: live, Drops: []campaignDropJSON{}}
	if live {
		c := detail.Campaign
		c.Platform = pf
		out.Game, out.Name = c.Game, c.Name
		out.BoxArtURL = campaignBoxArt(c, nil)
		out.StartsAt, out.EndsAt = rfc3339(c.StartsAt), rfc3339(c.EndsAt)
		out.LinkURL = detail.LinkURL
	} else {
		first := claims[0]
		out.Game, out.Name = first.Game, first.CampaignName
		out.BoxArtURL = boxArtURL(first.Game, "")
	}

	planned := plannedDrops(s.d.Pool)
	idx := map[string]int{}
	for _, d := range detail.Drops {
		dj := campaignDropJSON{ID: d.ID, Name: d.Name, ImageURL: d.ImageURL, RequiredMinutes: d.RequiredMinutes, Accounts: []campaignCellJSON{}}
		for _, c := range d.Cells {
			dj.Accounts = append(dj.Accounts, campaignCellJSON{
				AccountID: c.AccountID, Platform: c.Platform, State: c.State, Reason: c.Reason, Minutes: c.Minutes,
				Queued: planned[c.AccountID][d.ID],
			})
		}
		idx[d.ID] = len(out.Drops)
		out.Drops = append(out.Drops, dj)
	}
	// Claims override the board: a logged claim is final.
	for _, e := range claims {
		i, ok := idx[e.BenefitID]
		if !ok {
			i = len(out.Drops)
			idx[e.BenefitID] = i
			out.Drops = append(out.Drops, campaignDropJSON{
				ID: e.BenefitID, Name: e.BenefitName, ImageURL: e.ImageURL, Accounts: []campaignCellJSON{},
			})
		}
		d := &out.Drops[i]
		if d.Name == "" {
			d.Name = e.BenefitName
		}
		if d.ImageURL == "" {
			d.ImageURL = e.ImageURL
		}
		cell := -1
		for j := range d.Accounts {
			if d.Accounts[j].AccountID == e.AccountID {
				cell = j
				break
			}
		}
		if cell < 0 {
			cell = len(d.Accounts)
			d.Accounts = append(d.Accounts, campaignCellJSON{AccountID: e.AccountID, Platform: e.Platform})
		}
		c := &d.Accounts[cell]
		c.State, c.Reason, c.Queued = collect.StateClaimed, "", false
		if c.Minutes < d.RequiredMinutes {
			c.Minutes = d.RequiredMinutes
		}
		c.ClaimedAt = rfc3339(e.ClaimedAt)
	}

	// Accounts no longer in the store are dropped. Images go through the same
	// rewrite as Discover, so Kick art loads via the proxy.
	for i := range out.Drops {
		out.Drops[i].ImageURL = rewardImage(pf, out.Drops[i].ImageURL)
		kept := out.Drops[i].Accounts[:0]
		for _, c := range out.Drops[i].Accounts {
			name, ok := names[c.AccountID]
			if !ok {
				continue
			}
			c.AccountName = name
			if p := platforms[c.AccountID]; p != "" {
				c.Platform = p
			}
			kept = append(kept, c)
		}
		sort.SliceStable(kept, func(x, y int) bool {
			if kept[x].AccountName != kept[y].AccountName {
				return kept[x].AccountName < kept[y].AccountName
			}
			return kept[x].AccountID < kept[y].AccountID
		})
		out.Drops[i].Accounts = kept
	}
	writeJSON(w, http.StatusOK, out)
}

// plannedDrops maps account ID to the benefit IDs in that account's runner
// plan: the drop it mines, Up next and Blocked. A nil pool plans nothing.
func plannedDrops(p RunnerPool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if p == nil {
		return out
	}
	for _, sn := range p.Snapshots() {
		m := out[sn.AccountID]
		if m == nil {
			m = map[string]bool{}
			out[sn.AccountID] = m
		}
		if sn.Current != nil {
			m[sn.Current.Benefit.ID] = true
		}
		for _, t := range sn.UpNext {
			m[t.Benefit.ID] = true
		}
		for _, b := range sn.Blocked {
			m[b.Target.Benefit.ID] = true
		}
	}
	return out
}
