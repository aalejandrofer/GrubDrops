package twitch

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// gqlTransport sends a POST body to gql.twitch.tv/gql and returns the
// raw response body + HTTP status. Two implementations:
//   - direct via net/http (default; subject to Twitch's anti-bot
//     integrity wall)
//   - via the chromedp sidecar tab (browser-routed; bypasses integrity)
type gqlTransport interface {
	gqlPost(ctx context.Context, token, opName string, body []byte) ([]byte, int, error)
}

// client is the GraphQL client. It builds gql request envelopes and
// hands them to a gqlTransport, then unmarshals the response.
type client struct {
	endpoint     string
	homeURL      string
	integrityURL string
	http         *http.Client
	transport    gqlTransport

	// Per-process pseudo-browser identity.
	//   deviceID  comes from the `unique_id` cookie Twitch sets when
	//             we GET https://www.twitch.tv; we bootstrap it lazily
	//             on first use. Falls back to random 32-hex if Twitch
	//             never sets the cookie.
	//   sessionID is a random 16-hex string created at startup.
	deviceID  string
	sessionID string

	idMu        sync.Mutex
	idBootstrap bool

	intMu     sync.Mutex
	intToken  string
	intExpiry time.Time
}

func newClient() *client {
	jar, _ := cookiejar.New(nil)
	c := &client{
		endpoint:     gqlEndpoint,
		homeURL:      "https://www.twitch.tv",
		integrityURL: "https://gql.twitch.tv/integrity",
		http:         &http.Client{Timeout: 20 * time.Second, Jar: jar},
		deviceID:     randomHex(16),
		sessionID:    randomHex(16),
	}
	c.transport = httpTransport{c: c}
	return c
}

func newTestClient(endpoint string) *client {
	c := &client{
		endpoint:     endpoint,
		homeURL:      "",
		integrityURL: "",
		http:         &http.Client{Timeout: 5 * time.Second},
		deviceID:     randomHex(16),
		sessionID:    randomHex(16),
		idBootstrap:  true,
	}
	c.transport = httpTransport{c: c}
	return c
}

// newBrowserClient builds a client whose gql calls go through the
// chromedp sidecar tab keyed on accountID. Used by NewBrowserBackend.
func newBrowserClient(send TwitchGQLSender, accountID string) *client {
	c := &client{
		endpoint:    gqlEndpoint,
		http:        &http.Client{Timeout: 20 * time.Second},
		deviceID:    randomHex(16),
		sessionID:   randomHex(16),
		idBootstrap: true,
	}
	c.transport = browserTransport{send: send, accountID: accountID}
	return c
}

// TwitchGQLSender is the surface the browserTransport needs from the
// sidecar gRPC client. Implemented by *browser.Client.
type TwitchGQLSender interface {
	TwitchGQL(ctx context.Context, accountID, opName string, body []byte) ([]byte, int, error)
}

type httpTransport struct{ c *client }

func (t httpTransport) gqlPost(ctx context.Context, token, opName string, body []byte) ([]byte, int, error) {
	return t.c.directPost(ctx, token, opName, body)
}

type browserTransport struct {
	send      TwitchGQLSender
	accountID string
}

func (t browserTransport) gqlPost(ctx context.Context, _ /*token, ignored*/, opName string, body []byte) ([]byte, int, error) {
	return t.send.TwitchGQL(ctx, t.accountID, opName, body)
}

func randomHex(nBytes int) string {
	buf := make([]byte, nBytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// bootstrapIdentity is a no-op for the Android client profile —
// the upstream Twitch app sends a random device ID on first launch
// and never visits the web; we mirror that by keeping the random
// 32-hex deviceID created in newClient().
func (c *client) bootstrapIdentity(ctx context.Context) {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.idBootstrap = true
	_ = ctx
}

// integrity acquires Twitch's anti-bot integrity token from the
// /integrity endpoint. The token must be sent as Client-Integrity on
// subsequent /gql calls or Twitch returns "failed integrity check"
// for any drops/inventory field (auth still works — only the gated
// fields are nulled out).
//
// Token TTL ~= 24h. Bound to the (Client-Id, X-Device-Id,
// Client-Session-Id, User-Agent, auth-token cookie) tuple sent on the
// integrity request, so the same identifiers must accompany every
// /gql request that uses the token.
func (c *client) integrity(ctx context.Context, token string) (string, error) {
	if c.integrityURL == "" {
		return "", nil
	}
	c.intMu.Lock()
	defer c.intMu.Unlock()
	if c.intToken != "" && time.Now().Before(c.intExpiry.Add(-5*time.Minute)) {
		return c.intToken, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.integrityURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	c.setCommonHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		slog.Error("twitch integrity non-200", "status", resp.Status, "body", truncate(string(raw), 500))
		return "", fmt.Errorf("integrity: %s", resp.Status)
	}
	var body struct {
		Token      string `json:"token"`
		Expiration int64  `json:"expiration"` // epoch milliseconds
		IsBadBot   bool   `json:"is_bad_bot"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", fmt.Errorf("decode integrity: %w", err)
	}
	if body.Token == "" {
		return "", fmt.Errorf("integrity: empty token (is_bad_bot=%v, body=%q)", body.IsBadBot, truncate(string(raw), 200))
	}
	c.intToken = body.Token
	c.intExpiry = time.UnixMilli(body.Expiration)
	slog.Info("twitch integrity token acquired", "expiration", c.intExpiry, "is_bad_bot", body.IsBadBot, "token_prefix", truncate(body.Token, 30), "raw", truncate(string(raw), 500))
	return c.intToken, nil
}

// setCommonHeaders mirrors DevilXD's AuthState.headers(gql=True) for
// the Android client profile. Android apps don't send Origin/Referer
// so we omit them.
func (c *client) setCommonHeaders(req *http.Request, oauthToken string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Client-Session-Id", c.sessionID)
	req.Header.Set("X-Device-Id", c.deviceID)
	if oauthToken != "" {
		req.Header.Set("Authorization", "OAuth "+oauthToken)
	}
}

// gql sends a persisted GraphQL operation and decodes the `data` field
// into `out`. token may be empty for unauthenticated calls.
func (c *client) gql(ctx context.Context, token string, op Operation, variables map[string]any, out any) error {
	body, err := json.Marshal(gqlRequest{
		OperationName: op.Name,
		Variables:     variables,
		Extensions: gqlExtensions{
			PersistedQuery: gqlPersistedQuery{Version: 1, Sha256Hash: op.Hash},
		},
	})
	if err != nil {
		return err
	}
	return c.do(ctx, token, op.Name, body, out)
}

// gqlQuery sends a non-persisted GraphQL operation (full query body
// inline). Used by mutations like SendEvents where Twitch does not
// publish a stable persisted-query hash.
func (c *client) gqlQuery(ctx context.Context, token, operationName, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{
		"operationName": operationName,
		"query":         query,
		"variables":     variables,
	})
	if err != nil {
		return err
	}
	return c.do(ctx, token, operationName, body, out)
}

func (c *client) do(ctx context.Context, token, opName string, body []byte, out any) error {
	rawBody, status, err := c.transport.gqlPost(ctx, token, opName, body)
	if err != nil {
		return err
	}
	if status >= 500 {
		slog.Error("twitch gql 5xx", "op", opName, "status", status, "body", truncate(string(rawBody), 500))
		return fmt.Errorf("twitch gql %s: status %d", opName, status)
	}

	var envelope gqlResponse
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		slog.Error("twitch gql decode failed", "op", opName, "status", status, "body", truncate(string(rawBody), 500))
		return fmt.Errorf("decode gql response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		slog.Error("twitch gql application error", "op", opName, "status", status, "errors", msgs, "body", truncate(string(rawBody), 500))
		return fmt.Errorf("twitch gql %s: %s", opName, strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

// directPost is the original net/http path. Subject to Twitch's
// anti-bot integrity check; useful for local testing or environments
// without a sidecar.
func (c *client) directPost(ctx context.Context, token, opName string, body []byte) ([]byte, int, error) {
	c.bootstrapIdentity(ctx)

	if token != "" && c.http.Jar != nil && c.homeURL != "" {
		if u, err := url.Parse(c.homeURL); err == nil && u != nil {
			c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "auth-token", Value: token, Domain: "twitch.tv", Path: "/"}})
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	c.setCommonHeaders(req, token)
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	if intToken, err := c.integrity(ctx, token); err == nil && intToken != "" {
		req.Header.Set("Client-Integrity", intToken)
	} else if err != nil {
		slog.Warn("twitch integrity fetch failed, sending without", "op", opName, "err", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode, nil
}
