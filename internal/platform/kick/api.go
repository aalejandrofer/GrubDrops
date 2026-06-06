package kick

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// doer performs a single Kick HTTP call. Implemented by httpDoer (utls Chrome
// fingerprint); an interface so tests can inject canned responses.
type doer interface {
	do(ctx context.Context, sess platform.Session, method, path string, body []byte) ([]byte, int, error)
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
	ID       string
	Name     string
	Game     string
	Status   string
	StartsAt string
	EndsAt   string
	Rewards  []kickReward
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
			ID:       mstr(m, "id", "campaign_id", "campaignId", "uuid"),
			Name:     mstr(m, "name", "title"),
			Game:     gameName(m),
			Status:   mstr(m, "status", "state"),
			StartsAt: mstr(m, "starts_at", "startsAt", "start_at", "start_time"),
			EndsAt:   mstr(m, "ends_at", "endsAt", "end_at", "end_time"),
		}
		for _, rm := range mlist(m, "rewards", "benefits", "drops", "tiers") {
			c.Rewards = append(c.Rewards, kickReward{
				ID:              mstr(rm, "id", "reward_id", "rewardId", "uuid"),
				Name:            mstr(rm, "name", "title"),
				RequiredMinutes: mnum(rm, "required_units", "required_minutes", "requiredMinutes", "required_time", "minutes", "required_watch_time"),
				ImageURL:        mstr(rm, "image_url", "imageUrl", "image", "icon"),
			})
		}
		out = append(out, c)
	}
	return out, nil
}

// CampaignLivestreams returns channels currently serving a specific campaign —
// the per-campaign allow-list equivalent. AUTHED.
func (a *api) CampaignLivestreams(ctx context.Context, sess platform.Session, campaignID string) ([]platform.Stream, error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, dropsBase+"/api/v1/drops/campaigns/"+campaignID+"/livestreams", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("campaign livestreams %s: status %d", campaignID, status)
	}
	var resp livestreamsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode campaign livestreams: %w", err)
	}
	out := make([]platform.Stream, 0, len(resp.Data))
	for _, s := range resp.Data {
		if s.Channel.Slug == "" {
			continue
		}
		out = append(out, platform.Stream{
			Channel: s.Channel.Slug, ViewerCount: s.ViewerCount,
			DropsEnabled: true, ChannelID: fmt.Sprintf("%d", s.ID),
		})
	}
	return out, nil
}

// Progress returns drop progress/inventory. AUTHED. Tolerant parsing (see
// Campaigns) — the benefit/reward id keys the watcher's claimed[] set.
func (a *api) Progress(ctx context.Context, sess platform.Session) ([]platform.Progress, error) {
	body, status, err := a.d.do(ctx, sess, http.MethodGet, dropsBase+"/api/v1/drops/progress", nil)
	if err != nil {
		return nil, err
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
