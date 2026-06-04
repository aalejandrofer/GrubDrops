package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// client is the low-level HTTP client. It handles header injection,
// GraphQL persisted-query envelope marshaling, and response unmarshaling.
type client struct {
	endpoint string
	http     *http.Client
}

func newClient() *client {
	return &client{
		endpoint: gqlEndpoint,
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

func newTestClient(endpoint string) *client {
	return &client{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 5 * time.Second},
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "OAuth "+token)
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
