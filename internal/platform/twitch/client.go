package twitch

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// client is the low-level HTTP client. It handles header injection,
// GraphQL persisted-query envelope marshaling, and response unmarshaling.
//
// Header set matches DevilXD/TwitchDropsMiner master @ 2026-06-04
// (twitch.py::headers). Notably, Client-Integrity is NOT sent — the
// upstream miner gets through Twitch's anti-bot checks with just the
// device/session pair plus an Origin/Referer that match the web client.
type client struct {
	endpoint string
	homeURL  string
	http     *http.Client

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
}

func newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{
		endpoint:  gqlEndpoint,
		homeURL:   "https://www.twitch.tv",
		http:      &http.Client{Timeout: 20 * time.Second, Jar: jar},
		deviceID:  randomHex(16), // overwritten by bootstrap if Twitch sends unique_id
		sessionID: randomHex(16),
	}
}

func newTestClient(endpoint string) *client {
	return &client{
		endpoint:    endpoint,
		homeURL:     "",
		http:        &http.Client{Timeout: 5 * time.Second},
		deviceID:    randomHex(16),
		sessionID:   randomHex(16),
		idBootstrap: true, // skip bootstrap in tests
	}
}

func randomHex(nBytes int) string {
	buf := make([]byte, nBytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// bootstrapIdentity fetches https://www.twitch.tv once so Twitch can
// set its `unique_id` cookie; we then use that value as X-Device-Id
// on every subsequent /gql call. Mirrors DevilXD's `_validate` flow.
func (c *client) bootstrapIdentity(ctx context.Context) {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	if c.idBootstrap || c.homeURL == "" {
		return
	}
	c.idBootstrap = true

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.homeURL, nil)
	if err != nil {
		slog.Warn("twitch identity bootstrap: build request", "err", err)
		return
	}
	c.setCommonHeaders(req, "")
	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("twitch identity bootstrap: GET twitch.tv", "err", err)
		return
	}
	_ = resp.Body.Close()

	u, _ := url.Parse(c.homeURL)
	if c.http.Jar != nil {
		for _, ck := range c.http.Jar.Cookies(u) {
			if ck.Name == "unique_id" && ck.Value != "" {
				c.deviceID = ck.Value
				slog.Info("twitch device-id bootstrapped from cookie", "device_id", ck.Value)
				return
			}
		}
	}
	slog.Warn("twitch identity bootstrap: unique_id cookie not present, using random device-id")
}

// setCommonHeaders mirrors DevilXD's AuthState.headers(gql=True).
func (c *client) setCommonHeaders(req *http.Request, oauthToken string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Client-Session-Id", c.sessionID)
	req.Header.Set("X-Device-Id", c.deviceID)
	req.Header.Set("Origin", "https://www.twitch.tv")
	req.Header.Set("Referer", "https://www.twitch.tv/")
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
	c.bootstrapIdentity(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setCommonHeaders(req, token)
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("twitch gql %s: %s", opName, resp.Status)
	}

	var envelope gqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode gql response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("twitch gql %s: %s", opName, strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}
