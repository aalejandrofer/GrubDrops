package api

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/collect"
	"github.com/aalejandrofer/grubdrops/internal/discover"
	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/platform/twitch"
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
	writeJSON(w, http.StatusOK, discoverView(snap, s.d.Discover, s.chosenTwitchArt(r)))
}

// chosenTwitchArt maps each chosen game with a known Twitch id, by
// gameslug.Slug of its name, to its id-form box art. Empty on error.
func (s *server) chosenTwitchArt(r *http.Request) map[string]string {
	out := map[string]string{}
	if s.d.Chosen == nil {
		return out
	}
	chosen, err := s.d.Chosen.List(r.Context())
	if err != nil {
		return out
	}
	for _, c := range chosen {
		if u := twitch.BoxArtURL("", c.TwitchGameID); u != "" {
			out[gameslug.Slug(c.Name)] = u
		}
	}
	return out
}

// discoverView builds the Discover body. chosenArt fills in, after the
// snapshot's own Twitch art, for games only a chosen game's id covers.
func discoverView(snap discover.Snapshot, svc *discover.Service, chosenArt map[string]string) discoverJSON {
	out := discoverJSON{
		FetchedAt:     rfc3339(snap.FetchedAt),
		NextRefreshAt: rfc3339(svc.NextRefresh()),
		Platforms:     make(map[string]discoverPlatformJSON, len(snap.Results)),
		Campaigns:     []discoverCampaignJSON{},
	}
	for name, res := range snap.Results {
		out.Platforms[name] = discoverPlatformJSON{OK: res.Err == nil, Error: discover.ErrCode(res.Err), Count: len(res.Campaigns)}
	}
	art := twitchArtBySlug(snap)
	for k, u := range chosenArt {
		if art[k] == "" {
			art[k] = u
		}
	}
	for _, res := range snap.Results {
		for _, c := range res.Campaigns {
			out.Campaigns = append(out.Campaigns, discoverCampaign(c, art))
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

// twitchArtBySlug maps each Twitch game in snap, by gameslug.Slug of its
// name, to its box art, so a Kick campaign without a category image can
// borrow the art of the same game.
func twitchArtBySlug(snap discover.Snapshot) map[string]string {
	out := map[string]string{}
	for _, c := range snap.Results["twitch"].Campaigns {
		k := gameslug.Slug(c.Game)
		if k == "" || out[k] != "" {
			continue
		}
		if u := twitch.BoxArtURL(c.BoxArtURL, c.GameID); u != "" {
			out[k] = u
		}
	}
	return out
}

// campaignBoxArt is the image the SPA loads for a campaign's game. Twitch:
// the GQL game.boxArtURL, then the IGDB id form. Kick: the category image
// through the /api/img/kick proxy, then the same game's Twitch art in this
// snapshot or from its chosen game's Twitch id. Both end with the name-keyed Twitch URL, which only older games
// answer.
func campaignBoxArt(c platform.Campaign, twitchArt map[string]string) string {
	switch c.Platform {
	case "twitch":
		if u := twitch.BoxArtURL(c.BoxArtURL, c.GameID); u != "" {
			return u
		}
	case "kick":
		if u := rewardImage("kick", c.BoxArtURL); u != "" {
			return u
		}
		if u := twitchArt[gameslug.Slug(c.Game)]; u != "" {
			return u
		}
	}
	return twitch.BoxArtByName(c.Game)
}

// campaignNeedsLink reports whether the account must link an external
// account before claiming. Kick sends a connect URL only for campaigns that
// need one. Twitch leaves self.isAccountConnected false on many campaigns
// that need no link, with an empty or twitch.tv accountLinkURL; only a
// publisher link page means a link is required.
func campaignNeedsLink(c platform.Campaign, link string) bool {
	if link == "" {
		return false
	}
	if c.Platform == "twitch" {
		return platform.TwitchNeedsLink(c)
	}
	return true
}

func discoverCampaign(c platform.Campaign, twitchArt map[string]string) discoverCampaignJSON {
	link := collect.SafeLinkURL(c.AccountLinkURL)
	needs := campaignNeedsLink(c, link)
	cj := discoverCampaignJSON{
		ID: c.ID, Platform: c.Platform, Game: c.Game, Name: c.Name,
		BoxArtURL: campaignBoxArt(c, twitchArt),
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
