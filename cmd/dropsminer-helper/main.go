// dropsminer-helper is a small CLI that copies cookies from the
// user's local browser into the rust-drops-miner deployment. It
// eliminates the DevTools → Application → Cookies → paste dance for
// both Twitch and Kick accounts.
//
// Usage:
//
//	dropsminer-helper twitch <account-id> [flags]
//	dropsminer-helper kick   <account-id> [flags] --channel=<name>
//
// Flags:
//
//	--miner    URL    Base URL of the miner (default https://rdrops.ryuzec.dev)
//	--password STR    Admin password. Falls back to MINER_PASSWORD env.
//	--browser  NAME   Limit cookie search to a specific browser (chrome,
//	                  firefox, safari, etc). Default: try all installed.
//	--insecure        Skip TLS verification (debug only).
//
// The helper logs into the miner with the admin password, reads the
// platform's session cookies from the local browser via kooky, and
// POSTs them to the existing /accounts/:id/twitch/paste or
// /accounts/:id/login endpoint.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register all cookie store finders
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "twitch":
		err = runTwitch(args)
	case "kick":
		err = runKick(args)
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `dropsminer-helper — push browser cookies to a rust-drops-miner deployment

Usage:
  dropsminer-helper twitch <account-id> [--miner URL] [--password PW] [--browser NAME]
  dropsminer-helper kick   <account-id> [--miner URL] [--password PW] [--browser NAME] --channel NAME

Flags:
  --miner     base URL of the miner (default https://rdrops.ryuzec.dev)
  --password  admin password (or set MINER_PASSWORD)
  --browser   limit cookie search to this browser (chrome, firefox, safari, ...)
  --channel   kick channel to mine (kick only, required)
  --insecure  skip TLS verification

`)
}

type commonFlags struct {
	miner    string
	password string
	browser  string
	insecure bool
}

func parseCommon(fs *flag.FlagSet, args []string, extra func(*flag.FlagSet)) (commonFlags, []string, error) {
	cf := commonFlags{
		miner:    "https://rdrops.ryuzec.dev",
		password: os.Getenv("MINER_PASSWORD"),
	}
	fs.StringVar(&cf.miner, "miner", cf.miner, "base URL of the miner")
	fs.StringVar(&cf.password, "password", cf.password, "admin password")
	fs.StringVar(&cf.browser, "browser", "", "limit cookie search to this browser")
	fs.BoolVar(&cf.insecure, "insecure", false, "skip TLS verification")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return cf, nil, err
	}
	if cf.password == "" {
		return cf, nil, fmt.Errorf("missing --password (or MINER_PASSWORD env)")
	}
	return cf, fs.Args(), nil
}

func runTwitch(args []string) error {
	fs := flag.NewFlagSet("twitch", flag.ContinueOnError)
	cf, rest, err := parseCommon(fs, args, nil)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("twitch requires exactly one account-id argument")
	}
	accountID := rest[0]

	cookies, err := readDomainCookies("twitch.tv", cf.browser)
	if err != nil {
		return fmt.Errorf("read browser cookies: %w", err)
	}
	required := []string{"auth-token"}
	optional := []string{"persistent", "server_session_id", "twilight-user", "unique_id", "login", "name"}
	picked := pickCookies(cookies, append(required, optional...))
	if _, ok := picked["auth-token"]; !ok {
		return fmt.Errorf("auth-token cookie not found in any browser jar — are you logged into twitch.tv?")
	}

	c, err := newClient(cf)
	if err != nil {
		return err
	}
	if err := c.adminLogin(cf.password); err != nil {
		return fmt.Errorf("miner login: %w", err)
	}

	form := url.Values{}
	for k, v := range picked {
		form.Set(k, v)
	}
	if err := c.submit("/accounts/"+accountID+"/twitch/paste", form); err != nil {
		return err
	}
	fmt.Printf("✓ uploaded %d twitch cookies for account %s\n", len(picked), accountID)
	return nil
}

func runKick(args []string) error {
	fs := flag.NewFlagSet("kick", flag.ContinueOnError)
	var channel string
	cf, rest, err := parseCommon(fs, args, func(fs *flag.FlagSet) {
		fs.StringVar(&channel, "channel", "", "kick channel to mine (required)")
	})
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("kick requires exactly one account-id argument")
	}
	if channel == "" {
		return fmt.Errorf("kick requires --channel")
	}
	accountID := rest[0]

	cookies, err := readDomainCookies("kick.com", cf.browser)
	if err != nil {
		return fmt.Errorf("read browser cookies: %w", err)
	}
	// Kick's session cookie is `kick_session`, CSRF is `XSRF-TOKEN`.
	picked := pickCookies(cookies, []string{"kick_session", "XSRF-TOKEN", "cf_clearance"})
	if _, ok := picked["kick_session"]; !ok {
		return fmt.Errorf("kick_session cookie not found — are you logged into kick.com?")
	}
	if _, ok := picked["XSRF-TOKEN"]; !ok {
		return fmt.Errorf("XSRF-TOKEN cookie not found")
	}

	c, err := newClient(cf)
	if err != nil {
		return err
	}
	if err := c.adminLogin(cf.password); err != nil {
		return fmt.Errorf("miner login: %w", err)
	}

	form := url.Values{}
	form.Set("kick_session", picked["kick_session"])
	form.Set("xsrf_token", picked["XSRF-TOKEN"])
	if v, ok := picked["cf_clearance"]; ok {
		form.Set("cf_clearance", v)
	}
	form.Set("channel", channel)
	if err := c.submit("/accounts/"+accountID+"/login", form); err != nil {
		return err
	}
	fmt.Printf("✓ uploaded kick cookies for account %s (channel %s)\n", accountID, channel)
	return nil
}

// readDomainCookies returns all cookies whose Domain ends with the
// given suffix. browser filters by browser name when non-empty.
func readDomainCookies(domain, browser string) ([]*kooky.Cookie, error) {
	ctx := context.Background()
	cookies, err := kooky.ReadCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
	if err != nil {
		return nil, err
	}
	if browser == "" {
		return cookies, nil
	}
	out := cookies[:0]
	for _, c := range cookies {
		if c.Browser != nil && strings.EqualFold(c.Browser.Browser(), browser) {
			out = append(out, c)
		}
	}
	return out, nil
}

// pickCookies returns the latest non-expired value for each requested
// name. Where multiple browsers / profiles contain the same cookie,
// the newest Expires wins.
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

type minerClient struct {
	base *url.URL
	http *http.Client
}

func newClient(cf commonFlags) (*minerClient, error) {
	base, err := url.Parse(cf.miner)
	if err != nil {
		return nil, fmt.Errorf("parse miner URL: %w", err)
	}
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cf.insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 — opt-in debug flag
	}
	return &minerClient{
		base: base,
		http: &http.Client{Jar: jar, Transport: tr},
	}, nil
}

// adminLogin GETs /login to seed the csrf cookie + extract the form
// token, then POSTs /login with the password.
func (c *minerClient) adminLogin(password string) error {
	body, err := c.get("/login")
	if err != nil {
		return err
	}
	tok, err := extractCSRF(body)
	if err != nil {
		return fmt.Errorf("extract csrf: %w", err)
	}
	form := url.Values{"password": {password}, "csrf_token": {tok}}
	resp, err := c.postRaw("/login", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login responded %s", resp.Status)
	}
	// Detect a re-render of the login page (wrong password) — handler
	// returns 200 OK with "wrong password" flash instead of redirecting.
	if resp.StatusCode == http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(raw), "wrong password") {
			return fmt.Errorf("wrong password")
		}
	}
	return nil
}

// submit POSTs the supplied form to path after GETing it once to pick
// up a fresh csrf token. Follows the redirect on success.
func (c *minerClient) submit(path string, form url.Values) error {
	body, err := c.get(path)
	if err != nil {
		return err
	}
	tok, err := extractCSRF(body)
	if err != nil {
		return fmt.Errorf("extract csrf: %w", err)
	}
	form.Set("csrf_token", tok)
	resp, err := c.postRaw(path, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %s — %s", path, resp.Status, truncate(string(raw), 200))
	}
	return nil
}

func (c *minerClient) get(path string) (string, error) {
	u := *c.base
	u.Path = path
	resp, err := c.http.Get(u.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	return string(raw), err
}

func (c *minerClient) postRaw(path string, form url.Values) (*http.Response, error) {
	u := *c.base
	u.Path = path
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Prevent automatic redirect following so we can inspect 303s.
	prevCheck := c.http.CheckRedirect
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { c.http.CheckRedirect = prevCheck }()
	return c.http.Do(req)
}

var csrfRE = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)

func extractCSRF(html string) (string, error) {
	m := csrfRE.FindStringSubmatch(html)
	if len(m) < 2 {
		return "", fmt.Errorf("csrf_token form field not found")
	}
	return m[1], nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
