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
}

func NewTwitch(b *Browser) *Twitch {
	return &Twitch{b: b, authTabs: map[string]string{}, tabMu: map[string]*sync.Mutex{}}
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
	resp, status, err := t.evalFetch(tabCtx, body)
	// For campaign discovery: gql often returns 200 with an empty
	// dropCampaigns array because the user is only "enrolled" in a
	// subset. The /drops/campaigns page renders ALL active campaigns
	// (enrolled + joinable). Trigger the scrape supplement whenever
	// gql returned no campaigns OR errored.
	if isCampaignsQuery(body) {
		needScrape := err != nil || status != 200 || gqlCampaignsEmpty(resp)
		if needScrape {
			if err == nil && status == 200 {
				slog.Info("gql returned empty campaigns; supplementing via drops page scrape", "account", accountID)
			} else {
				slog.Warn("gql evalFetch failed; falling back to drops page scrape", "account", accountID, "err", err, "status", status)
			}
			camps, scrapeErr := scrapeDropsCampaignsPage(tabCtx)
			if scrapeErr != nil {
				if err == nil && status == 200 {
					return resp, status, nil // keep empty result; scrape was best-effort
				}
				return resp, status, fmt.Errorf("evalFetch failed (%v) and scrape fallback failed: %w", err, scrapeErr)
			}
			envelope := buildViewerDropsDashboardEnvelope(camps)
			return envelope, 200, nil
		}
	}
	return resp, status, err
}

// isCampaignsQuery sniffs the gql body to decide if it's the
// ViewerDropsDashboard query (the one our discovery cares about).
func isCampaignsQuery(body []byte) bool {
	return bytesContains(body, []byte("ViewerDropsDashboard"))
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
		// Last-resort DOM scrape: find the "Open Drop Campaigns" and
		// "Open Reward Campaigns" section headings, then walk the
		// container that follows. Twitch uses unlabelled tailwind divs
		// for the cards — no stable selectors — so heading-anchored
		// walk is the most robust approach.
		const domOut = [];
		try {
			const findSiblingContainer = (heading) => {
				let n = heading;
				// Walk up until we find a node with a sibling list/grid.
				for (let i = 0; i < 8 && n; i++) {
					const sib = n.nextElementSibling;
					if (sib && (sib.children.length >= 1)) return sib;
					n = n.parentElement;
				}
				return null;
			};
			const headings = Array.from(document.querySelectorAll('h1,h2,h3,h4'));
			const dropHeads = headings.filter(h => {
				const t = (h.textContent || '').trim().toLowerCase();
				return t.includes('open drop') || t.includes('open reward') || t.includes('drop campaign');
			});
			for (const h of dropHeads) {
				const container = findSiblingContainer(h);
				if (!container) continue;
				// Each direct child likely a card; capture text + nested image alt as game hint.
				Array.from(container.children).forEach((child) => {
					const txt = (child.textContent || '').trim();
					if (!txt) return;
					const lines = txt.split('\n').map(s => s.trim()).filter(Boolean);
					const img = child.querySelector('img');
					const gameHint = (img && img.alt) || '';
					domOut.push({
						id: 'dom-' + domOut.length,
						name: lines[0] ? lines[0].slice(0, 200) : '',
						game: gameHint || (lines[1] || ''),
						endsAt: '',
						startsAt: ''
					});
				});
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
		// If apollo walk produced nothing AND DOM scrape produced
		// something, return DOM scrape as a fallback.
		if (out.length === 0 && domOut.length > 0) {
			return JSON.stringify(domOut);
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
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate("https://www.twitch.tv/drops/campaigns"),
		chromedp.Sleep(4*time.Second),
		chromedp.Evaluate(dismissBanners, &dummy),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(dismissBanners, &dummy),
		chromedp.Sleep(6*time.Second),
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
func buildViewerDropsDashboardEnvelope(camps []apolloCampaign) []byte {
	type campOut struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Game  struct {
			Name string `json:"displayName"`
		} `json:"game"`
		EndAt   string `json:"endAt"`
		StartAt string `json:"startAt"`
	}
	out := make([]campOut, 0, len(camps))
	for _, c := range camps {
		var co campOut
		co.ID = c.ID
		co.Name = c.Name
		co.Game.Name = c.Game
		co.EndAt = c.EndsAt
		co.StartAt = c.StartsAt
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
		xhr.open("POST", %q, true);
		xhr.withCredentials = true;
		xhr.setRequestHeader("Content-Type", %q);
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
