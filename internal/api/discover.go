package api

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/discover"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type discoverRewardJSON struct {
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
	Minutes  int    `json:"minutes"`
}

type discoverCampaignJSON struct {
	ID        string               `json:"id"`
	Platform  string               `json:"platform"`
	Game      string               `json:"game"`
	BoxArtURL string               `json:"box_art_url"`
	Name      string               `json:"name"`
	StartsAt  *string              `json:"starts_at"`
	EndsAt    *string              `json:"ends_at"`
	LinkURL   string               `json:"link_url"`
	NeedsLink bool                 `json:"needs_link"`
	Rewards   []discoverRewardJSON `json:"rewards"`
}

// discoverPlatformJSON is one platform's outcome. Error is "" on success, else
// needs_android, timeout or failed.
type discoverPlatformJSON struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Count int    `json:"count"`
}

type discoverJSON struct {
	FetchedAt     *string                         `json:"fetched_at"`
	NextRefreshAt *string                         `json:"next_refresh_at"`
	Platforms     map[string]discoverPlatformJSON `json:"platforms"`
	Campaigns     []discoverCampaignJSON          `json:"campaigns"`
}

func (s *server) routesDiscover(r chi.Router) {
	r.Get("/discover", func(w http.ResponseWriter, r *http.Request) { s.serveDiscover(w, r, false) })
	r.Post("/discover/refresh", func(w http.ResponseWriter, r *http.Request) { s.serveDiscover(w, r, true) })
}

func (s *server) serveDiscover(w http.ResponseWriter, r *http.Request, refresh bool) {
	if s.d.Discover == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	snap := s.d.Discover.Get(r.Context(), refresh)
	writeJSON(w, http.StatusOK, discoverView(snap, s.d.Discover))
}

func discoverView(snap discover.Snapshot, svc *discover.Service) discoverJSON {
	out := discoverJSON{
		FetchedAt:     rfc3339(snap.FetchedAt),
		NextRefreshAt: rfc3339(svc.NextRefresh()),
		Platforms:     make(map[string]discoverPlatformJSON, len(snap.Results)),
		Campaigns:     []discoverCampaignJSON{},
	}
	for name, res := range snap.Results {
		out.Platforms[name] = discoverPlatformJSON{OK: res.Err == nil, Error: discover.ErrCode(res.Err), Count: len(res.Campaigns)}
		for _, c := range res.Campaigns {
			out.Campaigns = append(out.Campaigns, discoverCampaign(c))
		}
	}
	sort.SliceStable(out.Campaigns, func(i, j int) bool {
		a, b := out.Campaigns[i], out.Campaigns[j]
		if ga, gb := strings.ToLower(a.Game), strings.ToLower(b.Game); ga != gb {
			return ga < gb
		}
		if a.Platform != b.Platform {
			return a.Platform < b.Platform
		}
		return a.ID < b.ID
	})
	return out
}

func discoverCampaign(c platform.Campaign) discoverCampaignJSON {
	link := collect.SafeLinkURL(c.AccountLinkURL)
	// Twitch reports whether the account is linked; Kick sends a connect URL
	// only for campaigns that need a link.
	needs := link != "" && (c.Platform == "kick" || !c.AccountLinked)
	cj := discoverCampaignJSON{
		ID: c.ID, Platform: c.Platform, Game: c.Game, Name: c.Name,
		BoxArtURL: boxArtURL(c.Game, c.GameID),
		StartsAt:  rfc3339(c.StartsAt), EndsAt: rfc3339(c.EndsAt),
		LinkURL: link, NeedsLink: needs, Rewards: []discoverRewardJSON{},
	}
	seen := map[discoverRewardJSON]bool{}
	for _, b := range c.Benefits {
		rw := discoverRewardJSON{Name: b.Name, ImageURL: rewardImage(c.Platform, b.ImageURL), Minutes: b.RequiredMinutes}
		if seen[rw] {
			continue
		}
		seen[rw] = true
		cj.Rewards = append(cj.Rewards, rw)
	}
	return cj
}

// rewardImage is the URL the SPA loads: Kick goes through the /api/img/kick
// proxy, other platforms use the CDN URL when it is http(s).
func rewardImage(platformName, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if platformName == "kick" {
		return "/api/img/kick?u=" + url.QueryEscape(raw)
	}
	return collect.SafeLinkURL(raw)
}
