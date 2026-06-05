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

	pb "github.com/aalejandrofer/rust-drops-miner/internal/auth/browser/gen/browser/v1"
)

// Twitch keeps one persistent logged-in twitch.tv tab per account and
// proxies GraphQL requests through it. The tab is the trust anchor for
// Twitch's anti-bot integrity check — fetch() called from the page
// context inherits the browser's full identity and tokens, so Twitch
// treats the request as coming from a real user.
type Twitch struct {
	b *Browser

	mu       sync.Mutex
	authTabs map[string]string // account_id -> tab handle
}

func NewTwitch(b *Browser) *Twitch {
	return &Twitch{b: b, authTabs: map[string]string{}}
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
		// Install the stealth shim BEFORE any navigation so it runs at
		// the top of every document — including the first page load.
		if err := chromedp.Run(tabCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
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

	// Pull /api/me-equivalent: call gql with a tiny CurrentUser query
	// from the tab context to confirm auth-token cookie is honored.
	body, status, err := t.evalFetch(tabCtx, currentUserQueryBody())
	if err != nil {
		return "", "", fmt.Errorf("verify auth fetch: %w", err)
	}
	if status != 200 {
		return "", "", fmt.Errorf("verify auth: status %d body=%s", status, truncate(string(body), 200))
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
		return "", "", fmt.Errorf("verify auth parse: %w body=%s", err, truncate(string(body), 200))
	}
	if resp.Data.CurrentUser.Login == "" {
		return "", "", fmt.Errorf("verify auth: empty login — cookies invalid")
	}
	slog.Info("twitch sidecar authenticated", "account", accountID, "login", resp.Data.CurrentUser.Login, "user_id", resp.Data.CurrentUser.ID, "tab", handle)
	return resp.Data.CurrentUser.Login, resp.Data.CurrentUser.ID, nil
}

// GQL proxies one GraphQL POST through the account's twitch.tv tab.
// Returns the raw response body and HTTP status.
func (t *Twitch) GQL(ctx context.Context, accountID string, body []byte) ([]byte, int, error) {
	tabCtx, ok := t.tabFor(accountID)
	if !ok {
		return nil, 0, fmt.Errorf("no authenticated tab for account %s", accountID)
	}
	return t.evalFetch(tabCtx, body)
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
	// We base64-encode the body so JS string-escaping doesn't choke on
	// embedded quotes or unicode in the GraphQL payload.
	bodyB64 := base64.StdEncoding.EncodeToString(body)
	script := fmt.Sprintf(`(async () => {
		const b64 = %q;
		const bin = atob(b64);
		const bytes = new Uint8Array(bin.length);
		for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
		const resp = await fetch("https://gql.twitch.tv/gql", {
			method: "POST",
			credentials: "include",
			headers: {"Content-Type": "text/plain;charset=UTF-8"},
			body: bytes,
		});
		const txt = await resp.text();
		return {status: resp.status, body: btoa(unescape(encodeURIComponent(txt)))};
	})()`, bodyB64)

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
