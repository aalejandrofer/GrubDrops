package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
)

// oauthScopes mirrors DevilXD's pinned scope set.
const oauthScopes = "channel_read chat:read user_blocks_edit user_blocks_read user_follows_edit user_read"

var ErrAuthorizationPending = errors.New("authorization_pending")

type authFlow struct {
	deviceURL string
	tokenURL  string
	http      *http.Client
}

func newAuthFlow() *authFlow {
	return &authFlow{
		deviceURL: deviceAuthURL,
		tokenURL:  tokenURL,
		http:      &http.Client{Timeout: 20 * time.Second},
	}
}

type deviceInternal struct {
	DeviceCode string
}

func (a *authFlow) start(ctx context.Context) (platform.DeviceChallenge, error) {
	form := url.Values{
		"client_id": {clientID},
		"scopes":    {oauthScopes},
	}
	resp, err := a.postForm(ctx, a.deviceURL, form)
	if err != nil {
		return platform.DeviceChallenge{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return platform.DeviceChallenge{}, fmt.Errorf("device authorize: %s", resp.Status)
	}

	var body struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return platform.DeviceChallenge{}, err
	}
	return platform.DeviceChallenge{
		UserCode:        body.UserCode,
		VerificationURL: body.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
		Interval:        time.Duration(body.Interval) * time.Second,
		Internal:        deviceInternal{DeviceCode: body.DeviceCode},
	}, nil
}

func (a *authFlow) poll(ctx context.Context, internal deviceInternal) (platform.Session, error) {
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {internal.DeviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	resp, err := a.postForm(ctx, a.tokenURL, form)
	if err != nil {
		return platform.Session{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if strings.Contains(string(raw), "authorization_pending") {
			return platform.Session{}, ErrAuthorizationPending
		}
		return platform.Session{}, fmt.Errorf("token poll: %s: %s", resp.Status, string(raw))
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return platform.Session{}, err
	}
	return platform.Session{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    computeExpiresAt(body.ExpiresIn),
	}, nil
}

// Twitch's web client_id (kimne78kx3ncx6brgo4mv6wki5h1ko) returns
// expires_in=0 for device-code tokens, signalling "no expiry" rather
// than literally zero. Treat <= 0 as "long-lived" and set a 60-day
// horizon so the scheduler doesn't immediately mark the session
// expired on the next boot.
func computeExpiresAt(expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Now().Add(60 * 24 * time.Hour)
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
}

func (a *authFlow) refresh(ctx context.Context, s platform.Session) (platform.Session, error) {
	if s.RefreshToken == "" {
		return platform.Session{}, errors.New("no refresh token")
	}
	form := url.Values{
		"client_id":     {clientID},
		"refresh_token": {s.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	resp, err := a.postForm(ctx, a.tokenURL, form)
	if err != nil {
		return platform.Session{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return platform.Session{}, fmt.Errorf("refresh: %s", resp.Status)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return platform.Session{}, err
	}
	if body.RefreshToken == "" {
		body.RefreshToken = s.RefreshToken
	}
	return platform.Session{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    computeExpiresAt(body.ExpiresIn),
	}, nil
}

func (a *authFlow) postForm(ctx context.Context, target string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	return a.http.Do(req)
}
