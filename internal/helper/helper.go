// Package helper is the shared core used by both the dropsminer-helper
// CLI and the dropsminer-helper-gui binaries. It reads cookies from the
// user's local browser via browserutils/kooky and uploads them to a
// running rust-drops-miner deployment.
package helper

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register cookie store finders
)

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

func readDomainCookies(ctx context.Context, domain, browser string) ([]*kooky.Cookie, error) {
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
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %s — %s", path, resp.Status, truncate(string(raw), 200))
	}
	return nil
}

func (c *minerClient) get(ctx context.Context, path string) (string, error) {
	u := *c.base
	u.Path = path
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := c.http.Do(req)
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

func (c *minerClient) postRaw(ctx context.Context, path string, form url.Values) (*http.Response, error) {
	u := *c.base
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	prev := c.http.CheckRedirect
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { c.http.CheckRedirect = prev }()
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
