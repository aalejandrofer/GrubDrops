package sidecar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// Twitch keeps one persistent logged-in twitch.tv tab per account and
// proxies GraphQL requests through it. The tab is the trust anchor for
// Twitch's anti-bot integrity check — fetch() called from the page
// context inherits the browser's full identity and tokens, so Twitch
// treats the request as coming from a real user.
type Twitch struct {
	b *Browser

	mu       sync.Mutex
	authTabs map[string]string     // account_id -> tab handle
	tabMu    map[string]*sync.Mutex // account_id -> per-tab mutex (serialises navigations)

	// Last successful scrape result per account. Twitch's drops page
	// is flaky in headless — sometimes renders campaigns, sometimes
	// returns degraded HTML. Cache the last good result so an empty
	// scrape doesn't poison the watcher; we fall back to stale data
	// for up to 30 minutes.
	scrapeCacheMu sync.Mutex
	scrapeCache   map[string]scrapedCampaignsCache

	// Reward titles already claimed in this sidecar process lifetime,
	// keyed account -> normalized title. Skip on next reaper run so we
	// don't click "Claim" repeatedly on the same tile if Twitch keeps
	// the button rendered post-success (it does — the page only
	// repaints on full reload).
	claimedMu sync.Mutex
	claimed   map[string]map[string]bool
}

type scrapedCampaignsCache struct {
	camps  []apolloCampaign
	cached time.Time
}

func NewTwitch(b *Browser) *Twitch {
	return &Twitch{
		b:           b,
		authTabs:    map[string]string{},
		tabMu:       map[string]*sync.Mutex{},
		scrapeCache: map[string]scrapedCampaignsCache{},
		claimed:     map[string]map[string]bool{},
	}
}

// markClaimed records that we've successfully clicked the Claim button
// for (account, title). Subsequent reaper runs skip these titles.
func (t *Twitch) markClaimed(accountID, title string) {
	if accountID == "" || title == "" {
		return
	}
	key := normalizeTitle(title)
	t.claimedMu.Lock()
	defer t.claimedMu.Unlock()
	m, ok := t.claimed[accountID]
	if !ok {
		m = map[string]bool{}
		t.claimed[accountID] = m
	}
	m[key] = true
}

// alreadyClaimedTitles returns the set of titles we've already claimed
// for the account this process lifetime. JS receives this as a string[]
// and skips any tile whose extracted title matches.
func (t *Twitch) alreadyClaimedTitles(accountID string) []string {
	t.claimedMu.Lock()
	defer t.claimedMu.Unlock()
	m := t.claimed[accountID]
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func normalizeTitle(s string) string {
	// Lowercase + collapse whitespace + strip surrounding spaces.
	b := make([]byte, 0, len(s))
	prevSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c == ' ' || c == '\t' || c == '\n' {
			if prevSpace || len(b) == 0 {
				continue
			}
			b = append(b, ' ')
			prevSpace = true
			continue
		}
		b = append(b, c)
		prevSpace = false
	}
	if len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	return string(b)
}

// rememberScrape stores the last good scrape result for an account.
func (t *Twitch) rememberScrape(accountID string, camps []apolloCampaign) {
	if len(camps) == 0 {
		return
	}
	t.scrapeCacheMu.Lock()
	t.scrapeCache[accountID] = scrapedCampaignsCache{camps: camps, cached: time.Now()}
	t.scrapeCacheMu.Unlock()
}

// recallScrape returns the cached scrape result if it's fresh enough.
func (t *Twitch) recallScrape(accountID string) ([]apolloCampaign, bool) {
	t.scrapeCacheMu.Lock()
	defer t.scrapeCacheMu.Unlock()
	c, ok := t.scrapeCache[accountID]
	if !ok || time.Since(c.cached) > 30*time.Minute {
		return nil, false
	}
	return c.camps, true
}

// lockTab returns the per-account mutex used to serialise chromedp
// operations that navigate or evaluate scripts in the auth tab. Two
// concurrent ListActiveCampaigns calls (e.g. one from the watcher,
// one from the discovery scraper) would otherwise step on each
// other's Navigate and abort with net::ERR_ABORTED.
func (t *Twitch) lockTab(accountID string) func() {
	t.mu.Lock()
	m, ok := t.tabMu[accountID]
	if !ok {
		m = &sync.Mutex{}
		t.tabMu[accountID] = m
	}
	t.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Authenticate installs the supplied cookies, opens (or reuses) the
// per-account twitch.tv tab, navigates home, and verifies the page
// considers the user logged in.
func (t *Twitch) Authenticate(ctx context.Context, accountID string, session *pb.TwitchSession) (string, string, error) {
	handle, tabCtx, fresh, err := t.acquireTab(accountID)
	if err != nil {
		return "", "", err
	}
	if fresh {
		// Install the stealth shim AND disable CSP BEFORE any navigation.
		// CSP disabled because Twitch's connect-src may not include
		// gql.twitch.tv when scripts originate from chromedp's isolated
		// world, causing XHR/fetch to fail with status=0 readyState=4.
		if err := chromedp.Run(tabCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				if err := page.SetBypassCSP(true).Do(ctx); err != nil {
					return fmt.Errorf("bypass csp: %w", err)
				}
				_, err := page.AddScriptToEvaluateOnNewDocument(StealthScript).Do(ctx)
				return err
			}),
		); err != nil {
			t.closeTab(accountID)
			return "", "", fmt.Errorf("install stealth: %w", err)
		}
	}
	if err := installTwitchCookies(tabCtx, session); err != nil {
		t.closeTab(accountID)
		return "", "", fmt.Errorf("install cookies: %w", err)
	}
	if fresh {
		if err := chromedp.Run(tabCtx,
			chromedp.Navigate("https://www.twitch.tv/"),
			chromedp.Sleep(3*time.Second),
		); err != nil {
			t.closeTab(accountID)
			return "", "", fmt.Errorf("navigate twitch.tv: %w", err)
		}
	}

	// Best-effort verify: call a tiny CurrentUser gql query from the
	// tab context to confirm auth-token cookie is honored. PerimeterX
	// may poison fetch() on the first call; the watcher's later gql
	// requests share the same tab and often succeed once the page has
	// fully hydrated. Treat any verify failure as a warning, not a
	// fatal error — the cookies are already installed and the watcher
	// gets the real signal on its next ListActiveCampaigns.
	body, status, err := t.evalFetch(tabCtx, currentUserQueryBody())
	if err != nil {
		slog.Warn("twitch sidecar verify fetch threw; tab still ready, watcher will retry",
			"account", accountID, "tab", handle, "err", err)
		return "", "", nil
	}
	if status != 200 {
		slog.Warn("twitch sidecar verify non-200; tab still ready, watcher will retry",
			"account", accountID, "tab", handle, "status", status, "body", truncate(string(body), 200))
		return "", "", nil
	}
	var resp struct {
		Data struct {
			CurrentUser struct {
				ID    string `json:"id"`
				Login string `json:"login"`
			} `json:"currentUser"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		slog.Warn("twitch sidecar verify parse failed; tab still ready",
			"account", accountID, "err", err, "body", truncate(string(body), 200))
		return "", "", nil
	}
	if resp.Data.CurrentUser.Login == "" {
		slog.Warn("twitch sidecar verify empty login; tab still ready",
			"account", accountID)
		return "", "", nil
	}
	slog.Info("twitch sidecar authenticated", "account", accountID, "login", resp.Data.CurrentUser.Login, "user_id", resp.Data.CurrentUser.ID, "tab", handle)
	return resp.Data.CurrentUser.Login, resp.Data.CurrentUser.ID, nil
}

// GQL proxies one GraphQL POST through the account's twitch.tv tab.
// Returns the raw response body and HTTP status.
//
// When a GQL request fails (PerimeterX, CSP, CORS — all the same wall
// in practice), fall back to scraping the drops campaigns page state.
// The page is SSR + Apollo, so window.__APOLLO_STATE__ carries the
// data we need without any cross-origin XHR.
func (t *Twitch) GQL(ctx context.Context, accountID string, body []byte) ([]byte, int, error) {
	tabCtx, ok := t.tabFor(accountID)
	if !ok {
		return nil, 0, fmt.Errorf("no authenticated tab for account %s", accountID)
	}
	unlock := t.lockTab(accountID)
	defer unlock()
	// Warm the tab on /drops/inventory before Inventory queries so
	// Twitch's anti-bot sees the request originate from that page.
	// Without this the fetch fires from whatever page the tab last
	// landed on (often /drops/campaigns) and PerimeterX rejects with
	// xhr status=0 readyState=4 — the same symptom we hit when the
	// tab is unwarmed. Best-effort: errors are swallowed and the
	// evalFetch below is allowed to try regardless.
	if isInventoryQuery(body) {
		_ = chromedp.Run(tabCtx,
			chromedp.Navigate("https://www.twitch.tv/drops/inventory"),
			chromedp.Sleep(3*time.Second),
		)
	}
	resp, status, err := t.evalFetch(tabCtx, body)
	// For campaign discovery: ALWAYS scrape /drops/campaigns and union
	// with gql results. gql only returns campaigns the user is enrolled
	// in (e.g. 1 result for a user with just the Builder Cape reward),
	// while the page renders every active campaign. Union: prefer gql
	// fields where present (richer benefit data, accurate
	// isAccountConnected), fill in the rest from the scrape so the user
	// sees ALL active campaigns in /drops.
	if isCampaignsQuery(body) {
		var gqlCamps []apolloCampaign
		if err == nil && status == 200 {
			gqlCamps = extractGqlCampaigns(resp)
		}
		scrapeCamps, scrapeErr := scrapeDropsCampaignsPage(tabCtx)
		if scrapeErr == nil && len(scrapeCamps) > 0 {
			t.rememberScrape(accountID, scrapeCamps)
		} else if len(scrapeCamps) == 0 {
			if cached, ok := t.recallScrape(accountID); ok {
				slog.Info("scrape returned 0 campaigns; using cached result", "account", accountID, "cached_count", len(cached))
				scrapeCamps = cached
			}
		}
		if scrapeErr != nil {
			slog.Warn("scrape supplement failed", "account", accountID, "err", scrapeErr)
		}
		merged := unionCampaigns(gqlCamps, scrapeCamps)
		if len(merged) == 0 {
			if err == nil && status == 200 {
				return resp, status, nil
			}
			return resp, status, fmt.Errorf("evalFetch failed (%v) and scrape returned nothing", err)
		}
		slog.Info("twitch campaign discovery", "account", accountID, "gql", len(gqlCamps), "scrape", len(scrapeCamps), "merged", len(merged))
		envelope := buildViewerDropsDashboardEnvelope(merged)
		return envelope, 200, nil
	}
	return resp, status, err
}

// extractGqlCampaigns pulls the dropCampaigns array out of a successful
// ViewerDropsDashboard response, projected onto the apolloCampaign shape
// so it can be unioned with scrape results downstream.
func extractGqlCampaigns(body []byte) []apolloCampaign {
	var env struct {
		Data struct {
			CurrentUser struct {
				DropCampaigns []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					Status string `json:"status"`
					Game   struct {
						Name string `json:"displayName"`
					} `json:"game"`
					EndAt   string `json:"endAt"`
					StartAt string `json:"startAt"`
				} `json:"dropCampaigns"`
			} `json:"currentUser"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	out := make([]apolloCampaign, 0, len(env.Data.CurrentUser.DropCampaigns))
	for _, c := range env.Data.CurrentUser.DropCampaigns {
		out = append(out, apolloCampaign{
			ID:       c.ID,
			Name:     cleanCampaignName(c.Name),
			Game:     c.Game.Name,
			EndsAt:   c.EndAt,
			StartsAt: c.StartAt,
			Kind:     "drop", // gql-side campaigns are real drops the user is enrolled in
		})
	}
	return out
}

// unionCampaigns merges gql-derived and scrape-derived campaign lists.
// gql entries take priority on the (game,name) key — they have the
// real Twitch campaign ID + accurate metadata. Scrape entries fill in
// anything gql didn't return.
func unionCampaigns(gql, scrape []apolloCampaign) []apolloCampaign {
	seen := make(map[string]bool, len(gql)+len(scrape))
	key := func(c apolloCampaign) string { return c.Game + "\x00" + c.Name }
	out := make([]apolloCampaign, 0, len(gql)+len(scrape))
	for _, c := range gql {
		if c.Game == "" || c.Name == "" {
			continue
		}
		k := key(c)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, c)
	}
	for _, c := range scrape {
		if c.Game == "" || c.Name == "" {
			continue
		}
		k := key(c)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, c)
	}
	return out
}

// isCampaignsQuery sniffs the gql body to decide if it's the
// ViewerDropsDashboard query (the one our discovery cares about).
func isCampaignsQuery(body []byte) bool {
	return bytesContains(body, []byte("ViewerDropsDashboard"))
}

// isInventoryQuery sniffs the gql body for the Inventory persisted
// query. Routed to /drops/inventory warm-up before evalFetch.
func isInventoryQuery(body []byte) bool {
	return bytesContains(body, []byte(`"Inventory"`))
}

// gqlCampaignsEmpty returns true when the response body parses as a
// well-formed dropCampaigns response but the array is empty/null.
// Triggers the scrape supplement so we still see un-enrolled active
// campaigns the user could mine.
func gqlCampaignsEmpty(body []byte) bool {
	var env struct {
		Data struct {
			CurrentUser struct {
				DropCampaigns []json.RawMessage `json:"dropCampaigns"`
			} `json:"currentUser"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return len(env.Data.CurrentUser.DropCampaigns) == 0
}

func bytesContains(b, sub []byte) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}

// scrapeDropsCampaignsPage navigates the tab to /drops/campaigns and
// returns the parsed Apollo state. Same-origin so no CORS / fetch
// wrapper issues. Apollo Client serialises its cache into a JSON blob
// the page exposes as window.__APOLLO_STATE__.
type apolloCampaign struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Game     string `json:"game"`
	EndsAt   string `json:"endsAt"`
	StartsAt string `json:"startsAt"`
	Kind     string `json:"kind"`
}

func scrapeDropsCampaignsPage(tabCtx context.Context) ([]apolloCampaign, error) {
	// Navigate first to the drops page, wait for hydration, then dump
	// the Apollo cache. Wrapped in defer-restore so we don't lose the
	// tab's main twitch.tv navigation.
	// Walk EVERY apollo state entry — Twitch's cache uses several
	// __typename values for drop entries (DropCampaign, TimeBasedDrop,
	// DropCampaignSummary in different builds). Accept anything with a
	// game.{displayName,name} + a name/title + an end timestamp.
	// Resolve nested {__ref} pointers so we get the actual game name
	// instead of "{__ref:Game:12345}".
	//
	// Modern twitch.tv stopped exposing window.__APOLLO_STATE__; the
	// Apollo client instance is at window.__APOLLO_CLIENT__ if at all.
	// Try cache.extract() as a backup, then DOM scrape as last resort.
	script := `(() => {
		let state = window.__APOLLO_STATE__ || {};
		if ((!state || Object.keys(state).length === 0) && window.__APOLLO_CLIENT__) {
			try { state = window.__APOLLO_CLIENT__.cache.extract(); } catch (e) {}
		}
		// Last-resort DOM scrape: walk EVERY img[alt] in the document,
		// then climb up to the nearest container that looks like a
		// campaign card. Twitch's drop campaign cards always have a
		// game box-art image with the game name as alt text. Other
		// pages have nav icons / promo imagery — filter those by
		// requiring the card's nearest semantic ancestor to mention
		// "drop" or be inside the drops-page main content.
		const domOut = [];
		const seenGames = new Set();
		try {
			const imgs = Array.from(document.querySelectorAll('img[alt]'));
			for (const img of imgs) {
				const alt = (img.alt || '').trim();
				if (!alt || alt.length > 80) continue; // game names are short
				// Box-art aspect filter — drop campaign cards use
				// portrait orientation (game cover). Drops site nav
				// icons are square or wider.
				const w = img.naturalWidth || img.width || 0;
				const h = img.naturalHeight || img.height || 0;
				if (w > 0 && h > 0 && (h <= w * 1.1)) continue;
				// Climb up to the card root: nearest ancestor with
				// multiple text lines that mention a time or "Drops".
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
				const nameLine = lines.find(l => l && l !== alt && !l.toLowerCase().startsWith('claim'));
				const name = (nameLine || lines[0] || alt).slice(0, 200);
				const link = card.querySelector('a[href*="/drops/"]');
				const href = link ? (link.getAttribute('href') || '') : '';
				const idMatch = href.match(/\/drops\/campaigns\/([^/?#]+)/);
				const id = idMatch ? idMatch[1] : (alt + '|' + name);
				if (seenGames.has(id)) continue;
				seenGames.add(id);
				// kind detection: presence of watch-time progress
				// ("0h 0m / 4h", "watch for", "minutes to claim") =>
				// drop. Presence of "claim" / "get reward" / "reward"
				// without time hints => reward. Default "drop".
				const tlow = txt.toLowerCase();
				const hasTime = /\b\d+\s*h\s*\d+\s*m\b|\b\d+\s*\/\s*\d+\s*h(?:ours?)?\b|watch\s+(?:for|to)|minutes?\s+to\s+claim/.test(tlow);
				const hasReward = /\b(reward|claim)\b/.test(tlow);
				const kind = hasTime ? 'drop' : (hasReward ? 'reward' : 'drop');
				domOut.push({
					id: id,
					name: name,
					game: alt,
					endsAt: '',
					startsAt: '',
					kind: kind
				});
			}
		} catch (e) {}
		// Second pass: walk every <a href*=/drops/campaigns/>. Modern
		// Twitch may render cards without a portrait img[alt] (e.g. a
		// thin banner-style card). Anchor-href is the more reliable
		// discovery signal — every card links to /drops/campaigns/{id}.
		// For each anchor, walk up looking for a game name in the
		// ancestor headings OR a sibling img[alt]. Skip the link itself
		// as the name source if it's blank/too-short.
		try {
			const anchors = Array.from(document.querySelectorAll('a[href*="/drops/campaigns/"]'));
			for (const a of anchors) {
				const href = a.getAttribute('href') || '';
				const idMatch = href.match(/\/drops\/campaigns\/([^/?#]+)/);
				if (!idMatch) continue;
				const id = idMatch[1];
				if (seenGames.has(id)) continue;
				// Find a name: the anchor's own text, OR a heading inside.
				let name = (a.textContent || '').trim().split('\n').map(s => s.trim()).filter(Boolean)[0] || '';
				// Walk up to ~6 levels to find a game name. Game lives in
				// either: an img[alt] with portrait aspect, OR a sibling
				// link to /directory/category/{slug}, OR a heading.
				let game = '';
				let card = a;
				for (let i = 0; i < 6 && card; i++) {
					card = card.parentElement;
					if (!card) break;
					if (!game) {
						const img = card.querySelector('img[alt]');
						if (img && img.alt && img.alt.length > 0 && img.alt.length < 80) {
							const w = img.naturalWidth || img.width || 0;
							const h = img.naturalHeight || img.height || 0;
							if (w === 0 || h === 0 || (h > w * 0.9)) {
								game = img.alt.trim();
							}
						}
					}
					if (!game) {
						const catLink = card.querySelector('a[href*="/directory/category/"]');
						if (catLink) {
							const t = (catLink.textContent || '').trim();
							if (t && t.length < 80) game = t;
						}
					}
					if (game) break;
				}
				if (!name) name = id;
				const tlow = ((card && card.textContent) || '').toLowerCase();
				const hasTime = /\b\d+\s*h\s*\d+\s*m\b|\b\d+\s*\/\s*\d+\s*h(?:ours?)?\b|watch\s+(?:for|to)|minutes?\s+to\s+claim/.test(tlow);
				const hasReward = /\b(reward|claim)\b/.test(tlow);
				const kind = hasTime ? 'drop' : (hasReward ? 'reward' : 'drop');
				if (!game) continue; // can't whitelist without a game name
				seenGames.add(id);
				domOut.push({id: id, name: name.slice(0, 200), game: game, endsAt: '', startsAt: '', kind: kind});
			}
		} catch (e) {}
		const resolve = (v) => {
			if (!v || typeof v !== 'object') return v;
			if (v.__ref && state[v.__ref]) return state[v.__ref];
			return v;
		};
		const getGame = (v) => {
			const g = resolve(v && v.game);
			if (!g || typeof g !== 'object') return '';
			return g.displayName || g.name || '';
		};
		const out = [];
		const seen = new Set();
		for (const k of Object.keys(state)) {
			const v = state[k];
			if (!v || typeof v !== 'object') continue;
			const isCampaign = (
				v.__typename === 'DropCampaign' ||
				v.__typename === 'DropCampaignSummary' ||
				(typeof k === 'string' && (k.startsWith('DropCampaign:') || k.startsWith('DropCampaignSummary:')))
			);
			if (!isCampaign) continue;
			const id = v.id || k.split(':').slice(1).join(':');
			if (!id || seen.has(id)) continue;
			seen.add(id);
			out.push({
				id: id,
				name: v.name || v.title || '',
				game: getGame(v),
				endsAt: v.endAt || v.endsAt || v.endsAtTimestamp || '',
				startsAt: v.startAt || v.startsAt || ''
			});
		}
		// Merge: apollo entries are authoritative (real id + dates), DOM
		// entries fill gaps. Dedupe by id so apollo wins on collision.
		if (domOut.length > 0) {
			const seenIDs = new Set(out.map(c => c.id));
			for (const d of domOut) {
				if (!seenIDs.has(d.id)) {
					out.push(d);
					seenIDs.add(d.id);
				}
			}
		}
		return JSON.stringify(out);
	})()`

	var raw, debugInfo string
	debugScript := `(() => {
		const winKeys = Object.keys(window).filter(k => k.startsWith('__')).slice(0, 30);
		const hasClient = !!window.__APOLLO_CLIENT__;
		let cacheKeys = 0, typeCounts = {};
		if (hasClient) {
			try {
				const cache = window.__APOLLO_CLIENT__.cache.extract();
				cacheKeys = Object.keys(cache).length;
				for (const k of Object.keys(cache)) {
					const v = cache[k];
					if (v && typeof v === 'object' && v.__typename) {
						typeCounts[v.__typename] = (typeCounts[v.__typename] || 0) + 1;
					}
				}
			} catch (e) {}
		}
		const domCards = document.querySelectorAll('[data-test-selector*="drop"], [class*="drop-campaign"], a[href*="/drops/campaigns/"]').length;
		const title = document.title;
		const url = location.href;
		const bodyText = (document.body && document.body.innerText || '').slice(0, 1500);
		const headings = Array.from(document.querySelectorAll('h1,h2,h3,h4')).slice(0, 12).map(h => (h.textContent||'').trim()).filter(Boolean);
		const anchors = Array.from(document.querySelectorAll('a[href*="drops"]')).slice(0, 20).map(a => ({h: a.getAttribute('href'), t: (a.textContent||'').trim().slice(0,80)}));
		return JSON.stringify({
			url, title,
			apollo_client_present: hasClient,
			apollo_cache_keys: cacheKeys,
			typename_counts: typeCounts,
			dom_drop_cards: domCards,
			headings, anchors,
			body_text_prefix: bodyText,
			win_keys: winKeys
		});
	})()`
	// Dismiss OneTrust cookie banner + Terms-of-Service modal that
	// commonly blank out the drops page in a fresh headless session.
	// Pure JS clicks (no chromedp.Click) so missing elements are silent.
	const dismissBanners = `(() => {
		const tryClick = (sel) => { const e = document.querySelector(sel); if (e && e.offsetParent) { try { e.click(); return true; } catch (_) {} } return false; };
		// OneTrust accept-all
		tryClick('#onetrust-accept-btn-handler');
		tryClick('#onetrust-reject-all-handler');
		tryClick('[aria-label="Accept"]');
		tryClick('[aria-label="Reject"]');
		// Twitch ToS update modal — usually a single Accept button
		const buttons = Array.from(document.querySelectorAll('button'));
		for (const b of buttons) {
			const t = (b.textContent || '').trim().toLowerCase();
			if (t === 'accept' || t === 'i accept' || t === 'agree') {
				try { b.click(); break; } catch (_) {}
			}
		}
		return true;
	})()`
	// Scroll the page to trigger any lazy-loaded campaign list +
	// give React + intersection observers time to mount everything.
	const scrollScript = `(() => {
		window.scrollTo(0, document.body.scrollHeight);
		setTimeout(() => window.scrollTo(0, 0), 100);
		return true;
	})()`
	var dummy bool
	// Force a hard reload by bouncing through about:blank — otherwise
	// chromedp serves the cached degraded HTML from the last scrape
	// attempt and we never see the drops list re-render.
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate("about:blank"),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Navigate("https://www.twitch.tv/drops/campaigns"),
		chromedp.Sleep(5*time.Second),
		chromedp.Evaluate(dismissBanners, &dummy),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(dismissBanners, &dummy),
		chromedp.Sleep(8*time.Second),
		chromedp.Evaluate(scrollScript, &dummy),
		chromedp.Sleep(4*time.Second),
		chromedp.Evaluate(scrollScript, &dummy),
		chromedp.Sleep(4*time.Second),
		chromedp.Evaluate(debugScript, &debugInfo),
		chromedp.Evaluate(script, &raw),
	); err != nil {
		return nil, fmt.Errorf("scrape drops page: %w", err)
	}
	slog.Info("twitch drops page scrape", "apollo", debugInfo, "campaigns_raw", truncate(raw, 300))
	var camps []apolloCampaign
	if err := json.Unmarshal([]byte(raw), &camps); err != nil {
		return nil, fmt.Errorf("parse apollo state: %w (raw=%s)", err, truncate(raw, 200))
	}
	return camps, nil
}

// buildViewerDropsDashboardEnvelope synthesises a gql response with
// the same shape ListActiveCampaigns expects, so the existing parser
// in internal/platform/twitch/campaigns.go doesn't need to know about
// the scrape fallback.
//
// Scraped campaigns assume Status=ACTIVE and self.isAccountConnected=true
// so the watcher tries to mine them. If the account isn't actually
// linked, Twitch will reject the claim later — better than silently
// skipping campaigns the user CAN see in their browser.
func buildViewerDropsDashboardEnvelope(camps []apolloCampaign) []byte {
	type campOut struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Self   struct {
			IsAccountConnected bool `json:"isAccountConnected"`
		} `json:"self"`
		Game struct {
			Name string `json:"displayName"`
		} `json:"game"`
		EndAt   string `json:"endAt"`
		StartAt string `json:"startAt"`
		Kind    string `json:"__kind,omitempty"`
	}
	out := make([]campOut, 0, len(camps))
	for _, c := range camps {
		var co campOut
		co.ID = c.ID
		co.Name = cleanCampaignName(c.Name)
		co.Status = "ACTIVE"
		co.Self.IsAccountConnected = true // optimistic; watcher hits real check on claim
		co.Game.Name = c.Game
		co.EndAt = c.EndsAt
		co.StartAt = c.StartsAt
		co.Kind = c.Kind
		out = append(out, co)
	}
	resp := map[string]any{
		"data": map[string]any{
			"currentUser": map[string]any{
				"dropCampaigns": out,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// cleanCampaignName strips trailing date strings the scrape concatenates
// onto the drop name ("Builder CapeSat, May 30, 3:30 PM UTC - Mon, Jun
// 15, 6:59 AM UTC" -> "Builder Cape"). Heuristic: cut at the EARLIEST
// weekday abbreviation regardless of which day it is. The earlier
// implementation iterated day-by-day and took the first hit, which
// returned the wrong split when "Mon, " appeared later than the
// embedded "Sat" in "CapeSat".
func cleanCampaignName(s string) string {
	earliest := -1
	for _, day := range []string{"Sun, ", "Mon, ", "Tue, ", "Wed, ", "Thu, ", "Fri, ", "Sat, "} {
		i := stringsIndex(s, day)
		if i > 0 && (earliest < 0 || i < earliest) {
			earliest = i
		}
	}
	// Also accept weekday-with-no-comma like "Thu" so "FooThu, Jan 1"
	// trims at "Thu". The 3-char form is more aggressive — only apply
	// when the comma-form failed.
	if earliest < 0 {
		for _, day := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
			i := stringsIndex(s, day)
			if i > 0 && (earliest < 0 || i < earliest) {
				earliest = i
			}
		}
	}
	if earliest <= 0 {
		return s
	}
	return trimTrailingSpace(s[:earliest])
}

// stringsIndex / trimTrailingSpace inlined to avoid pulling in "strings"
// at this site (sidecar package already imports it elsewhere, but the
// linter complains about cyclic refactors during agent runs).
func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimTrailingSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// OpenStream opens twitch.tv/<channel> in a fresh tab so the embedded
// HLS player starts watch-time accrual.
func (t *Twitch) OpenStream(channel string) (string, error) {
	handle, tabCtx, err := t.b.OpenTab()
	if err != nil {
		return "", err
	}
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(fmt.Sprintf("https://www.twitch.tv/%s", channel)),
		chromedp.Sleep(5*time.Second),
	); err != nil {
		t.b.CloseTab(handle)
		return "", fmt.Errorf("open stream %s: %w", channel, err)
	}
	return handle, nil
}

// CloseAccount tears down the persistent auth tab for an account.
func (t *Twitch) CloseAccount(accountID string) {
	t.closeTab(accountID)
}

// ClaimedReward is one entry returned from ClaimRewards.
type ClaimedReward struct {
	Game  string
	Title string
}

// ClaimRewards navigates the account's auth tab to /drops/inventory
// and clicks every visible "Claim" / "Claim now" button whose tile
// belongs to a game on allowedGames (case-insensitive name OR slug).
// Empty allowedGames -> claim everything. Returns the list of titles
// successfully clicked + soft errors per-tile. Re-uses the same tab
// as ListActiveCampaigns so PerimeterX state stays warm.
func (t *Twitch) ClaimRewards(ctx context.Context, accountID string, allowedGames []string) ([]ClaimedReward, []string, error) {
	if accountID == "" {
		return nil, nil, fmt.Errorf("account_id required")
	}
	release := t.lockTab(accountID)
	defer release()
	_, tabCtx, _, err := t.acquireTab(accountID)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire tab: %w", err)
	}
	allowJSON, _ := json.Marshal(allowedGames)
	alreadyJSON, _ := json.Marshal(t.alreadyClaimedTitles(accountID))
	// Walk every tile on /drops/inventory. A claimable reward tile
	// has a button whose accessible name contains "Claim". Strategy:
	//   1. Find every "Claim" button.
	//   2. Climb up the DOM to find the nearest enclosing tile that
	//      has both a title (any non-Claim text node 2-120 chars) and
	//      a game name (from img[alt] portrait OR a section heading
	//      above the tile group).
	//   3. Skip if game not in allow-list OR title already claimed
	//      this session.
	//   4. Click + wait + return.
	// Tiles with progress bars (in-progress watch-time drops) don't
	// have a Claim button yet — they're naturally skipped.
	script := `(async () => {
		const allow = new Set((` + string(allowJSON) + ` || []).map(s => (s||'').toLowerCase()));
		const already = new Set((` + string(alreadyJSON) + ` || []).map(s => (s||'').toLowerCase()));
		const norm = (s) => (s||'').toLowerCase().replace(/\s+/g, ' ').trim();
		// Find the game heading nearest above the tile by walking up
		// + scanning previous siblings for h1-h4 text.
		const findGameAbove = (start) => {
			let node = start;
			for (let depth = 0; depth < 12 && node; depth++) {
				let sib = node.previousElementSibling;
				while (sib) {
					const h = sib.querySelector ? sib.querySelector('h1,h2,h3,h4') : null;
					if (h) {
						const t = (h.textContent || '').trim();
						if (t.length > 1 && t.length < 60 && !/claim|drop|reward/i.test(t)) {
							return t;
						}
					}
					if (sib.tagName && /^H[1-4]$/.test(sib.tagName)) {
						const t = (sib.textContent || '').trim();
						if (t.length > 1 && t.length < 60) return t;
					}
					sib = sib.previousElementSibling;
				}
				node = node.parentElement;
			}
			return '';
		};
		const out = [];
		const errors = [];
		const buttons = Array.from(document.querySelectorAll('button'));
		for (const btn of buttons) {
			const txt = (btn.textContent || '').trim().toLowerCase();
			if (!/^claim( now| reward)?$/.test(txt)) continue;
			let card = btn;
			let title = '';
			let game = '';
			for (let i = 0; i < 8 && card; i++) {
				card = card.parentElement;
				if (!card) break;
				if (!game) {
					const img = card.querySelector('img[alt]');
					if (img && img.alt) {
						const w = img.naturalWidth || img.width || 0;
						const h = img.naturalHeight || img.height || 0;
						if (w > 0 && h > 0 && (h > w * 1.1)) {
							game = (img.alt || '').trim();
						}
					}
				}
				if (!title) {
					const headings = card.querySelectorAll('p,h1,h2,h3,h4,h5,span');
					for (const el of headings) {
						const t = (el.textContent || '').trim();
						if (t && t.length > 2 && t.length < 120 && t.toLowerCase() !== 'claim') {
							title = t;
							break;
						}
					}
				}
				if (game && title) break;
			}
			if (!game) game = findGameAbove(btn);
			if (already.has(norm(title))) continue;
			if (allow.size > 0 && game && !allow.has(game.toLowerCase())) continue;
			try {
				btn.click();
				await new Promise(r => setTimeout(r, 600));
				out.push({game: game || 'unknown', title: title || 'unknown'});
			} catch (e) {
				errors.push(String(e));
			}
		}
		return JSON.stringify({claimed: out, errors: errors});
	})()`
	var raw string
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate("about:blank"),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Navigate("https://www.twitch.tv/drops/inventory"),
		chromedp.Sleep(6*time.Second),
		chromedp.Evaluate(script, &raw, awaitPromise),
	); err != nil {
		return nil, nil, fmt.Errorf("claim rewards: %w", err)
	}
	var resp struct {
		Claimed []ClaimedReward `json:"claimed"`
		Errors  []string        `json:"errors"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, nil, fmt.Errorf("parse claim response (raw=%s): %w", truncate(raw, 200), err)
	}
	for _, c := range resp.Claimed {
		t.markClaimed(accountID, c.Title)
	}
	return resp.Claimed, resp.Errors, nil
}

// --- internals ---

func (t *Twitch) acquireTab(accountID string) (string, context.Context, bool, error) {
	t.mu.Lock()
	if h, ok := t.authTabs[accountID]; ok {
		if c, ok := t.b.Tab(h); ok {
			t.mu.Unlock()
			return h, c, false, nil
		}
		// stale handle, fall through and open a new one
		delete(t.authTabs, accountID)
	}
	t.mu.Unlock()

	handle, tabCtx, err := t.b.OpenTab()
	if err != nil {
		return "", nil, false, err
	}
	t.mu.Lock()
	t.authTabs[accountID] = handle
	t.mu.Unlock()
	return handle, tabCtx, true, nil
}

func (t *Twitch) tabFor(accountID string) (context.Context, bool) {
	t.mu.Lock()
	h, ok := t.authTabs[accountID]
	t.mu.Unlock()
	if !ok {
		return nil, false
	}
	return t.b.Tab(h)
}

func (t *Twitch) closeTab(accountID string) {
	t.mu.Lock()
	h, ok := t.authTabs[accountID]
	delete(t.authTabs, accountID)
	t.mu.Unlock()
	if ok {
		t.b.CloseTab(h)
	}
}

func installTwitchCookies(ctx context.Context, session *pb.TwitchSession) error {
	if session == nil {
		return nil
	}
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, c := range session.Cookies {
				domain := c.Domain
				if domain == "" {
					domain = ".twitch.tv"
				}
				path := c.Path
				if path == "" {
					path = "/"
				}
				if err := network.SetCookie(c.Name, c.Value).
					WithDomain(domain).
					WithPath(path).
					WithSecure(true).
					Do(ctx); err != nil {
					return fmt.Errorf("set cookie %s: %w", c.Name, err)
				}
			}
			return nil
		}),
	)
}

// evalFetch runs a fetch() inside the tab's page context against
// https://gql.twitch.tv/gql with the supplied JSON body, returning
// the raw response body and status code.
func (t *Twitch) evalFetch(tabCtx context.Context, body []byte) ([]byte, int, error) {
	return evalFetchPOST(tabCtx, "https://gql.twitch.tv/gql", "text/plain;charset=UTF-8", body)
}

// evalFetchPOST runs a fetch() POST inside the tab's page context against
// the supplied URL with the given content-type and body. Returns the raw
// response body and HTTP status. The body is base64-encoded over the wire
// so JS string-escaping doesn't choke on embedded quotes or unicode.
func evalFetchPOST(tabCtx context.Context, url, contentType string, body []byte) ([]byte, int, error) {
	bodyB64 := base64.StdEncoding.EncodeToString(body)
	// Use XMLHttpRequest instead of fetch(). PerimeterX wraps fetch
	// even when captured early — its bundle is shipped inline in the
	// page's <head> and runs before our AddScriptToEvaluateOnNewDocument
	// shim. XHR is wrapped less aggressively; if the original is
	// captured via window.__OrigXHR we use that, falling back to
	// XMLHttpRequest. Same-origin cookies are sent automatically
	// because the tab is already on www.twitch.tv.
	script := fmt.Sprintf(`(() => new Promise((resolve, reject) => {
		const b64 = %q;
		const bin = atob(b64);
		const bytes = new Uint8Array(bin.length);
		for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
		const XHR = window.__OrigXHR || window.XMLHttpRequest;
		const xhr = new XHR();
		const url = %q;
		xhr.open("POST", url, true);
		xhr.withCredentials = true;
		xhr.setRequestHeader("Content-Type", %q);
		// Twitch gql endpoint requires Authorization: OAuth <auth-token>
		// AND a Client-Id header. The auth-token cookie carries the
		// access token; pluck it from document.cookie. Client-Id is
		// twitch.tv's public web app id — same value the official site
		// uses, copied verbatim. Without these, gql.twitch.tv accepts
		// the request but returns the anonymous-user view (empty
		// dropCampaigns even when the user is enrolled).
		if (url.indexOf("gql.twitch.tv") >= 0) {
			const m = document.cookie.match(/(?:^|;\s*)auth-token=([^;]+)/);
			if (m && m[1]) {
				xhr.setRequestHeader("Authorization", "OAuth " + m[1]);
			}
			xhr.setRequestHeader("Client-Id", "kimne78kx3ncx6brgo4mv6wki5h1ko");
			// Forward the X-Device-Id Twitch attaches to its own
			// requests; absent → backend may fall back to anonymous
			// rate limiting which can return degraded responses.
			const dm = document.cookie.match(/(?:^|;\s*)unique_id=([^;]+)/);
			if (dm && dm[1]) {
				xhr.setRequestHeader("X-Device-Id", dm[1]);
			}
		}
		xhr.responseType = "text";
		xhr.onload = () => resolve({status: xhr.status, body: btoa(unescape(encodeURIComponent(xhr.responseText)))});
		xhr.onerror = (e) => reject(new Error("xhr error status=" + xhr.status + " readyState=" + xhr.readyState));
		xhr.ontimeout = () => reject(new Error("xhr timeout"));
		xhr.timeout = 15000;
		xhr.send(bytes);
	}))()`, bodyB64, url, contentType)

	return runEvalFetch(tabCtx, script)
}

// evalFetchGET runs a fetch() GET inside the tab's page context against
// the supplied URL with credentials included. Returns body + HTTP status.
// Uses the pristine fetch captured by StealthScript (see evalFetchPOST).
func evalFetchGET(tabCtx context.Context, url string) ([]byte, int, error) {
	script := fmt.Sprintf(`(async () => {
		const f = window.__origFetch || window.fetch;
		const resp = await f(%q, {
			method: "GET",
			credentials: "include",
			headers: {"Accept": "application/json"},
		});
		const txt = await resp.text();
		return {status: resp.status, body: btoa(unescape(encodeURIComponent(txt)))};
	})()`, url)

	return runEvalFetch(tabCtx, script)
}

func runEvalFetch(tabCtx context.Context, script string) ([]byte, int, error) {
	var result struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := chromedp.Run(tabCtx,
		chromedp.Evaluate(script, &result, awaitPromise),
	); err != nil {
		return nil, 0, fmt.Errorf("eval fetch: %w", err)
	}
	decoded, err := decodeJSB64(result.Body)
	if err != nil {
		return nil, result.Status, fmt.Errorf("decode body: %w", err)
	}
	return decoded, result.Status, nil
}

// decodeJSB64 reverses the `btoa(unescape(encodeURIComponent(txt)))`
// dance used by evalFetch to ferry arbitrary unicode response bodies
// through chromedp's JSON serializer.
func decodeJSB64(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	// The JS pipeline above first percent-encodes UTF-8, then base64s.
	// After base64 decode we have the percent-encoded UTF-8 bytes back
	// as a Latin-1 string. Decode percent-escapes to recover real bytes.
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '%' && i+2 < len(raw) {
			hi := hexDigit(raw[i+1])
			lo := hexDigit(raw[i+2])
			if hi >= 0 && lo >= 0 {
				out = append(out, byte(hi<<4|lo))
				i += 2
				continue
			}
		}
		out = append(out, raw[i])
	}
	return out, nil
}

func hexDigit(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	}
	return -1
}

// awaitPromise tells chromedp's Evaluate action to await the JS
// promise returned by the script before unmarshaling.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// currentUserQueryBody returns a tiny gql query used by Authenticate
// to confirm cookie-based auth still works.
func currentUserQueryBody() []byte {
	body, _ := json.Marshal(map[string]any{
		"operationName": "CurrentUser",
		"query":         "query CurrentUser { currentUser { id login } }",
		"variables":     map[string]any{},
	})
	return body
}
