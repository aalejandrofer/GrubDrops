package kick

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// kickFilesBase is the CDN host for Kick reward/drop images. The drops
// API returns image_url as a host-relative path (e.g.
// "drops/reward-image/01k….png"); the UI needs an absolute URL. The real
// host is ext.cdn.kick.com (Cloudflare-fronted). Images are proxied
// through /img/kick anyway, which only trusts the path — this is for
// completeness/back-compat.
const kickFilesBase = "https://ext.cdn.kick.com/"

// absImageURL turns a Kick image_url into an absolute URL. Already-absolute
// URLs (http/https) and empty strings pass through unchanged.
func absImageURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return kickFilesBase + strings.TrimPrefix(u, "/")
}

// doer performs a single Kick HTTP call. Implemented by httpDoer (utls Chrome
// fingerprint); an interface so tests can inject canned responses.
type doer interface {
	do(ctx context.Context, sess platform.Session, method, path string, body []byte) ([]byte, int, error)
	getRaw(ctx context.Context, rawURL string) ([]byte, string, int, error)
}

// FetchImage pulls a Kick CDN asset (reward image) over the utls transport,
// bypassing Cloudflare's hotlink 403. Returns bytes + Content-Type.
func (a *api) FetchImage(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	return a.d.getRaw(ctx, rawURL)
}

// api wraps the utls transport with typed Kick endpoint calls. All paths are
// the real endpoints reverse-engineered from Kick's Next.js bundles (see
// project_kick_breakthrough_utls memory). Kick's /api/* 403s any CDP browser but
// accepts this pure-HTTP Chrome-fingerprinted client.
type api struct {
	d doer
}

func newAPI() *api { return &api{d: newHTTPDoer()} }

// ---- Public channel discovery (no auth required) -------------------------

// livestreamsResp is the /stream/livestreams/{category} shape (verified live).
type livestreamsResp struct {
	Data []struct {
		ID           int64  `json:"id"` // livestream id (used for the watch ping)
		SessionTitle string `json:"session_title"`
		IsLive       bool   `json:"is_live"`
		ViewerCount  int    `json:"viewer_count"`
		Channel      struct {
			Slug string `json:"slug"` // channel login, e.g. "tippie"
		} `json:"channel"`
	} `json:"data"`
}

// DiscoverChannelsForCategory lists currently-live channels in a Kick category
// (game). Public endpoint — works without a logged-in session. This is the
// auto-discovery that removes manual one-channel-at-a-time entry.
func (a *api) DiscoverChannelsForCategory(ctx context.Context, sess platform.Session, categorySlug string) ([]platform.Stream, error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, discoveryBase+"/stream/livestreams/"+categorySlug, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("livestreams %s: status %d", categorySlug, status)
	}
	var resp livestreamsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode livestreams: %w", err)
	}
	out := make([]platform.Stream, 0, len(resp.Data))
	for _, s := range resp.Data {
		if !s.IsLive || s.Channel.Slug == "" {
			continue
		}
		out = append(out, platform.Stream{
			Channel:      s.Channel.Slug,
			ViewerCount:  s.ViewerCount,
			DropsEnabled: true,
			ChannelID:    fmt.Sprintf("%d", s.ID), // livestream id — used by the watch ping
		})
	}
	return out, nil
}

// ---- Authed drops endpoints ----------------------------------------------
// NOTE: the response STRUCTS below are modelled on Kick's drops/all-campaigns
// scrape shape + the JS bundle field names; they are PENDING verification
// against a live authed response (run `cmd/kick-http <cookies> drops` with a
// fresh session, then finalize). Field tags are the best-effort mapping.

type kickCampaign struct {
	ID         string
	Name       string
	Game       string
	Status     string
	StartsAt   string
	EndsAt     string
	Rewards    []kickReward
	Channels   []kickChannel // eligible channels (campaign.channels[])
	ConnectURL string        // external account-link URL; non-empty = needs linking
}

// kickChannel is an eligible channel for a campaign. ID is the numeric channel
// id used by the viewer-WS channel_handshake (NOT the livestream id).
type kickChannel struct {
	Slug string
	ID   string
}

type kickReward struct {
	ID              string
	Name            string
	RequiredMinutes int
	ImageURL        string
}

// Campaigns lists the account's active drop campaigns. AUTHED.
//
// The exact JSON shape is undocumented and unverified against a live authed
// response, so we parse TOLERANTLY: accept {data:[]} / {campaigns:[]} / bare [],
// and try several field-name variants per object. This maximises the chance the
// real response parses on first contact with a fresh session.
func (a *api) Campaigns(ctx context.Context, sess platform.Session) ([]kickCampaign, error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, dropsBase+"/api/v1/drops/campaigns", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("drops campaigns: status %d body %s", status, truncate(body, 200))
	}
	items, err := asList(body, "data", "campaigns", "drops")
	if err != nil {
		return nil, fmt.Errorf("decode campaigns: %w", err)
	}
	out := make([]kickCampaign, 0, len(items))
	for _, m := range items {
		c := kickCampaign{
			ID:         mstr(m, "id", "campaign_id", "campaignId", "uuid"),
			Name:       mstr(m, "name", "title"),
			Game:       gameName(m),
			Status:     mstr(m, "status", "state"),
			StartsAt:   mstr(m, "starts_at", "startsAt", "start_at", "start_time"),
			EndsAt:     mstr(m, "ends_at", "endsAt", "end_at", "end_time"),
			ConnectURL: mstr(m, "connect_url", "connectUrl"),
		}
		for _, rm := range mlist(m, "rewards", "benefits", "drops", "tiers") {
			c.Rewards = append(c.Rewards, kickReward{
				ID:              mstr(rm, "id", "reward_id", "rewardId", "uuid"),
				Name:            mstr(rm, "name", "title"),
				RequiredMinutes: mnum(rm, "required_units", "required_minutes", "requiredMinutes", "required_time", "minutes", "required_watch_time"),
				ImageURL:        absImageURL(mstr(rm, "image_url", "imageUrl", "image", "icon")),
			})
		}
		// Eligible channels are embedded in the campaign payload (the
		// separate /campaigns/{id}/livestreams endpoint returns 400). id is
		// the channel id the viewer-WS handshake needs.
		for _, ch := range mlist(m, "channels") {
			if slug := mstr(ch, "slug", "username", "name"); slug != "" {
				c.Channels = append(c.Channels, kickChannel{Slug: slug, ID: mstr(ch, "id", "channel_id", "channelId")})
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// ChannelLivestream reports whether a channel is live and, if so, its livestream
// id (needed for the watch ping) + viewer count. Public endpoint on kick.com.
// {data:null} = offline. Used to filter a campaign's eligible channels down to
// the ones currently broadcasting.
func (a *api) ChannelLivestream(ctx context.Context, sess platform.Session, slug string) (live bool, livestreamID string, viewers int, category string, err error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, discoveryBase+"/api/v2/channels/"+slug+"/livestream", nil)
	if err != nil {
		return false, "", 0, "", err
	}
	if status != 200 {
		return false, "", 0, "", fmt.Errorf("channel livestream %s: status %d", slug, status)
	}
	var r struct {
		Data *struct {
			ID          int64 `json:"id"`
			ViewerCount int   `json:"viewer_count"`
			// The live category. Kick nests the game under
			// categories[].name (and a parent category.name); take the
			// first non-empty so we can gate watch-time on the right game.
			Categories []struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"categories"`
			Category *struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"category"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return false, "", 0, "", fmt.Errorf("decode channel livestream %s: %w", slug, err)
	}
	if r.Data == nil {
		return false, "", 0, "", nil // offline
	}
	cat := ""
	if len(r.Data.Categories) > 0 {
		cat = r.Data.Categories[0].Name
	}
	if cat == "" && r.Data.Category != nil {
		cat = r.Data.Category.Name
	}
	return true, fmt.Sprintf("%d", r.Data.ID), r.Data.ViewerCount, cat, nil
}

// Progress returns drop progress/inventory. AUTHED. Tolerant parsing (see
// Campaigns) — the benefit/reward id keys the watcher's claimed[] set.
func (a *api) Progress(ctx context.Context, sess platform.Session) ([]platform.Progress, error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, dropsBase+"/api/v1/drops/progress", nil)
	if err != nil {
		return nil, err
	}
	if status == 403 {
		// Kick returns 403 on /drops/progress until the account is enrolled /
		// participating in an active drop. Treat as "no progress yet" rather
		// than erroring the watch loop — the viewer-WS presence keeps accruing
		// time server-side; progress should open up once enrolled.
		return nil, nil
	}
	if status != 200 {
		return nil, fmt.Errorf("drops progress: status %d", status)
	}
	items, err := asList(body, "data", "progress", "drops")
	if err != nil {
		return nil, fmt.Errorf("decode progress: %w", err)
	}
	out := make([]platform.Progress, 0, len(items))
	for _, m := range items {
		out = append(out, platform.Progress{
			BenefitID:      mstr(m, "reward_id", "rewardId", "benefit_id", "benefitId", "id"),
			MinutesWatched: mnum(m, "minutes_watched", "minutesWatched", "current_minutes", "progress", "watched_minutes"),
			Claimed:        mbool(m, "claimed", "is_claimed", "isClaimed"),
		})
	}
	return out, nil
}

// ParticipatingCampaignIDs returns the set of campaign ids the account is
// enrolled in (from /drops/progress). 403 (not enrolled in any) → empty set.
// Used to decide which connect_url campaigns the account is actually linked to.
func (a *api) ParticipatingCampaignIDs(ctx context.Context, sess platform.Session) (map[string]bool, error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, dropsBase+"/api/v1/drops/progress", nil)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	if status == 403 {
		return out, nil // not enrolled in anything
	}
	if status != 200 {
		return out, fmt.Errorf("drops progress: status %d", status)
	}
	items, _ := asList(body, "data", "progress", "drops")
	for _, m := range items {
		if id := mstr(m, "campaign_id", "campaignId", "campaign"); id != "" {
			out[id] = true
		}
	}
	return out, nil
}

// ---- tolerant JSON helpers -----------------------------------------------

// asList extracts a list of objects from a response that may be a bare array or
// wrapped under one of the given keys ({data:[]}, {campaigns:[]}, ...).
func asList(body []byte, keys ...string) ([]map[string]any, error) {
	var bare []map[string]any
	if err := json.Unmarshal(body, &bare); err == nil {
		return bare, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	for _, k := range keys {
		raw, ok := obj[k]
		if !ok {
			continue
		}
		var list []map[string]any
		if err := json.Unmarshal(raw, &list); err == nil {
			return list, nil
		}
		// nested one level deeper, e.g. {data:{campaigns:[]}}
		var inner map[string]json.RawMessage
		if json.Unmarshal(raw, &inner) == nil {
			for _, k2 := range keys {
				if r2, ok := inner[k2]; ok {
					var l2 []map[string]any
					if json.Unmarshal(r2, &l2) == nil {
						return l2, nil
					}
				}
			}
		}
	}
	return nil, nil // no recognizable list — treat as empty, not an error
}

func mstr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch s := v.(type) {
			case string:
				if s != "" {
					return s
				}
			case float64:
				return fmt.Sprintf("%.0f", s)
			}
		}
	}
	return ""
}

func mnum(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case string:
				var i int
				if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
					return i
				}
			}
		}
	}
	return 0
}

func mbool(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}

// mlist returns a list of sub-objects under the first matching key.
func mlist(m map[string]any, keys ...string) []map[string]any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if arr, ok := v.([]any); ok {
				out := make([]map[string]any, 0, len(arr))
				for _, e := range arr {
					if em, ok := e.(map[string]any); ok {
						out = append(out, em)
					}
				}
				return out
			}
		}
	}
	return nil
}

// gameName handles game as a bare string or a nested object {name|slug}.
func gameName(m map[string]any) string {
	if s := mstr(m, "game", "gameName", "category"); s != "" {
		return s
	}
	for _, k := range []string{"game", "category"} {
		if sub, ok := m[k].(map[string]any); ok {
			if s := mstr(sub, "name", "slug", "title"); s != "" {
				return s
			}
		}
	}
	return ""
}

// Claim claims a drop reward. AUTHED. POST {reward_id, campaign_id}.
func (a *api) Claim(ctx context.Context, sess platform.Session, rewardID, campaignID string) error {
	payload, _ := json.Marshal(map[string]string{"reward_id": rewardID, "campaign_id": campaignID})
	body, status, err := a.d.do(ctx, sess, http.MethodPost, dropsBase+"/api/v1/drops/claim", payload)
	if err != nil {
		return err
	}
	if status != 200 && status != 201 {
		return fmt.Errorf("drops claim: status %d body %s", status, truncate(body, 200))
	}
	return nil
}

// WatchPing registers a view on a livestream — Kick accrues watch time from
// these. Call periodically (~60s) while watching. AUTHED.
func (a *api) WatchPing(ctx context.Context, sess platform.Session, livestreamID string) error {
	_, status, err := a.d.do(ctx, sess, http.MethodPost, discoveryBase+"/api/v1/video/views/"+livestreamID, nil)
	if err != nil {
		return err
	}
	if status != 200 && status != 201 && status != 204 {
		return fmt.Errorf("watch ping %s: status %d", livestreamID, status)
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
