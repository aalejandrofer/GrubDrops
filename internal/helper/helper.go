// Package helper is the shared core used by both the dropsminer-helper
// CLI and the dropsminer-helper-gui binaries. It reads cookies from the
// user's local browser via browserutils/kooky and uploads them to a
// running rust-drops-miner deployment.
package helper

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register cookie store finders
)

// debug returns true when MINER_HELPER_DEBUG=1 — prints every miner
// HTTP request + response and lists which browser stores yielded
// cookies.
func debug() bool { return os.Getenv("MINER_HELPER_DEBUG") == "1" }

func dlog(format string, args ...any) {
	if debug() {
		log.Printf("helper: "+format, args...)
	}
}

// Config carries the connection details shared by every push.
type Config struct {
	MinerURL string // e.g. https://rdrops.ryuzec.dev
	Password string // admin password
	Browser  string // optional — limit cookie scan to a specific browser
	Insecure bool   // skip TLS verify (debug)
}

// TwitchRequest pushes the local twitch.tv cookies onto a single
// account on the miner.
type TwitchRequest struct {
	Config
	AccountID string
}

// KickRequest pushes the local kick.com cookies + the channel choice.
type KickRequest struct {
	Config
	AccountID string
	Channel   string // streamer login to mine
}

// Result is what every PushXxx returns. Echoed back to the user.
type Result struct {
	UploadedCookies []string // names of cookies that were sent
	Message         string   // human-readable summary
}

// PushTwitch reads twitch.tv cookies and POSTs them to
// /accounts/<id>/twitch/paste.
func PushTwitch(ctx context.Context, req TwitchRequest) (Result, error) {
	if req.AccountID == "" {
		return Result{}, fmt.Errorf("account-id is required")
	}
	cookies, err := readDomainCookies(ctx, "twitch.tv", req.Browser)
	if err != nil {
		return Result{}, fmt.Errorf("read browser cookies: %w", err)
	}
	want := []string{"auth-token", "persistent", "server_session_id", "twilight-user", "unique_id", "login", "name"}
	picked := pickCookies(cookies, want)
	if _, ok := picked["auth-token"]; !ok {
		return Result{}, fmt.Errorf("auth-token cookie not found in any browser jar — are you signed in to twitch.tv?")
	}

	client, err := newMinerClient(req.Config)
	if err != nil {
		return Result{}, err
	}
	if err := client.adminLogin(ctx, req.Password); err != nil {
		return Result{}, fmt.Errorf("miner login: %w", err)
	}

	form := url.Values{}
	names := make([]string, 0, len(picked))
	for k, v := range picked {
		form.Set(k, v)
		names = append(names, k)
	}
	if err := client.submit(ctx, "/accounts/"+req.AccountID+"/twitch/paste", form); err != nil {
		return Result{}, err
	}
	return Result{
		UploadedCookies: names,
		Message:         fmt.Sprintf("uploaded %d twitch cookies for account %s", len(names), req.AccountID),
	}, nil
}

// PushKick reads kick.com cookies and POSTs them to /accounts/<id>/login.
func PushKick(ctx context.Context, req KickRequest) (Result, error) {
	if req.AccountID == "" {
		return Result{}, fmt.Errorf("account-id is required")
	}
	if req.Channel == "" {
		return Result{}, fmt.Errorf("channel is required")
	}
	cookies, err := readDomainCookies(ctx, "kick.com", req.Browser)
	if err != nil {
		return Result{}, fmt.Errorf("read browser cookies: %w", err)
	}
	picked := pickCookies(cookies, []string{"kick_session", "XSRF-TOKEN", "cf_clearance"})
	if _, ok := picked["kick_session"]; !ok {
		return Result{}, fmt.Errorf("kick_session cookie not found — are you signed in to kick.com?")
	}
	if _, ok := picked["XSRF-TOKEN"]; !ok {
		return Result{}, fmt.Errorf("XSRF-TOKEN cookie not found")
	}

	client, err := newMinerClient(req.Config)
	if err != nil {
		return Result{}, err
	}
	if err := client.adminLogin(ctx, req.Password); err != nil {
		return Result{}, fmt.Errorf("miner login: %w", err)
	}

	form := url.Values{}
	form.Set("kick_session", picked["kick_session"])
	form.Set("xsrf_token", picked["XSRF-TOKEN"])
	if v, ok := picked["cf_clearance"]; ok {
		form.Set("cf_clearance", v)
	}
	form.Set("channel", req.Channel)

	if err := client.submit(ctx, "/accounts/"+req.AccountID+"/login", form); err != nil {
		return Result{}, err
	}
	names := []string{"kick_session", "XSRF-TOKEN"}
	if _, ok := picked["cf_clearance"]; ok {
		names = append(names, "cf_clearance")
	}
	return Result{
		UploadedCookies: names,
		Message:         fmt.Sprintf("uploaded kick cookies for account %s (channel %s)", req.AccountID, req.Channel),
	}, nil
}

// --- cookie reading ---

// readDomainCookies aggregates cookies for `domain` across every
// browser kooky can find. kooky returns a non-nil error whenever
// ANY store probe fails (missing browser binary, sandboxed file,
// etc.) — that's noise here, since we only care that at least one
// store had real cookies for this domain. Treat per-store failures
// as warnings and only return an error when nothing came back.
func readDomainCookies(ctx context.Context, domain, browser string) ([]*kooky.Cookie, error) {
	cookies, probeErr := kooky.ReadCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
	if browser != "" {
		out := cookies[:0]
		for _, c := range cookies {
			if c.Browser != nil && strings.EqualFold(c.Browser.Browser(), browser) {
				out = append(out, c)
			}
		}
		cookies = out
	}
	if debug() {
		seen := map[string]int{}
		for _, c := range cookies {
			b := "unknown"
			if c.Browser != nil {
				b = c.Browser.Browser()
			}
			seen[b]++
		}
		dlog("found %d cookies for %s across browsers: %v", len(cookies), domain, seen)
		if probeErr != nil {
			dlog("(some browser stores failed to open: %v)", probeErr)
		}
	}
	if len(cookies) == 0 {
		if probeErr != nil {
			return nil, fmt.Errorf("no cookies found for %s — none of the installed browsers' stores were readable: %w", domain, probeErr)
		}
		return nil, fmt.Errorf("no cookies found for %s — are you signed in there in any browser?", domain)
	}
	return cookies, nil
}

func pickCookies(all []*kooky.Cookie, want []string) map[string]string {
	bestExpiry := map[string]int64{}
	out := map[string]string{}
	for _, c := range all {
		for _, w := range want {
			if c.Name != w {
				continue
			}
			exp := c.Expires.Unix()
			if cur, ok := bestExpiry[w]; !ok || exp > cur {
				bestExpiry[w] = exp
				out[w] = c.Value
			}
		}
	}
	return out
}

// --- miner HTTP client ---

type minerClient struct {
	base *url.URL
	http *http.Client
}

func newMinerClient(cf Config) (*minerClient, error) {
	if cf.MinerURL == "" {
		return nil, fmt.Errorf("miner URL is required")
	}
	if cf.Password == "" {
		return nil, fmt.Errorf("admin password is required")
	}
	base, err := url.Parse(cf.MinerURL)
	if err != nil {
		return nil, fmt.Errorf("parse miner URL: %w", err)
	}
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cf.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	}
	return &minerClient{base: base, http: &http.Client{Jar: jar, Transport: tr}}, nil
}

func (c *minerClient) adminLogin(ctx context.Context, password string) error {
	body, err := c.get(ctx, "/login")
	if err != nil {
		return err
	}
	tok, err := extractCSRF(body)
	if err != nil {
		return fmt.Errorf("extract csrf: %w", err)
	}
	form := url.Values{"password": {password}, "csrf_token": {tok}}
	resp, err := c.postRaw(ctx, "/login", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(raw), "wrong password") {
			return fmt.Errorf("wrong password")
		}
	}
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login responded %s", resp.Status)
	}
	return nil
}

// submitErrorMarkers are substrings that, when present in the body of
// a 200 response to a form POST, indicate the miner re-rendered the
// page with an error flash rather than redirecting on success. Real
// successes are signaled by a 303 + Location header (the server's
// http.Redirect for the post-success view); a 200 means the same
// template came back, almost always because the handler appended a
// flash and called render().
//
// This list is intentionally a small, hand-picked allowlist of the
// markers the miner UI is known to produce. Each entry must match the
// exact phrase used by the server-side flash so we never report
// success when one of these errors is shown.
var submitErrorMarkers = []string{
	"sidecar rejected",
	"failed to persist",
	"wrong password",
	"CSRF token invalid",
}

// submit posts form to path on the miner and returns nil only when the
// miner responded with a redirect (303 + Location) — the unambiguous
// signal that the handler completed and moved the user to a new page.
//
// Heuristic for 200 OK: the miner re-renders the same template on
// failure (with an error flash appended), so a 200 here is NOT a
// success signal on its own. We peek at the body for known error
// markers (see submitErrorMarkers) and surface the matching marker as
// the error message. If a 200 carries no known marker we still treat
// it as success — better than blocking legitimate flows when the
// server happens to land on a 200 path we haven't catalogued yet.
func (c *minerClient) submit(ctx context.Context, path string, form url.Values) error {
	body, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	tok, err := extractCSRF(body)
	if err != nil {
		return fmt.Errorf("extract csrf: %w", err)
	}
	form.Set("csrf_token", tok)
	resp, err := c.postRaw(ctx, path, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusSeeOther && resp.Header.Get("Location") != "":
		// Real success — server redirected us to a new page.
		return nil
	case resp.StatusCode == http.StatusOK:
		raw, _ := io.ReadAll(resp.Body)
		bodyStr := string(raw)
		for _, marker := range submitErrorMarkers {
			if strings.Contains(bodyStr, marker) {
				return fmt.Errorf("POST %s: %s", path, marker)
			}
		}
		return nil
	case resp.StatusCode == http.StatusSeeOther:
		// 303 with no Location header — treat as a malformed response.
		return fmt.Errorf("POST %s: 303 with no Location header", path)
	default:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %s — %s", path, resp.Status, truncate(string(raw), 200))
	}
}

func (c *minerClient) get(ctx context.Context, path string) (string, error) {
	u := *c.base
	u.Path = path
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	dlog("GET %s", u.String())
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	dlog("← %s (%d bytes)", resp.Status, resp.ContentLength)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	return string(raw), err
}

func (c *minerClient) postRaw(ctx context.Context, path string, form url.Values) (*http.Response, error) {
	u := *c.base
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// nosurf rejects TLS POSTs that lack a same-origin Origin/Referer.
	// Sec-Fetch-Site: same-origin short-circuits the whole check, so
	// set it AND Origin AND Referer for belt-and-braces.
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", c.base.Scheme+"://"+c.base.Host)
	req.Header.Set("Referer", u.String())
	if debug() {
		dlog("POST %s (origin=%s referer=%s)", u.String(), req.Header.Get("Origin"), req.Header.Get("Referer"))
		for _, ck := range c.http.Jar.Cookies(&u) {
			dlog("  cookie: %s=%s...", ck.Name, truncate(ck.Value, 16))
		}
		dlog("  form: %s", form.Encode())
	}
	prev := c.http.CheckRedirect
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { c.http.CheckRedirect = prev }()
	resp, err := c.http.Do(req)
	if err == nil {
		dlog("← %s", resp.Status)
	}
	return resp, err
}

var csrfRE = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)

func extractCSRF(body string) (string, error) {
	m := csrfRE.FindStringSubmatch(body)
	if len(m) < 2 {
		return "", fmt.Errorf("csrf_token form field not found")
	}
	// Go html/template auto-escapes input value="..." attributes —
	// "+" becomes "&#43;", "=" stays, etc. Unescape so we post the
	// raw token nosurf actually generated.
	return html.UnescapeString(m[1]), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
