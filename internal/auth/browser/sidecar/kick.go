package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// Kick wraps Browser with Kick.com-specific page logic.
type Kick struct {
	b *Browser

	mu       sync.Mutex
	authTabs map[string]string // account_id -> persistent tab handle
}

func NewKick(b *Browser) *Kick {
	return &Kick{b: b, authTabs: map[string]string{}}
}

// acquireTab mirrors Twitch.acquireTab — returns the per-account
// persistent tab (creating one + installing stealth+cookies if missing).
// fresh=true indicates a brand-new tab where the caller must install
// stealth/cookies and navigate from scratch.
func (k *Kick) acquireTab(accountID string) (string, context.Context, bool, error) {
	if accountID == "" {
		// Anonymous mode (e.g. discovery without a logged-in account):
		// always allocate a throwaway tab.
		handle, tabCtx, err := k.b.OpenTab()
		if err != nil {
			return "", nil, false, err
		}
		return handle, tabCtx, true, nil
	}
	k.mu.Lock()
	if h, ok := k.authTabs[accountID]; ok {
		if c, ok := k.b.Tab(h); ok {
			k.mu.Unlock()
			return h, c, false, nil
		}
		delete(k.authTabs, accountID)
	}
	k.mu.Unlock()

	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return "", nil, false, err
	}
	k.mu.Lock()
	k.authTabs[accountID] = handle
	k.mu.Unlock()
	return handle, tabCtx, true, nil
}

// closeAuthTab tears down the persistent tab for an account (used on
// fatal init errors so the next call starts fresh).
func (k *Kick) closeAuthTab(accountID string) {
	k.mu.Lock()
	h, ok := k.authTabs[accountID]
	delete(k.authTabs, accountID)
	k.mu.Unlock()
	if ok {
		k.b.CloseTab(h)
	}
}

// InstallCookies pushes the user-supplied session cookies into a tab
// before navigation. Must be called before chromedp.Navigate.
func (k *Kick) InstallCookies(ctx context.Context, session *pb.KickSession) error {
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, c := range session.Cookies {
				expr := network.SetCookie(c.Name, c.Value).
					WithDomain(c.Domain).
					WithPath(c.Path)
				if err := expr.Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
	)
}

// VerifyAuth confirms the supplied cookies are valid and returns the
// authenticated user's username. It installs the stealth shim, lands on
// kick.com, then calls /api/v1/user from inside the authenticated page
// context (instead of navigating directly to the JSON endpoint) so
// Cloudflare treats the request as user-initiated. The response is
// parsed against several known body shapes.
func (k *Kick) VerifyAuth(ctx context.Context, session *pb.KickSession) (string, error) {
	// Install stealth shim BEFORE navigation so it runs at the top of
	// every document the tab loads.
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(StealthScript).Do(ctx)
			return err
		}),
	); err != nil {
		return "", fmt.Errorf("install stealth: %w", err)
	}
	if err := k.InstallCookies(ctx, session); err != nil {
		return "", fmt.Errorf("install cookies: %w", err)
	}
	// Land on the kick.com origin so the subsequent fetch inherits
	// first-party cookies + a real document context. 5s gives Cloudflare's
	// JS challenge time to settle.
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://kick.com/"),
		chromedp.Sleep(5*time.Second),
	); err != nil {
		return "", fmt.Errorf("verify auth navigate kick.com: %w", err)
	}

	const apiURL = "https://kick.com/api/v1/user"
	body, status, err := evalFetchGET(ctx, apiURL)
	if err != nil {
		return "", fmt.Errorf("verify auth fetch %s: %w", apiURL, err)
	}
	if status != 200 {
		return "", fmt.Errorf("verify auth: GET %s status=%d body=%s", apiURL, status, truncate(string(body), 200))
	}

	username, ok := parseKickUsername(body)
	if !ok {
		slog.Warn("kick verify auth: unrecognized body shape", "url", apiURL, "status", status, "body_prefix", truncate(string(body), 200))
		return "", fmt.Errorf("verify auth: no username field in %s response (status=%d body=%s)", apiURL, status, truncate(string(body), 200))
	}
	return username, nil
}

// parseKickUsername attempts to pull a username out of any of the
// shapes /api/v1/user has been observed to return:
//   - {"username": "x", ...}              (legacy flat)
//   - {"data": {"username": "x", ...}}    (JSON:API-style wrapper)
//   - {"user": {"username": "x", ...}}    (nested "user" object)
//
// Returns ("", false) if the body isn't JSON or no shape matches with
// a non-empty username.
func parseKickUsername(raw []byte) (string, bool) {
	// Try flat shape first.
	var flat struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &flat); err == nil && flat.Username != "" {
		return flat.Username, true
	}
	// Try wrapped shapes.
	var wrapped struct {
		Data struct {
			Username string `json:"username"`
		} `json:"data"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if wrapped.Data.Username != "" {
			return wrapped.Data.Username, true
		}
		if wrapped.User.Username != "" {
			return wrapped.User.Username, true
		}
	}
	return "", false
}

// OpenStream opens kick.com/<channel> in a new tab; the HLS player
// auto-loads on the page and watch time accrues as long as the tab stays open.
func (k *Kick) OpenStream(channel string, session *pb.KickSession) (string, error) {
	handle, ctx, err := k.b.OpenTab()
	if err != nil {
		return "", err
	}
	if err := k.InstallCookies(ctx, session); err != nil {
		k.b.CloseTab(handle)
		return "", err
	}
	err = chromedp.Run(ctx,
		chromedp.Navigate(fmt.Sprintf("https://kick.com/%s", channel)),
		chromedp.Sleep(5*time.Second),
	)
	if err != nil {
		k.b.CloseTab(handle)
		return "", fmt.Errorf("open stream %s: %w", channel, err)
	}
	return handle, nil
}

// Inventory scrapes the user's drops inventory page.
//
// NOTE: This relies on window.__NEXT_DATA__ which is injected by Next.js SSR.
// Kick.com may migrate away from Next.js or change the pageProps schema at any
// time. If the drops array is missing from the JSON path the function returns
// an empty slice rather than an error — callers should log a warning and treat
// this as "no active drops" until the schema can be confirmed.
func (k *Kick) Inventory(ctx context.Context, session *pb.KickSession) ([]*pb.DropProgress, error) {
	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return nil, err
	}
	defer k.b.CloseTab(handle)

	if err := k.InstallCookies(tabCtx, session); err != nil {
		return nil, err
	}

	var raw string
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/dashboard/drops"),
		chromedp.Sleep(3*time.Second),
		chromedp.Evaluate(`JSON.stringify(window.__NEXT_DATA__ || {})`, &raw),
	)
	if err != nil {
		return nil, fmt.Errorf("inventory navigate: %w", err)
	}
	return parseInventoryNextData(raw)
}

// parseInventoryNextData extracts drops progress from the Next.js page state.
// Returns an empty slice when the JSON path is missing (common when the user
// has no active drops). Schema drift in production should be flagged here.
func parseInventoryNextData(raw string) ([]*pb.DropProgress, error) {
	var page struct {
		Props struct {
			PageProps struct {
				Drops []struct {
					ID             string `json:"id"`
					MinutesWatched int32  `json:"minutesWatched"`
					Claimed        bool   `json:"claimed"`
				} `json:"drops"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return nil, fmt.Errorf("parse next data: %w", err)
	}
	out := make([]*pb.DropProgress, 0, len(page.Props.PageProps.Drops))
	for _, d := range page.Props.PageProps.Drops {
		out = append(out, &pb.DropProgress{
			BenefitId:      d.ID,
			MinutesWatched: d.MinutesWatched,
			Claimed:        d.Claimed,
		})
	}
	return out, nil
}

// ScrapeActiveDrops loads https://kick.com/drops in the per-account
// auth tab and returns every active drop campaign Kick currently
// surfaces (not just Rust). Kick's /drops page is rendered by Nuxt /
// Next.js so the page state lives in window.__NUXT__ or
// window.__NEXT_DATA__. We grab both, then fall back to a DOM scrape
// for [data-drop-card] cards if neither state object yields campaigns.
//
// The shape of Kick's state JSON has not been stable enough to lock to
// a single struct, so parseActiveDropsJSON walks every nested
// array/object looking for objects that look like a campaign (have an
// "id" + "game" + "name"-ish key) and lifts them out.
func (k *Kick) ScrapeActiveDrops(ctx context.Context, accountID string, session *pb.KickSession) ([]*pb.KickCampaign, error) {
	handle, tabCtx, fresh, err := k.acquireTab(accountID)
	if err != nil {
		return nil, err
	}
	// For anonymous tabs (accountID == "") we own the handle and should
	// close it on exit. For per-account tabs, leave them open — the next
	// scrape reuses the same tab so Cloudflare keeps trusting us.
	if accountID == "" {
		defer k.b.CloseTab(handle)
	}

	if fresh {
		if err := chromedp.Run(tabCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				_, err := page.AddScriptToEvaluateOnNewDocument(StealthScript).Do(ctx)
				return err
			}),
		); err != nil {
			if accountID != "" {
				k.closeAuthTab(accountID)
			}
			return nil, fmt.Errorf("install stealth: %w", err)
		}
		if session != nil && len(session.Cookies) > 0 {
			if err := k.InstallCookies(tabCtx, session); err != nil {
				if accountID != "" {
					k.closeAuthTab(accountID)
				}
				return nil, fmt.Errorf("install cookies: %w", err)
			}
		}
	}

	// Kick's canonical drops listing is /drops/all-campaigns (user-
	// pointed). Force a fresh render via about:blank to avoid serving
	// the cached degraded HTML when Cloudflare flagged us earlier.
	const dropsURL = "https://kick.com/drops/all-campaigns"
	var nuxtRaw, nextRaw, htmlRaw, domRaw string
	const domScrapeScript = `(() => {
		// Walk every img[alt] (game box-art) and climb to its card
		// ancestor — same pattern as the Twitch scraper. Kick uses
		// portrait box art for each campaign card.
		const out = [];
		const seen = new Set();
		const imgs = Array.from(document.querySelectorAll('img[alt]'));
		for (const img of imgs) {
			const alt = (img.alt || '').trim();
			if (!alt || alt.length > 80) continue;
			const w = img.naturalWidth || img.width || 0;
			const h = img.naturalHeight || img.height || 0;
			if (w > 0 && h > 0 && (h <= w * 1.1)) continue;
			let card = img.parentElement;
			for (let i = 0; i < 6 && card; i++) {
				const txt = (card.textContent || '').trim();
				if (txt.length > alt.length + 4 && card.children.length >= 2) break;
				card = card.parentElement;
			}
			if (!card) continue;
			const txt = (card.textContent || '').trim();
			if (txt.length < alt.length + 5) continue;
			const lines = txt.split('\n').map(s => s.trim()).filter(Boolean);
			const nameLine = lines.find(l => l && l !== alt);
			const name = (nameLine || lines[0] || alt).slice(0, 200);
			const link = card.querySelector('a[href*="/drops/"]');
			const href = link ? (link.getAttribute('href') || '') : '';
			const id = (href || (alt + '|' + name));
			if (seen.has(id)) continue;
			seen.add(id);
			out.push({id: id, name: name, game: alt});
		}
		return JSON.stringify(out);
	})()`
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("about:blank"),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Navigate(dropsURL),
		chromedp.Sleep(8*time.Second),
		chromedp.Evaluate(`window.scrollTo(0,document.body.scrollHeight); 1`, new(int)),
		chromedp.Sleep(3*time.Second),
		chromedp.Evaluate(`window.scrollTo(0,0); 1`, new(int)),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`JSON.stringify(window.__NUXT__ || {})`, &nuxtRaw),
		chromedp.Evaluate(`JSON.stringify(window.__NEXT_DATA__ || {})`, &nextRaw),
		chromedp.Evaluate(domScrapeScript, &domRaw),
		chromedp.Evaluate(`document.documentElement.outerHTML`, &htmlRaw),
	)
	if err != nil {
		return nil, fmt.Errorf("scrape drops navigate: %w", err)
	}

	camps, src := parseActiveDropsState(nuxtRaw, nextRaw)
	if len(camps) > 0 {
		slog.Info("kick scrape drops parsed state", "source", src, "count", len(camps))
		return camps, nil
	}
	// New: img[alt] walk via DOM script (mirrors Twitch scrape). Most
	// reliable for the modern Kick app which doesn't expose state via
	// __NUXT__ or __NEXT_DATA__ anymore.
	if domRaw != "" {
		var domCards []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Game string `json:"game"`
		}
		if err := json.Unmarshal([]byte(domRaw), &domCards); err == nil && len(domCards) > 0 {
			out := make([]*pb.KickCampaign, 0, len(domCards))
			for _, c := range domCards {
				out = append(out, &pb.KickCampaign{Id: c.ID, Name: c.Name, Game: c.Game, Status: "active"})
			}
			slog.Info("kick scrape drops parsed dom-walk", "count", len(out))
			return dedupCampaigns(out), nil
		}
	}
	// Fallback: HTML data-attribute scan (legacy).
	camps = parseActiveDropsHTML(htmlRaw)
	if len(camps) > 0 {
		slog.Info("kick scrape drops parsed dom-attr", "count", len(camps))
		return camps, nil
	}
	slog.Warn("kick scrape drops: no campaigns found",
		"nuxt_prefix", truncate(nuxtRaw, 200),
		"next_prefix", truncate(nextRaw, 200),
		"dom_prefix", truncate(domRaw, 200),
		"html_prefix", truncate(htmlRaw, 200),
	)
	return nil, nil
}

// parseActiveDropsState scans the Nuxt/Next.js page state JSON for
// objects shaped like a Kick drop campaign and returns them.
// Returns the campaigns plus a tag indicating which source matched
// (for logging only). Defensive against schema drift — Kick has
// changed its /drops page state shape twice already in 2025-2026.
func parseActiveDropsState(nuxtRaw, nextRaw string) ([]*pb.KickCampaign, string) {
	if camps := scanForCampaigns(nuxtRaw); len(camps) > 0 {
		return camps, "nuxt"
	}
	if camps := scanForCampaigns(nextRaw); len(camps) > 0 {
		return camps, "next"
	}
	return nil, ""
}

// scanForCampaigns walks an arbitrary JSON tree pulling out every
// object that looks like a Kick drop campaign. Recognized fields:
//   - id (string) — required
//   - game / gameName / gameSlug (string) — required
//   - name / title (string) — required
//   - startsAt / starts_at / startDate (string|number) — optional
//   - endsAt / ends_at / endDate (string|number) — optional
//   - status (string) — optional
//   - benefits / rewards / drops (array) — optional
func scanForCampaigns(raw string) []*pb.KickCampaign {
	if raw == "" || raw == "{}" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	out := []*pb.KickCampaign{}
	walkJSON(root, func(obj map[string]any) {
		camp, ok := campaignFromObj(obj)
		if ok {
			out = append(out, camp)
		}
	})
	return dedupCampaigns(out)
}

// walkJSON recursively walks an arbitrary JSON value invoking visit
// for every map[string]any encountered.
func walkJSON(v any, visit func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		visit(x)
		for _, vv := range x {
			walkJSON(vv, visit)
		}
	case []any:
		for _, vv := range x {
			walkJSON(vv, visit)
		}
	}
}

func campaignFromObj(obj map[string]any) (*pb.KickCampaign, bool) {
	id := firstString(obj, "id", "campaignId", "campaign_id", "slug")
	name := firstString(obj, "name", "title", "campaignName")
	game := firstString(obj, "game", "gameName", "game_name", "gameSlug", "game_slug")
	// Drop-down for nested game object: {game: {name: "Rust"}}
	if game == "" {
		if g, ok := obj["game"].(map[string]any); ok {
			game = firstString(g, "name", "slug", "title")
		}
	}
	if id == "" || name == "" || game == "" {
		return nil, false
	}
	camp := &pb.KickCampaign{
		Id:       id,
		Game:     game,
		Name:     name,
		StartsAt: firstUnix(obj, "startsAt", "starts_at", "startDate", "start_date", "startTime"),
		EndsAt:   firstUnix(obj, "endsAt", "ends_at", "endDate", "end_date", "endTime"),
		Status:   firstString(obj, "status", "state"),
	}
	if camp.Status == "" {
		camp.Status = "active"
	}
	camp.Benefits = benefitsFromObj(obj)
	return camp, true
}

func benefitsFromObj(obj map[string]any) []*pb.KickBenefit {
	for _, key := range []string{"benefits", "rewards", "drops", "items"} {
		arr, ok := obj[key].([]any)
		if !ok {
			continue
		}
		out := make([]*pb.KickBenefit, 0, len(arr))
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := firstString(m, "id", "benefitId", "benefit_id", "rewardId", "slug")
			name := firstString(m, "name", "title", "label")
			if id == "" && name == "" {
				continue
			}
			if id == "" {
				id = name
			}
			out = append(out, &pb.KickBenefit{
				Id:              id,
				Name:            name,
				RequiredMinutes: int32(firstInt(m, "requiredMinutes", "required_minutes", "minutesRequired", "minutes")),
				ImageUrl:        firstString(m, "imageUrl", "image_url", "image", "thumbnail"),
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func firstString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstInt(obj map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := obj[k].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		case string:
			// best effort numeric string
			n := int64(0)
			parsed := false
			for _, c := range v {
				if c < '0' || c > '9' {
					parsed = false
					break
				}
				n = n*10 + int64(c-'0')
				parsed = true
			}
			if parsed {
				return n
			}
		}
	}
	return 0
}

// firstUnix returns a unix-seconds timestamp from a JSON field. Accepts
// either RFC3339 strings or already-numeric epoch values (seconds OR
// milliseconds — values > 1e12 are assumed ms).
func firstUnix(obj map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := obj[k].(type) {
		case string:
			if v == "" {
				continue
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t.Unix()
			}
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t.Unix()
			}
		case float64:
			n := int64(v)
			if n > 1e12 {
				return n / 1000
			}
			return n
		}
	}
	return 0
}

func dedupCampaigns(in []*pb.KickCampaign) []*pb.KickCampaign {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]*pb.KickCampaign, 0, len(in))
	for _, c := range in {
		if _, ok := seen[c.Id]; ok {
			continue
		}
		seen[c.Id] = struct{}{}
		out = append(out, c)
	}
	return out
}

// parseActiveDropsHTML is the last-ditch fallback: pull data-drop-card
// attributes (or a similar marker) straight out of the rendered HTML.
// Kick has moved this markup around but the cards consistently carry a
// data-* hook on the outer element. We accept several candidates so a
// rename doesn't break us silently.
func parseActiveDropsHTML(html string) []*pb.KickCampaign {
	if html == "" {
		return nil
	}
	// Look for "data-drop-card" / "data-campaign-id" / "data-drop-id"
	// attributes. We don't actually need to parse HTML — the attribute
	// itself carries the campaign id, and the surrounding aria-label /
	// alt text typically carries the game + name. Best effort only.
	out := []*pb.KickCampaign{}
	for _, marker := range []string{"data-drop-card=", "data-campaign-id=", "data-drop-id="} {
		idx := 0
		for {
			i := strings.Index(html[idx:], marker)
			if i < 0 {
				break
			}
			start := idx + i + len(marker)
			// Expect: "ID"  (quoted)
			if start >= len(html) || (html[start] != '"' && html[start] != '\'') {
				idx = start
				continue
			}
			quote := html[start]
			end := strings.IndexByte(html[start+1:], quote)
			if end < 0 {
				break
			}
			id := html[start+1 : start+1+end]
			idx = start + 1 + end
			if id == "" {
				continue
			}
			out = append(out, &pb.KickCampaign{
				Id:     id,
				Game:   "",
				Name:   id,
				Status: "active",
			})
		}
		if len(out) > 0 {
			break
		}
	}
	// DOM fallback can't reliably surface game/name without a proper
	// HTML parser. Drop entries with no game — they'd fail every
	// whitelist anyway and pollute the dashboard.
	filtered := out[:0]
	for _, c := range out {
		if c.Game != "" {
			filtered = append(filtered, c)
		}
	}
	return dedupCampaigns(filtered)
}

// Claim drives the claim button for a specific benefit. If the click
// fails because the button isn't there (already claimed) we treat that
// as benign success with alreadyClaimed=true.
func (k *Kick) Claim(ctx context.Context, session *pb.KickSession, benefitID string) (bool, error) {
	handle, tabCtx, err := k.b.OpenTab()
	if err != nil {
		return false, err
	}
	defer k.b.CloseTab(handle)

	if err := k.InstallCookies(tabCtx, session); err != nil {
		return false, err
	}

	selector := fmt.Sprintf(`button[data-benefit-id=%q]`, benefitID)
	claimedSelector := fmt.Sprintf(`[data-benefit-id=%q] .claimed-badge`, benefitID)

	var alreadyClaimed bool
	err = chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/dashboard/drops"),
		chromedp.Sleep(3*time.Second),
		chromedp.Click(selector, chromedp.NodeVisible),
		chromedp.Sleep(2*time.Second),
		chromedp.EvaluateAsDevTools(
			fmt.Sprintf(`!!document.querySelector(%q)`, claimedSelector),
			&alreadyClaimed,
		),
	)
	if err != nil {
		// Treat click-failure as "button missing because already claimed".
		return true, nil
	}
	return alreadyClaimed, nil
}
