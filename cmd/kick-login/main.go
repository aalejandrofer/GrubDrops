// Command kick-login is a LOCAL diagnostic for the Kick page-scrape path.
//
// Background (see kick_issues.md): Kick's API (/api/v1/*) 403s under ANY
// CDP-driven Chrome — headless AND headed real Chrome that earned its own
// cf_clearance. CDP attachment itself is the tell. BUT page navigation/GETs are
// NOT blocked. So the production strategy is: navigate the real page and scrape
// the embedded hydration state (__NEXT_DATA__ / __NUXT__ / DOM) — never call the
// API. Auth is verified by DOM (is the user rendered logged-in?), not /api/v1/user.
//
// This tool validates that path locally and answers the IP-lock question:
//   - Inject ONLY kick_session (+ XSRF) — NOT cf_clearance. Let the headed
//     browser earn its own CF clearance by navigating.
//   - If the page renders LOGGED-IN with only a portable kick_session, then the
//     session cookie is NOT IP-locked (only cf_clearance is, and the browser
//     re-earns that at whatever IP it runs on) → the homelab sidecar can use a
//     session pasted from the user's machine.
//   - Then scrape a game directory page for live channels (the missing
//     auto-discovery piece) to confirm channels are scrapeable from page state.
//
// Run:
//
//	go run ./cmd/kick-login path/to/cookies.json [game-slug]
//
// Env toggles:
//
//	KICK_INJECT_CF=1   also inject the exported cf_clearance (to compare).
//	KICK_CHROME=<path> override the Chrome binary.
//	KICK_HOLD=1        keep the browser open 60s at the end for manual inspection.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	cdpnetwork "github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/aalejandrofer/dropsminer/internal/auth/browser/sidecar"
)

type exportedCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: kick-login path/to/cookies.json [game-slug]")
	}
	gameSlug := "rust"
	if len(os.Args) >= 3 {
		gameSlug = os.Args[2]
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		return fmt.Errorf("read cookies file: %w", err)
	}
	var exported []exportedCookie
	if err := json.Unmarshal(raw, &exported); err != nil {
		return fmt.Errorf("parse cookies json: %w", err)
	}
	want := map[string]string{}
	for _, c := range exported {
		switch c.Name {
		case "kick_session", "XSRF-TOKEN", "cf_clearance":
			want[c.Name] = c.Value
		}
	}
	if want["kick_session"] == "" {
		return fmt.Errorf("no kick_session in export")
	}
	injectCF := os.Getenv("KICK_INJECT_CF") == "1"

	chromePath := os.Getenv("KICK_CHROME")
	if chromePath == "" {
		chromePath = defaultChromePath()
	}
	if chromePath == "" {
		return fmt.Errorf("Chrome not found; set KICK_CHROME")
	}

	profileDir, _ := os.MkdirTemp("", "kick-login-*")
	defer os.RemoveAll(profileDir)

	opts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profileDir),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-popup-blocking", true),
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	ctx, cancel := context.WithTimeout(allocCtx, 150*time.Second)
	defer cancel()
	tabCtx, tabCancel := chromedp.NewContext(ctx)
	defer tabCancel()

	setCookies := []chromedp.Action{
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(sidecar.StealthScript).Do(ctx)
			return err
		}),
		setCookie("kick_session", want["kick_session"]),
		setCookie("XSRF-TOKEN", want["XSRF-TOKEN"]),
	}
	injected := "kick_session, XSRF-TOKEN"
	if injectCF && want["cf_clearance"] != "" {
		setCookies = append(setCookies, setCookie("cf_clearance", want["cf_clearance"]))
		injected += ", cf_clearance"
	}
	fmt.Printf("headed real Chrome (%s)\ninjecting: %s\n", chromePath, injected)
	if err := chromedp.Run(tabCtx, setCookies...); err != nil {
		return fmt.Errorf("install stealth+cookies: %w", err)
	}

	// 1) Navigate home, let Cloudflare settle so the browser earns its own
	//    __cf_bm / cf_clearance.
	fmt.Println("\n[1] navigate kick.com, settle Cloudflare (8s)...")
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate("https://kick.com/"),
		chromedp.Sleep(8*time.Second),
	); err != nil {
		return fmt.Errorf("navigate home: %w", err)
	}
	earned, _ := getCookies(tabCtx)
	fmt.Println("    cookies now held:", earned)
	fmt.Printf("    earned own cf_clearance: %v  (injected cf: %v)\n", hasCookie(tabCtx, "cf_clearance") && !injectCF, injectCF)

	// 2) DOM auth check (NOT the API). Look in __NEXT_DATA__ / window for the
	//    logged-in user. Proves session portability / not-IP-locked.
	fmt.Println("\n[2] DOM auth check (no API call)...")
	user, authRaw := domAuthCheck(tabCtx)
	if user != "" {
		fmt.Printf("    RESULT: LOGGED IN as %q via page state.\n", user)
		fmt.Println("    → kick_session is portable (not IP-locked); browser re-earns cf_clearance.")
		fmt.Println("      Homelab sidecar can use a session pasted from this machine.")
	} else {
		fmt.Printf("    RESULT: not detectably logged in. clues: %s\n", truncate(authRaw, 400))
	}

	// 3) Channel auto-discovery: navigate a game directory page and scrape live
	//    channels from embedded state / DOM. This is the missing piece.
	dirURL := "https://kick.com/browse/games/" + gameSlug
	fmt.Printf("\n[3] scrape live channels from %s ...\n", dirURL)
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(dirURL),
		chromedp.Sleep(6*time.Second),
	); err != nil {
		fmt.Println("    navigate err:", err)
	}
	chans := scrapeChannels(tabCtx)
	fmt.Printf("    found %d live channel(s):\n", len(chans))
	for i, c := range chans {
		if i >= 15 {
			fmt.Printf("    ... +%d more\n", len(chans)-15)
			break
		}
		fmt.Printf("      - %s\n", c)
	}
	if len(chans) == 0 {
		fmt.Println("    (none parsed — selectors/state shape need adjusting; see raw dump below)")
		fmt.Println("    raw state probe:", truncate(stateProbe(tabCtx), 600))
	}

	if os.Getenv("KICK_HOLD") == "1" {
		fmt.Println("\nKICK_HOLD=1 — holding browser open 60s for manual inspection...")
		chromedp.Run(tabCtx, chromedp.Sleep(60*time.Second))
	}
	return nil
}

func setCookie(name, val string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if val == "" {
			return nil
		}
		return cdpnetwork.SetCookie(name, val).WithDomain(".kick.com").WithPath("/").Do(ctx)
	})
}

func getCookies(ctx context.Context) (string, error) {
	var names string
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cs, e := cdpnetwork.GetCookies().WithURLs([]string{"https://kick.com/"}).Do(ctx)
		if e != nil {
			return e
		}
		for i, c := range cs {
			if i > 0 {
				names += ", "
			}
			names += c.Name
		}
		return nil
	}))
	return names, err
}

func hasCookie(ctx context.Context, name string) bool {
	var found bool
	chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cs, e := cdpnetwork.GetCookies().WithURLs([]string{"https://kick.com/"}).Do(ctx)
		if e != nil {
			return e
		}
		for _, c := range cs {
			if c.Name == name && c.Value != "" {
				found = true
			}
		}
		return nil
	}))
	return found
}

// domAuthCheck reads the logged-in user from page hydration state without
// touching the API. Tries __NEXT_DATA__, common Nuxt/window globals, then a
// DOM avatar/username probe. Returns (username, rawCluesForDebug).
func domAuthCheck(ctx context.Context) (string, string) {
	const script = `(() => {
		const out = {clues: []};
		try {
			const nd = document.getElementById('__NEXT_DATA__');
			if (nd) {
				const j = JSON.parse(nd.textContent);
				const pp = j && j.props && j.props.pageProps;
				out.clues.push('has __NEXT_DATA__');
				const cands = [pp && pp.user, pp && pp.authUser, pp && pp.currentUser,
					j && j.props && j.props.initialState && j.props.initialState.user];
				for (const c of cands) {
					if (c && (c.username || c.slug)) { out.user = c.username || c.slug; }
				}
			}
		} catch (e) { out.clues.push('next err ' + e); }
		try {
			for (const k of ['__NUXT__','__remixContext','__INITIAL_STATE__']) {
				if (window[k]) out.clues.push('has window.' + k);
			}
		} catch (e) {}
		// DOM fallback: an avatar/profile link usually carries the own channel slug.
		try {
			const a = document.querySelector('a[href^="/"][data-testid*="user"], header a img[alt]');
			if (a) out.clues.push('dom user node present');
		} catch (e) {}
		return JSON.stringify(out);
	})()`
	var res string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &res)); err != nil {
		return "", "eval err: " + err.Error()
	}
	var parsed struct {
		User  string   `json:"user"`
		Clues []string `json:"clues"`
	}
	_ = json.Unmarshal([]byte(res), &parsed)
	cl, _ := json.Marshal(parsed.Clues)
	return parsed.User, string(cl)
}

// scrapeChannels extracts live channel slugs from a directory page via embedded
// state first, then anchor-based DOM fallback.
func scrapeChannels(ctx context.Context) []string {
	const script = `(() => {
		const found = new Set();
		// embedded state: walk __NEXT_DATA__ for objects with a slug + livestream
		try {
			const nd = document.getElementById('__NEXT_DATA__');
			if (nd) {
				const walk = (o) => {
					if (!o || typeof o !== 'object') return;
					if (o.slug && (o.is_live || o.livestream || o.viewer_count != null)) found.add(o.slug);
					for (const k in o) walk(o[k]);
				};
				walk(JSON.parse(nd.textContent));
			}
		} catch (e) {}
		// DOM fallback: channel card anchors /<slug>
		try {
			document.querySelectorAll('a[href^="/"]').forEach(a => {
				const m = a.getAttribute('href').match(/^\/([a-zA-Z0-9_]+)$/);
				if (m && a.querySelector('img, video, [class*="viewer"], [class*="live"]')) found.add(m[1]);
			});
		} catch (e) {}
		return JSON.stringify([...found]);
	})()`
	var res string
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &res)); err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(res), &out)
	return out
}

// stateProbe dumps the top-level keys of the page's embedded state so we can see
// what's actually available when the scrape finds nothing.
func stateProbe(ctx context.Context) string {
	const script = `(() => {
		try {
			const nd = document.getElementById('__NEXT_DATA__');
			if (nd) { const j = JSON.parse(nd.textContent);
				const pp = j.props && j.props.pageProps;
				return 'pageProps keys: ' + (pp ? Object.keys(pp).join(',') : '(none)'); }
		} catch (e) { return 'probe err ' + e; }
		return 'no __NEXT_DATA__; title=' + document.title;
	})()`
	var res string
	chromedp.Run(ctx, chromedp.Evaluate(script, &res))
	return res
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func defaultChromePath() string {
	switch runtime.GOOS {
	case "darwin":
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	case "linux":
		for _, p := range []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
