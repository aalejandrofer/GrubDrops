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
	"strings"
	"sync"
	"time"
)

// client is the low-level HTTP client. It handles header injection,
// GraphQL persisted-query envelope marshaling, and response unmarshaling.
type client struct {
	endpoint     string
	integrityURL string
	http         *http.Client

	// Per-process pseudo-browser identity. Twitch's anti-bot integrity
	// check binds the issued integrity token to the X-Device-Id sent
	// when requesting it; the same value must accompany every /gql
	// call. Client-Session-Id is similar but session-scoped.
	deviceID  string
	sessionID string

	intMu     sync.Mutex
	intToken  string
	intExpiry time.Time
}

func newClient() *client {
	return &client{
		endpoint:     gqlEndpoint,
		integrityURL: integrityURL,
		http:         &http.Client{Timeout: 20 * time.Second},
		deviceID:     randomHex(16),
		sessionID:    randomHex(16),
	}
}

func newTestClient(endpoint string) *client {
	return &client{
		endpoint:     endpoint,
		integrityURL: "", // tests do not exercise the integrity path
		http:         &http.Client{Timeout: 5 * time.Second},
		deviceID:     randomHex(16),
		sessionID:    randomHex(16),
	}
}

func randomHex(nBytes int) string {
	buf := make([]byte, nBytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// integrity fetches (and caches) a Client-Integrity token. Twitch
// rejects /gql calls that lack one with `failed integrity check`.
//
// The token is bound to the headers sent on the integrity request
// (Client-Id, X-Device-Id, Client-Session-Id, User-Agent). Subsequent
// /gql requests must echo the same X-Device-Id and Client-Session-Id
// or Twitch will invalidate the token.
//
// Token TTL is ~24h in practice; we refresh 5 minutes before expiry.
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
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", c.deviceID)
	req.Header.Set("Client-Session-Id", c.sessionID)
	if token != "" {
		req.Header.Set("Authorization", "OAuth "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("integrity: %s", resp.Status)
	}
	var body struct {
		Token      string `json:"token"`
		Expiration int64  `json:"expiration"` // epoch milliseconds
		IsBadBot   bool   `json:"is_bad_bot"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode integrity: %w", err)
	}
	if body.Token == "" {
		return "", fmt.Errorf("integrity: empty token (is_bad_bot=%v)", body.IsBadBot)
	}
	c.intToken = body.Token
	c.intExpiry = time.UnixMilli(body.Expiration)
	slog.Info("twitch integrity token obtained", "expiration", c.intExpiry, "is_bad_bot", body.IsBadBot)
	return c.intToken, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", c.deviceID)
	req.Header.Set("Client-Session-Id", c.sessionID)
	if token != "" {
		req.Header.Set("Authorization", "OAuth "+token)
	}
	if integrityToken, err := c.integrity(ctx, token); err == nil && integrityToken != "" {
		req.Header.Set("Client-Integrity", integrityToken)
	} else if err != nil {
		slog.Warn("twitch integrity fetch failed; sending request anyway", "op", opName, "err", err)
	}

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
