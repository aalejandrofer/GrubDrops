// Package helper is the shared core used by both the grubdrops-helper
// CLI and the grubdrops-helper-gui binaries. It reads cookies from the
// user's local browser via browserutils/kooky and uploads them to a
// running grubdrops deployment.
package helper

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register cookie store finders
)

// debug returns true when GRUB_HELPER_DEBUG=1 — prints every miner
// HTTP request + response and lists which browser stores yielded
// cookies.
func debug() bool { return os.Getenv("GRUB_HELPER_DEBUG") == "1" }

func dlog(format string, args ...any) {
	if debug() {
		log.Printf("helper: "+format, args...)
	}
}

// DefaultMinerURL is the production deployment. The helper is handed to
// friends to authorize their own accounts against prod, so this — not
// localhost — is the sensible default.
const DefaultMinerURL = "https://drops.ryuzec.dev"

// Config carries the connection details shared by every push. There is no
// password: the unguessable acc_<24hex> account ID in the upload path is the
// only credential the no-auth /helper endpoint requires.
type Config struct {
	MinerURL string // e.g. https://drops.ryuzec.dev
	Browser  string // optional — limit cookie scan to a specific browser
	Insecure bool   // skip TLS verify (debug)
}

// KickRequest pushes the local kick.com cookies + the channel choice.
// Channel is kept for back-compat (single channel); Channels carries
// the multi-channel list. If both are set, Channels wins.
type KickRequest struct {
	Config
	AccountID string
	Channel   string   // single-channel legacy field
	Channels  []string // multi-channel list (preferred)
}

// Result is what every PushXxx returns. Echoed back to the user.
type Result struct {
	UploadedCookies []string // names of cookies that were sent
	Message         string   // human-readable summary
}

// PushKick reads kick.com cookies and POSTs them to the no-auth helper
// endpoint /helper/accounts/<id>/kick. No password; the account ID is the
// credential. Channels are optional — they auto-discover from each campaign's
// game, so an empty list is fine.
func PushKick(ctx context.Context, req KickRequest) (Result, error) {
	if req.AccountID == "" {
		return Result{}, fmt.Errorf("account-id is required")
	}
	channels := req.Channels
	if len(channels) == 0 && req.Channel != "" {
		channels = []string{req.Channel}
	}
	cookies, err := readDomainCookies(ctx, "kick.com", req.Browser)
	if err != nil {
		return Result{}, fmt.Errorf("read browser cookies: %w", err)
	}
	picked := pickCookies(cookies, []string{"kick_session", "XSRF-TOKEN", "cf_clearance", "session_token"})
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

	form := url.Values{}
	form.Set("kick_session", picked["kick_session"])
	form.Set("xsrf_token", picked["XSRF-TOKEN"])
	if v, ok := picked["cf_clearance"]; ok {
		form.Set("cf_clearance", v)
	}
	// session_token carries the Sanctum bearer the utls transport needs for
	// authed drops calls (progress/claim). Optional — older sessions worked
	// without it for discovery, but claims need it.
	if v, ok := picked["session_token"]; ok {
		form.Set("session_token", v)
	}
	// Server-side parseKickChannels accepts comma/space-separated input;
	// empty is fine (channels auto-discover).
	if len(channels) > 0 {
		form.Set("channel", strings.Join(channels, ","))
	}

	if err := client.postForm(ctx, "/helper/accounts/"+req.AccountID+"/kick", form); err != nil {
		return Result{}, err
	}
	names := []string{"kick_session", "XSRF-TOKEN"}
	if _, ok := picked["cf_clearance"]; ok {
		names = append(names, "cf_clearance")
	}
	msg := fmt.Sprintf("uploaded kick cookies for account %s", req.AccountID)
	if len(channels) > 0 {
		msg += fmt.Sprintf(" (channels %s)", strings.Join(channels, ", "))
	} else {
		msg += " (channels auto-discover)"
	}
	return Result{UploadedCookies: names, Message: msg}, nil
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
	// kooky can't find some Chromium-based browsers (notably Opera GX, which it
	// only knows as "Opera Stable"). Scan their cookie DBs explicitly and merge
	// — the on-disk format is identical to Chrome's, and kooky's chrome reader
	// auto-locates each profile's Local State for decryption.
	cookies = append(cookies, readExtraChromiumCookies(ctx, domain, browser)...)
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

// postForm POSTs form to path on the miner and returns nil on any 2xx.
// The /helper endpoints are no-auth and CSRF-exempt — keyed only by the
// account ID in the path — so there's no login or CSRF dance. A 404 means
// the account ID is wrong; surface that clearly.
func (c *minerClient) postForm(ctx context.Context, path string, form url.Values) error {
	resp, err := c.postRaw(ctx, path, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("account not found on the miner — double-check the Account ID (it should look like acc_…)")
	}
	return fmt.Errorf("POST %s: %s — %s", path, resp.Status, truncate(string(raw), 200))
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
