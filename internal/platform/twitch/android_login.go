package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Android paste login. Twitch removed device-code login for the Android
// app client (DevilXD #1165), but the implicit grant still works: the user
// signs in at AndroidLoginURL in their own browser, Twitch redirects the tab
// to https://www.twitch.tv/#access_token=..., and the user pastes that
// address back. Android tokens can read ViewerDropsDashboard and
// DropCampaignDetails, which return null for TV tokens.

var (
	// ErrNoAccessToken: the pasted text holds no access token.
	ErrNoAccessToken = errors.New("no access token found")
	// ErrWrongClient: the token is valid but was not issued to the Android client.
	ErrWrongClient = errors.New("token was not issued to the Twitch Android app")
	// ErrTokenInvalid: Twitch rejected the token (401 from validate).
	ErrTokenInvalid = errors.New("twitch token invalid or expired")
)

// AndroidLoginURL is the Twitch login page that, after sign-in, redirects to
// https://www.twitch.tv/#access_token=<token> under the Android app client.
func AndroidLoginURL() string {
	params := "client_id=" + profileAndroid.ID +
		"&redirect_uri=" + url.QueryEscape("https://www.twitch.tv/") +
		"&response_type=token"
	return "https://www.twitch.tv/login?client_id=" + profileAndroid.ID +
		"&redirect_params=" + url.QueryEscape(params)
}

var (
	fragmentTokenRe = regexp.MustCompile(`(?:^|[#&?])access_token=(\w+)`)
	bareTokenRe     = regexp.MustCompile(`^\w{20,64}$`)
)

// ExtractAccessToken pulls the access token out of the pasted redirect URL,
// or accepts a bare token. The error never contains the input.
func ExtractAccessToken(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if m := fragmentTokenRe.FindStringSubmatch(s); m != nil {
		return m[1], nil
	}
	if bareTokenRe.MatchString(s) {
		return s, nil
	}
	return "", ErrNoAccessToken
}

// TokenInfo is the id.twitch.tv/oauth2/validate response.
type TokenInfo struct {
	ClientID  string   `json:"client_id"`
	Login     string   `json:"login"`
	UserID    string   `json:"user_id"`
	Scopes    []string `json:"scopes"`
	ExpiresIn int      `json:"expires_in"`
}

// validate checks token against id.twitch.tv. A 401 maps to ErrTokenInvalid.
// Errors never include the token.
func (a *authFlow) validate(ctx context.Context, token string) (TokenInfo, error) {
	target := a.validateURL
	if target == "" {
		target = validateURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return TokenInfo{}, err
	}
	req.Header.Set("Authorization", "OAuth "+token)
	req.Header.Set("User-Agent", profileAndroid.UserAgent)
	resp, err := a.http.Do(req)
	if err != nil {
		return TokenInfo{}, fmt.Errorf("validate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return TokenInfo{}, ErrTokenInvalid
	}
	if resp.StatusCode != http.StatusOK {
		return TokenInfo{}, fmt.Errorf("validate: %s", resp.Status)
	}
	var info TokenInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info); err != nil {
		return TokenInfo{}, fmt.Errorf("validate: decode: %w", err)
	}
	return info, nil
}

// androidLogin extracts and validates a pasted token and builds an Android
// session from it. The token must belong to the Android app client.
func (a *authFlow) androidLogin(ctx context.Context, raw string) (platform.Session, TokenInfo, error) {
	token, err := ExtractAccessToken(raw)
	if err != nil {
		return platform.Session{}, TokenInfo{}, err
	}
	info, err := a.validate(ctx, token)
	if err != nil {
		return platform.Session{}, TokenInfo{}, err
	}
	if info.ClientID != profileAndroid.ID {
		return platform.Session{}, TokenInfo{}, ErrWrongClient
	}
	if info.UserID == "" {
		return platform.Session{}, TokenInfo{}, errors.New("validate: no user_id")
	}
	return platform.Session{
		AccessToken: token,
		ExpiresAt:   computeExpiresAt(info.ExpiresIn),
		Cookies:     SynthCookieBlob(token),
		ClientID:    ClientAndroid,
		UserID:      info.UserID,
	}, info, nil
}

// revalidate stands in for refresh on Android sessions, which have no
// refresh token: a live token gets a new expiry, a dead one ErrTokenInvalid.
func (a *authFlow) revalidate(ctx context.Context, s platform.Session) (platform.Session, error) {
	info, err := a.validate(ctx, s.AccessToken)
	if err != nil {
		return platform.Session{}, err
	}
	if info.ClientID != profileAndroid.ID {
		return platform.Session{}, ErrWrongClient
	}
	next := s
	next.ExpiresAt = computeExpiresAt(info.ExpiresIn)
	if next.UserID == "" {
		next.UserID = info.UserID
	}
	return next, nil
}

// AndroidLogin turns a pasted redirect URL or bare token into an Android
// session. Returns the user's login name for the account label.
func (b *Backend) AndroidLogin(ctx context.Context, raw string) (platform.Session, string, error) {
	s, info, err := b.auth.androidLogin(ctx, raw)
	return s, info.Login, err
}

// AndroidLogin turns a pasted redirect URL or bare token into an Android
// session. Returns the user's login name for the account label.
func (b *BrowserBackend) AndroidLogin(ctx context.Context, raw string) (platform.Session, string, error) {
	s, info, err := b.authFlow.androidLogin(ctx, raw)
	return s, info.Login, err
}
