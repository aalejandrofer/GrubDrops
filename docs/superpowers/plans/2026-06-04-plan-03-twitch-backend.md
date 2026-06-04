# Plan 3: Real Twitch Backend

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `FakeBackend`'s `twitch` registration with a real implementation that talks to `gql.twitch.tv` using the web Client-ID + OAuth device-code flow, discovers drops campaigns, emits `MinuteWatched` heartbeats, and claims drops — matching the surface of `DevilXD/TwitchDropsMiner` (the canonical working reference).

**Architecture:** A new `internal/platform/twitch` package implementing the `Backend` interface. All GraphQL operations are captured as Go constants (extracted from the DevilXD repo). Sessions persist through the cryptor in the existing `sessions` table. The web GUI gains a Twitch-specific login flow that displays the device code and verification URL while polling the token endpoint. Tests use `httptest.Server` with golden-file responses so the test suite never hits live Twitch.

**Tech Stack:** Go stdlib `net/http` + `encoding/json`, existing `internal/platform` interface, existing `internal/store` cryptor for session blob persistence, existing HTMX templates + handlers extended for the device-code flow.

**Out of scope:**
- Real Kick backend (Plan 4)
- Browser sidecar (Plan 4)
- Per-account proxy routing (works via Backend config but no GUI in Plan 3 — env-driven for now)
- Production deploy (Plan 5)
- Persisted query hash rotation detection (hashes are pinned; Plan 6 adds a refresher)

---

## File Map

New files:

| File | Responsibility |
|---|---|
| `internal/platform/twitch/ops.go` | Persisted-query hashes + operation names + payload builders, mirrored from DevilXD source |
| `internal/platform/twitch/types.go` | Local Go types matching GraphQL response shapes |
| `internal/platform/twitch/client.go` | HTTP client with `Client-ID` + bearer header injection + retry + JSON marshaling |
| `internal/platform/twitch/auth.go` | Device-code flow: `StartDeviceLogin` + `PollDeviceLogin` + `RefreshSession` |
| `internal/platform/twitch/campaigns.go` | `ListActiveCampaigns`, `ListEligibleChannels`, `InventoryProgress` |
| `internal/platform/twitch/watch.go` | `StartWatch`, `Heartbeat` (MinuteWatched mutation), `StopWatch` |
| `internal/platform/twitch/claim.go` | `Claim` mutation |
| `internal/platform/twitch/backend.go` | The `Backend` interface impl that wires the above into a single struct |
| `internal/platform/twitch/backend_test.go` | Golden-file HTTP replay tests for the full lifecycle |
| `internal/platform/twitch/testdata/*.json` | Recorded GraphQL responses for the tests |
| `internal/api/handlers_login_twitch.go` | GET/POST `/accounts/:id/login` → renders device-code page + polls token |
| `internal/web/templates/login_twitch.html` | Device-code flow page (HTMX-polled status partial) |
| `internal/web/templates/login_twitch_status.html` | Partial polled every 2s while waiting for the user to approve |
| `internal/store/queries/sessions_extras.sql` | `DeleteSession`, `ListSessionAccountIDs` (housekeeping) |

Modified:

| File | Change |
|---|---|
| `cmd/miner/main.go` | Register `twitch.New(cryptor, q)` next to `fake.New`. Boot path now loads any persisted Twitch session for each enabled account via the cryptor and hands it to the watcher; missing/expired sessions pause the account into `AuthRequired` until the GUI re-runs the device flow. |
| `internal/web/templates/accounts_new.html` | Add `<option value="twitch">Twitch (drops)</option>` to platform select |
| `internal/api/handlers_accounts.go` | After `newPost` creates a Twitch account, redirect to `/accounts/:id/login` instead of `/accounts` |
| `internal/api/server.go` | Mount `/accounts/{id}/login` GET + `/accounts/{id}/login/poll` GET |
| `internal/scheduler/control.go` | No code change but Reload behavior is exercised end-to-end (paused → authed flips state) |

---

## Task 1: Research + capture Twitch GraphQL operations

We pin the persisted-query SHA-256 hashes from DevilXD's source so the implementation matches a known-working client.

**Files:**
- Create: `internal/platform/twitch/ops.go`
- Create: `docs/superpowers/notes/2026-06-04-twitch-ops-source.md` (research log)

- [ ] **Step 1: Inspect DevilXD/TwitchDropsMiner for current operation hashes**

The implementer subagent must use `WebFetch` against the following raw GitHub URLs (substituting the default branch — usually `master`):

```
https://raw.githubusercontent.com/DevilXD/TwitchDropsMiner/master/constants.py
https://raw.githubusercontent.com/DevilXD/TwitchDropsMiner/master/gui.py
```

If `master` returns 404 try `main`. Look specifically for a `GQL_OPERATIONS` dict (or similarly-named const) mapping operation name → `{operationName, sha256Hash}`. Capture the following operations into the research note:

- `ClaimDrop`
- `ChannelPointsContext`
- `Inventory`
- `CurrentDrop`
- `DropCampaignDetails`
- `DropsHighlightService_AvailableDrops`
- `DropsPage_ContentList` (campaigns landing page)
- `PlaybackAccessToken_Template`
- `WithIsStreamLiveQuery` (or whichever query yields live-channel info per campaign)
- `MinuteWatchedTrackingChannel` — note: in DevilXD this is sent to `https://countess.twitch.tv/site.js` not the GraphQL endpoint; the heartbeat path is REST-ish. Capture the URL pattern.

Also capture the constants:

- `CLIENT_ID` (web client ID, usually `kimne78kx3ncx6brgo4mv6wki5h1ko`)
- `USER_AGENT` (a recent Chrome desktop UA string is fine; use whatever DevilXD pins)
- GraphQL endpoint: `https://gql.twitch.tv/gql`
- OAuth device endpoint: `https://id.twitch.tv/oauth2/device`
- OAuth token endpoint: `https://id.twitch.tv/oauth2/token`

Write the captured data into `docs/superpowers/notes/2026-06-04-twitch-ops-source.md` like:

```markdown
# Twitch operations source snapshot

Captured 2026-06-04 from DevilXD/TwitchDropsMiner@<commit-sha>.

## Constants

- CLIENT_ID: `<value>`
- USER_AGENT: `<value>`
- GraphQL: `https://gql.twitch.tv/gql`
- Device authorize: `https://id.twitch.tv/oauth2/device`
- Token: `https://id.twitch.tv/oauth2/token`

## Persisted query hashes

| Operation                    | sha256 |
|------------------------------|--------|
| ClaimDrop                    | `…`    |
| Inventory                    | `…`    |
| DropCampaignDetails          | `…`    |
| DropsPage_ContentList        | `…`    |
| PlaybackAccessToken_Template | `…`    |
| WithIsStreamLiveQuery        | `…`    |
…

## Minute-watched heartbeat

URL: `<exact url>` (note params)
Body: <description of expected POST body>
```

- [ ] **Step 2: Translate the captured data into Go constants**

Create `internal/platform/twitch/ops.go`:

```go
package twitch

// Captured 2026-06-04 from DevilXD/TwitchDropsMiner. If Twitch rotates a
// persisted query, the matching response will be {"errors":[{"message":"PersistedQueryNotFound"}]}
// and these hashes must be refreshed from the upstream source.
const (
	clientID  = "<paste from research note>"
	userAgent = "<paste from research note>"

	gqlEndpoint      = "https://gql.twitch.tv/gql"
	deviceAuthURL    = "https://id.twitch.tv/oauth2/device"
	tokenURL         = "https://id.twitch.tv/oauth2/token"
	minuteWatchedURL = "<paste from research note>" // e.g. https://countess.twitch.tv/site.js
)

// Operation identifies a persisted GraphQL operation.
type Operation struct {
	Name string
	Hash string
}

var (
	OpInventory             = Operation{Name: "Inventory", Hash: "<paste>"}
	OpClaimDrop             = Operation{Name: "ClaimDrop", Hash: "<paste>"}
	OpCampaigns  = Operation{Name: "DropsPage_ContentList", Hash: "<paste>"}
	OpDropCampaignDetails   = Operation{Name: "DropCampaignDetails", Hash: "<paste>"}
	OpPlaybackAccessToken   = Operation{Name: "PlaybackAccessToken_Template", Hash: "<paste>"}
	OpGetStreamInfo = Operation{Name: "WithIsStreamLiveQuery", Hash: "<paste>"}
)
```

After pasting real values, every `<paste from research note>` must be a concrete string. If you cannot retrieve a value, stop and report BLOCKED instead of guessing.

- [ ] **Step 3: Verify build**

```bash
go build ./internal/platform/twitch/...
```

Clean (no tests yet).

- [ ] **Step 4: Commit**

```bash
git add internal/platform/twitch/ops.go docs/superpowers/notes/
git commit -m "$(cat <<'EOF'
feat(platform/twitch): pin GraphQL operation hashes from DevilXD reference

Captured from DevilXD/TwitchDropsMiner. Refresh whenever Twitch rotates
persisted-query hashes (signaled by PersistedQueryNotFound errors in
production logs).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: HTTP client with header injection and retry

**Files:**
- Create: `internal/platform/twitch/client.go`
- Create: `internal/platform/twitch/types.go`
- Test: `internal/platform/twitch/client_test.go`

- [ ] **Step 1: Define response shapes**

Create `internal/platform/twitch/types.go`:

```go
package twitch

import "encoding/json"

// gqlRequest is the JSON sent to https://gql.twitch.tv/gql.
type gqlRequest struct {
	OperationName string          `json:"operationName"`
	Variables     map[string]any  `json:"variables,omitempty"`
	Extensions    gqlExtensions   `json:"extensions"`
}

type gqlExtensions struct {
	PersistedQuery gqlPersistedQuery `json:"persistedQuery"`
}

type gqlPersistedQuery struct {
	Version    int    `json:"version"`
	Sha256Hash string `json:"sha256Hash"`
}

type gqlError struct {
	Message string `json:"message"`
	Path    []any  `json:"path,omitempty"`
}

// gqlResponse wraps the operation's `data` field plus any errors. Caller
// unmarshals `Data` into a per-operation struct after checking Errors.
type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors,omitempty"`
}
```

- [ ] **Step 2: Write failing test**

Create `internal/platform/twitch/client_test.go`:

```go
package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GQLSendsClientIDAndHash(t *testing.T) {
	var got struct {
		clientID string
		body     []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.clientID = r.Header.Get("Client-Id")
		got.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out struct {
		Ok bool `json:"ok"`
	}
	require.NoError(t, c.gql(context.Background(), "", OpInventory, nil, &out))

	assert.Equal(t, clientID, got.clientID)
	assert.Contains(t, string(got.body), `"operationName":"Inventory"`)
	assert.Contains(t, string(got.body), `"sha256Hash":"`+OpInventory.Hash+`"`)
	assert.True(t, out.Ok)
}

func TestClient_GQLReturnsErrorOnGQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"PersistedQueryNotFound"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out json.RawMessage
	err := c.gql(context.Background(), "", OpInventory, nil, &out)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "PersistedQueryNotFound"))
}

func TestClient_GQLSendsBearerWhenTokenProvided(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out json.RawMessage
	require.NoError(t, c.gql(context.Background(), "abc123", OpInventory, nil, &out))
	assert.Equal(t, "OAuth abc123", gotAuth)
}
```

- [ ] **Step 3: Implement the client**

Create `internal/platform/twitch/client.go`:

```go
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

// newTestClient creates a client pointed at an httptest server.
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
		return fmt.Errorf("twitch gql %s: %s", op.Name, resp.Status)
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
		return fmt.Errorf("twitch gql %s: %s", op.Name, strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/platform/twitch/... -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/twitch/
git commit -m "$(cat <<'EOF'
feat(platform/twitch): low-level GraphQL persisted-query client

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Device-code OAuth flow

**Files:**
- Create: `internal/platform/twitch/auth.go`
- Test: `internal/platform/twitch/auth_test.go`

- [ ] **Step 1: Test against a fake device endpoint**

```go
// internal/platform/twitch/auth_test.go
package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuth_StartDeviceLogin_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, clientID, r.Form.Get("client_id"))
		assert.Contains(t, r.Form.Get("scopes"), "user:read:email")
		_, _ = w.Write([]byte(`{
			"device_code":"DEVABC123",
			"user_code":"AAAABBBB",
			"verification_uri":"https://www.twitch.tv/activate",
			"interval":5,
			"expires_in":1800
		}`))
	}))
	defer srv.Close()

	a := &authFlow{deviceURL: srv.URL, tokenURL: "", http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := a.start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "AAAABBBB", ch.UserCode)
	assert.Equal(t, "https://www.twitch.tv/activate", ch.VerificationURL)
	assert.Equal(t, 5*time.Second, ch.Interval)
	internal := ch.Internal.(deviceInternal)
	assert.Equal(t, "DEVABC123", internal.DeviceCode)
}

func TestAuth_PollDeviceLogin_ReturnsSessionOnAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
		assert.Equal(t, "DEVABC123", r.Form.Get("device_code"))
		_, _ = w.Write([]byte(`{
			"access_token":"acc_tok",
			"refresh_token":"ref_tok",
			"expires_in":14400
		}`))
	}))
	defer srv.Close()

	a := &authFlow{deviceURL: "", tokenURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	sess, err := a.poll(context.Background(), deviceInternal{DeviceCode: "DEVABC123"})
	require.NoError(t, err)
	assert.Equal(t, "acc_tok", sess.AccessToken)
	assert.Equal(t, "ref_tok", sess.RefreshToken)
	assert.True(t, sess.ExpiresAt.After(time.Now()))
}

func TestAuth_PollDeviceLogin_ReturnsPendingErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"authorization_pending","status":400}`))
	}))
	defer srv.Close()

	a := &authFlow{tokenURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := a.poll(context.Background(), deviceInternal{DeviceCode: "x"})
	require.ErrorIs(t, err, ErrAuthorizationPending)
}

func TestAuth_FormEncoding(t *testing.T) {
	// Sanity check that we url.Values{}.Encode() consistently.
	v := url.Values{"client_id": {clientID}, "scopes": {"user:read:email channel:read:redemptions"}}
	enc := v.Encode()
	assert.Contains(t, enc, "client_id="+clientID)
	assert.Contains(t, enc, "scopes=user")
}
```

- [ ] **Step 2: Implement**

```go
// internal/platform/twitch/auth.go
package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

// Scopes requested for device login. Mirrors DevilXD reference.
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.deviceURL, strings.NewReader(form.Encode()))
	if err != nil {
		return platform.DeviceChallenge{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.http.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return platform.Session{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.http.Do(req)
	if err != nil {
		return platform.Session{}, err
	}
	defer resp.Body.Close()

	raw, _ := readBody(resp)
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
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return platform.Session{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.http.Do(req)
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
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	const max = 1 << 20
	buf := make([]byte, 0, 1024)
	read := 0
	chunk := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			read += n
			if read > max {
				return buf, fmt.Errorf("response too large")
			}
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}

// secondsFromString helps when downstream code receives `expires_in` as string.
func secondsFromString(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/platform/twitch/... -v -run Auth
```

Expected: all auth tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/twitch/auth.go internal/platform/twitch/auth_test.go
git commit -m "$(cat <<'EOF'
feat(platform/twitch): device-code OAuth flow (start, poll, refresh)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Discovery — campaigns + channels + inventory

**Files:**
- Create: `internal/platform/twitch/campaigns.go`
- Test: `internal/platform/twitch/campaigns_test.go`
- Create: `internal/platform/twitch/testdata/dropspage_contentlist.json` (recorded)
- Create: `internal/platform/twitch/testdata/dropcampaigndetails.json` (recorded)
- Create: `internal/platform/twitch/testdata/inventory.json` (recorded)

- [ ] **Step 1: Record fixture responses**

The implementer subagent should `WebFetch` the DevilXD repo for sample responses (often in `tests/` or as Python docstrings). If no fixtures exist, hand-write minimal JSON matching the response shape that real Twitch returns — DevilXD's parser logic in `inventory.py` and `channel.py` shows the field paths to populate. Save under `internal/platform/twitch/testdata/`. Each fixture must be just enough to drive the parsing code; no need to be byte-for-byte accurate to a live response.

Example minimum shape for `dropspage_contentlist.json`:

```json
{
  "data": {
    "currentUser": {
      "dropCampaigns": [
        {
          "id": "camp1",
          "name": "Rust Twitch Drops",
          "game": {"id": "263490", "displayName": "Rust"},
          "status": "ACTIVE",
          "startAt": "2026-06-01T00:00:00Z",
          "endAt": "2026-07-01T00:00:00Z"
        }
      ]
    }
  }
}
```

Inventory + campaign details — same shape: a `data` envelope around the field paths your code reads. The implementer should look at DevilXD's `Inventory.parse` and replicate the keys.

- [ ] **Step 2: Write the failing test**

```go
// internal/platform/twitch/campaigns_test.go
package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestCampaigns_ListActive_ParsesFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "dropspage_contentlist.json"))
		case OpDropCampaignDetails.Name:
			_, _ = w.Write(loadFixture(t, "dropcampaigndetails.json"))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	d := &discovery{c: c}
	camps, err := d.listActive(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	require.NotEmpty(t, camps)
	assert.Equal(t, "camp1", camps[0].ID)
	assert.Equal(t, "Rust", camps[0].Game)
	// Each campaign should have its benefits filled in from the details query.
	assert.NotEmpty(t, camps[0].Benefits)
}

func TestCampaigns_Inventory_ParsesFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "inventory.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	d := &discovery{c: c}
	pr, err := d.inventory(context.Background(), platform.Session{AccessToken: "tok"})
	require.NoError(t, err)
	require.NotEmpty(t, pr)
}
```

- [ ] **Step 3: Implement discovery**

```go
// internal/platform/twitch/campaigns.go
package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

type discovery struct {
	c *client
}

// dropsPageData is the JSON shape returned by OpCampaigns.
type dropsPageData struct {
	CurrentUser struct {
		DropCampaigns []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			StartAt time.Time `json:"startAt"`
			EndAt   time.Time `json:"endAt"`
			Game    struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"game"`
		} `json:"dropCampaigns"`
	} `json:"currentUser"`
}

type dropCampaignDetailsData struct {
	User struct {
		DropCampaign struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			TimeBasedDrops []struct {
				ID                 string `json:"id"`
				Name               string `json:"name"`
				RequiredMinutesWatched int `json:"requiredMinutesWatched"`
				BenefitEdges       []struct {
					Benefit struct {
						ID    string `json:"id"`
						Name  string `json:"name"`
						Image struct {
							URL string `json:"url"`
						} `json:"image"`
					} `json:"benefit"`
				} `json:"benefitEdges"`
			} `json:"timeBasedDrops"`
		} `json:"dropCampaign"`
	} `json:"user"`
}

type inventoryData struct {
	CurrentUser struct {
		Inventory struct {
			DropCampaignsInProgress []struct {
				ID             string `json:"id"`
				TimeBasedDrops []struct {
					ID                  string `json:"id"`
					Self                struct {
						CurrentMinutesWatched int  `json:"currentMinutesWatched"`
						IsClaimed            bool `json:"isClaimed"`
						DropInstanceID       string `json:"dropInstanceID"`
					} `json:"self"`
				} `json:"timeBasedDrops"`
			} `json:"dropCampaignsInProgress"`
		} `json:"inventory"`
	} `json:"currentUser"`
}

func (d *discovery) listActive(ctx context.Context, sess platform.Session) ([]platform.Campaign, error) {
	var page dropsPageData
	if err := d.c.gql(ctx, sess.AccessToken, OpCampaigns, nil, &page); err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	out := make([]platform.Campaign, 0, len(page.CurrentUser.DropCampaigns))
	for _, c := range page.CurrentUser.DropCampaigns {
		if c.Status != "ACTIVE" {
			continue
		}
		details, err := d.fetchDetails(ctx, sess, c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.Campaign{
			ID:       c.ID,
			Platform: "twitch",
			Game:     c.Game.DisplayName,
			Name:     c.Name,
			Status:   "active",
			StartsAt: c.StartAt,
			EndsAt:   c.EndAt,
			Benefits: details,
		})
	}
	return out, nil
}

func (d *discovery) fetchDetails(ctx context.Context, sess platform.Session, campaignID string) ([]platform.DropBenefit, error) {
	var det dropCampaignDetailsData
	if err := d.c.gql(ctx, sess.AccessToken, OpDropCampaignDetails,
		map[string]any{"dropID": campaignID, "channelLogin": ""}, &det); err != nil {
		return nil, fmt.Errorf("campaign details %s: %w", campaignID, err)
	}
	benefits := []platform.DropBenefit{}
	for _, td := range det.User.DropCampaign.TimeBasedDrops {
		for _, be := range td.BenefitEdges {
			benefits = append(benefits, platform.DropBenefit{
				ID:              be.Benefit.ID,
				CampaignID:      campaignID,
				Name:            be.Benefit.Name,
				RequiredMinutes: td.RequiredMinutesWatched,
				ImageURL:        be.Benefit.Image.URL,
			})
		}
	}
	return benefits, nil
}

func (d *discovery) inventory(ctx context.Context, sess platform.Session) ([]platform.Progress, error) {
	var inv inventoryData
	if err := d.c.gql(ctx, sess.AccessToken, OpInventory, nil, &inv); err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	out := []platform.Progress{}
	for _, camp := range inv.CurrentUser.Inventory.DropCampaignsInProgress {
		for _, td := range camp.TimeBasedDrops {
			out = append(out, platform.Progress{
				BenefitID:      td.ID,
				MinutesWatched: td.Self.CurrentMinutesWatched,
				Claimed:        td.Self.IsClaimed,
			})
		}
	}
	return out, nil
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func init() {
	_ = json.Unmarshal // keep import used
}
```

> If the implementer finds that DevilXD's actual field paths differ from the ones I sketched (e.g. `Benefit.GameAsset` instead of `Benefit.Image`), trust the DevilXD source and adjust types/parsers. Update the matching fixture file in `testdata/`.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/platform/twitch/... -v -run Campaigns
```

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/twitch/
git commit -m "$(cat <<'EOF'
feat(platform/twitch): campaigns + inventory discovery

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Channel listing — find a live drops-enabled stream

**Files:**
- Create: `internal/platform/twitch/channels.go`
- Test: `internal/platform/twitch/channels_test.go`
- Create: `internal/platform/twitch/testdata/streamlive.json`

- [ ] **Step 1: Fixture**

Save minimal JSON to `testdata/streamlive.json`:

```json
{
  "data": {
    "user": {
      "login": "fakestreamer",
      "stream": {
        "id": "12345",
        "viewersCount": 9001
      }
    }
  }
}
```

(The implementer should inspect DevilXD for the actual operation name and field path. `WithIsStreamLiveQuery` may not be the right operation — DevilXD uses a per-channel query when checking if a campaign's allow-listed streamers are live. Adjust accordingly.)

- [ ] **Step 2: Test**

```go
// internal/platform/twitch/channels_test.go
package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestChannels_ListEligible_ReturnsLiveOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		_, _ = w.Write(loadFixture(t, "streamlive.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	ch := &channels{c: c}
	camp := platform.Campaign{ID: "camp1", Platform: "twitch"}
	out, err := ch.listEligible(context.Background(), platform.Session{AccessToken: "tok"}, camp, []string{"fakestreamer"})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	assert.Equal(t, "fakestreamer", out[0].Channel)
	assert.True(t, out[0].DropsEnabled)
}
```

- [ ] **Step 3: Implement**

```go
// internal/platform/twitch/channels.go
package twitch

import (
	"context"
	"fmt"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

type channels struct {
	c *client
}

type streamLiveData struct {
	User struct {
		Login  string `json:"login"`
		Stream *struct {
			ID           string `json:"id"`
			ViewersCount int    `json:"viewersCount"`
		} `json:"stream"`
	} `json:"user"`
}

func (ch *channels) listEligible(ctx context.Context, sess platform.Session, _ platform.Campaign, allowedLogins []string) ([]platform.Stream, error) {
	out := []platform.Stream{}
	for _, login := range allowedLogins {
		var sd streamLiveData
		err := ch.c.gql(ctx, sess.AccessToken, OpGetStreamInfo,
			map[string]any{"login": login}, &sd)
		if err != nil {
			return nil, fmt.Errorf("stream live %s: %w", login, err)
		}
		if sd.User.Stream == nil {
			continue
		}
		out = append(out, platform.Stream{
			Channel:      sd.User.Login,
			ViewerCount:  sd.User.Stream.ViewersCount,
			DropsEnabled: true, // we trust the campaign allow-list; live + allowed = drops-enabled
		})
	}
	return out, nil
}
```

> Real Twitch returns the allowed-channel list as part of the campaign details (`allow.channels[]` in DevilXD). The implementer should thread that through — for now the simplest contract is "give me a list of logins, tell me which are live."

- [ ] **Step 4: Run tests**

```bash
go test ./internal/platform/twitch/... -v -run Channels
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/twitch/
git commit -m "$(cat <<'EOF'
feat(platform/twitch): channel listing (live + drops-enabled filter)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Watch heartbeat (SendEvents mutation)

DevilXD's heartbeat is a GraphQL **mutation** posted to `gql.twitch.tv/gql`, not a REST endpoint. The mutation is `SendEvents` with the full query body inline (no persisted hash). Payload is `base64(gzip(json_minify([eventList])))` carried in `variables.input.data`. Required `variables.input` extras: `repository="twilight"` and `encoding="GZIP_B64"`. Successful response shape: `{ "data": { "sendSpadeEvents": { "statusCode": 204 } } }`.

**Files:**
- Create: `internal/platform/twitch/watch.go`
- Test: `internal/platform/twitch/watch_test.go`
- Modify: `internal/platform/twitch/client.go` (add `gqlQuery` for non-persisted operations)

- [ ] **Step 1: Add `gqlQuery` to the client**

In `internal/platform/twitch/client.go`, add this method below `gql`:

```go
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
		return fmt.Errorf("twitch gql %s: %s", operationName, resp.Status)
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
		return fmt.Errorf("twitch gql %s: %s", operationName, strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}
```

- [ ] **Step 2: Test (against httptest)**

```go
// internal/platform/twitch/watch_test.go
package twitch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestWatch_HeartbeatSendsGzippedBase64Mutation(t *testing.T) {
	var got struct {
		body []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"data":{"sendSpadeEvents":{"statusCode":204}}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	wt := &watch{c: c}
	h, err := wt.start(context.Background(), platform.Session{AccessToken: "tok"},
		platform.Stream{Channel: "fakestreamer"})
	require.NoError(t, err)

	require.NoError(t, wt.heartbeat(context.Background(), h))

	// Parse the outbound JSON request body, extract variables.input.data,
	// b64-decode, gunzip, JSON-parse, assert the event payload.
	var req struct {
		OperationName string `json:"operationName"`
		Variables     struct {
			Input struct {
				Data       string `json:"data"`
				Repository string `json:"repository"`
				Encoding   string `json:"encoding"`
			} `json:"input"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(got.body, &req))
	assert.Equal(t, "SendEvents", req.OperationName)
	assert.Equal(t, "twilight", req.Variables.Input.Repository)
	assert.Equal(t, "GZIP_B64", req.Variables.Input.Encoding)

	raw, err := base64.StdEncoding.DecodeString(req.Variables.Input.Data)
	require.NoError(t, err)
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	plain, err := io.ReadAll(gr)
	require.NoError(t, err)

	var events []map[string]any
	require.NoError(t, json.Unmarshal(plain, &events))
	require.Len(t, events, 1)
	assert.Equal(t, "minute-watched", events[0]["event"])
	props := events[0]["properties"].(map[string]any)
	assert.Equal(t, "fakestreamer", props["channel"])
}
```

- [ ] **Step 3: Implement**

```go
// internal/platform/twitch/watch.go
package twitch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

const sendEventsMutation = `mutation SendEvents($input: SendSpadeEventsInput!) {
  sendSpadeEvents(input: $input) {
    statusCode
  }
}`

type watch struct {
	c *client
}

func newWatch() *watch {
	return &watch{c: newClient()}
}

type watchInternal struct {
	Channel    string
	ChannelID  string
	BroadcastID string
	UserID     int64
}

func (w *watch) start(_ context.Context, _ platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	// In a complete impl we'd fetch channel id + broadcast id here. For
	// the heartbeat to register drop progress, the channel login is the
	// minimum; channel/broadcast IDs are best-effort and may need a
	// follow-up GetStreamInfo query in production.
	return platform.WatchHandle{
		Channel:  stream.Channel,
		Internal: watchInternal{Channel: stream.Channel},
	}, nil
}

func (w *watch) heartbeat(ctx context.Context, h platform.WatchHandle) error {
	internal, ok := h.Internal.(watchInternal)
	if !ok {
		return fmt.Errorf("invalid watch handle")
	}

	event := map[string]any{
		"event": "minute-watched",
		"properties": map[string]any{
			"broadcast_id":   internal.BroadcastID,
			"channel_id":     internal.ChannelID,
			"channel":        internal.Channel,
			"client_time":    time.Now().UTC().Format(time.RFC3339),
			"game":           "",
			"game_id":        "",
			"hidden":         false,
			"is_live":        true,
			"live":           true,
			"logged_in":      true,
			"minutes_logged": 1,
			"muted":          false,
			"user_id":        internal.UserID,
		},
	}
	plain, err := json.Marshal([]any{event})
	if err != nil {
		return err
	}

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	if _, err := gw.Write(plain); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(gz.Bytes())

	variables := map[string]any{
		"input": map[string]any{
			"data":       encoded,
			"repository": "twilight",
			"encoding":   "GZIP_B64",
		},
	}

	var resp struct {
		SendSpadeEvents struct {
			StatusCode int `json:"statusCode"`
		} `json:"sendSpadeEvents"`
	}
	return w.c.gqlQuery(ctx, "", "SendEvents", sendEventsMutation, variables, &resp)
}

func (w *watch) stop(_ context.Context, _ platform.WatchHandle) error {
	return nil
}
```

> The auth token is intentionally `""` on `gqlQuery` because the SessionStore handle isn't reachable from `watch.heartbeat` — the watcher's main loop calls Heartbeat without re-supplying the session. Plan 3 Task 8 (Backend wrapper) bridges this: the wrapper's `Heartbeat` method should call `w.c.gqlQuery(ctx, b.currentToken(h), ...)` where `currentToken` looks the token up from the active session map keyed by handle. For now, this minimal impl will work against the httptest server (no auth check) but a real Twitch request will 401. The bridging is added when Backend is finalized in Task 8.

- [ ] **Step 3: Run tests**

```bash
go test ./internal/platform/twitch/... -v -run Watch
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/twitch/watch.go internal/platform/twitch/watch_test.go
git commit -m "$(cat <<'EOF'
feat(platform/twitch): minute-watched heartbeat

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Claim mutation

**Files:**
- Create: `internal/platform/twitch/claim.go`
- Test: `internal/platform/twitch/claim_test.go`
- Create: `internal/platform/twitch/testdata/claim_ok.json`

- [ ] **Step 1: Fixture**

```json
{"data":{"claimDropRewards":{"status":"DROP_INSTANCE_ALREADY_CLAIMED"}}}
```

- [ ] **Step 2: Test**

```go
// internal/platform/twitch/claim_test.go
package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestClaim_SendsCorrectVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "claim_ok.json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	cl := &claimer{c: c}
	err := cl.claim(context.Background(), platform.Session{AccessToken: "tok"},
		platform.DropBenefit{ID: "drop1", CampaignID: "camp1"})
	require.NoError(t, err)
}
```

- [ ] **Step 3: Implement**

```go
// internal/platform/twitch/claim.go
package twitch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

type claimer struct {
	c *client
}

type claimResult struct {
	ClaimDropRewards struct {
		Status string `json:"status"`
	} `json:"claimDropRewards"`
}

func (cl *claimer) claim(ctx context.Context, sess platform.Session, b platform.DropBenefit) error {
	var out claimResult
	err := cl.c.gql(ctx, sess.AccessToken, OpClaimDrop,
		map[string]any{"input": map[string]any{"dropInstanceID": b.ID}}, &out)
	if err != nil {
		return fmt.Errorf("claim %s: %w", b.ID, err)
	}
	switch out.ClaimDropRewards.Status {
	case "ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED":
		return nil
	case "":
		// No status returned — treat as success if no error came through gql().
		return nil
	default:
		return fmt.Errorf("claim status: %s", out.ClaimDropRewards.Status)
	}
}

func init() { _ = json.Unmarshal }
```

> The `dropInstanceID` field name comes from DevilXD. Actual GraphQL `input` shape may have additional required fields — confirm against DevilXD.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/platform/twitch/... -v -run Claim
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/twitch/claim.go internal/platform/twitch/claim_test.go internal/platform/twitch/testdata/
git commit -m "$(cat <<'EOF'
feat(platform/twitch): ClaimDrop mutation

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Backend wrapper implementing platform.Backend

**Files:**
- Create: `internal/platform/twitch/backend.go`
- Test: `internal/platform/twitch/backend_test.go`

- [ ] **Step 1: Implement the Backend struct**

```go
// internal/platform/twitch/backend.go
package twitch

import (
	"context"
	"errors"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

// Backend implements platform.Backend for Twitch using GraphQL persisted
// queries (mirrored from DevilXD/TwitchDropsMiner).
type Backend struct {
	c     *client
	auth  *authFlow
	disc  *discovery
	chans *channels
	watch *watch
	claim *claimer

	// allowedLoginsByCampaign is populated by ListActiveCampaigns and
	// consumed by ListEligibleChannels. Mutex-protected.
	mu                       sync.Mutex
	allowedLoginsByCampaign  map[string][]string
}

var _ platform.Backend = (*Backend)(nil)

func New() *Backend {
	c := newClient()
	return &Backend{
		c:    c,
		auth: newAuthFlow(),
		disc: &discovery{c: c},
		chans: &channels{c: c},
		watch: newWatch(),
		claim: &claimer{c: c},
		allowedLoginsByCampaign: map[string][]string{},
	}
}

func (b *Backend) Name() string { return "twitch" }

func (b *Backend) StartDeviceLogin(ctx context.Context) (platform.DeviceChallenge, error) {
	return b.auth.start(ctx)
}

func (b *Backend) PollDeviceLogin(ctx context.Context, ch platform.DeviceChallenge) (platform.Session, error) {
	internal, ok := ch.Internal.(deviceInternal)
	if !ok {
		return platform.Session{}, errors.New("invalid challenge internal")
	}
	return b.auth.poll(ctx, internal)
}

func (b *Backend) LoginViaBrowser(_ context.Context, _ platform.BrowserRPC) (platform.Session, error) {
	return platform.Session{}, errors.New("not supported")
}

func (b *Backend) RefreshSession(ctx context.Context, s platform.Session) (platform.Session, error) {
	return b.auth.refresh(ctx, s)
}

func (b *Backend) ListActiveCampaigns(ctx context.Context, s platform.Session) ([]platform.Campaign, error) {
	camps, err := b.disc.listActive(ctx, s)
	if err != nil {
		return nil, err
	}
	// DevilXD also fetches the per-campaign allow-list; we plug that in here
	// by reading the same `dropCampaignDetails` query's `allow.channels[].login`
	// field. For now we leave it empty and treat every campaign as "any live".
	b.mu.Lock()
	for _, c := range camps {
		b.allowedLoginsByCampaign[c.ID] = nil
	}
	b.mu.Unlock()
	return camps, nil
}

func (b *Backend) ListEligibleChannels(ctx context.Context, s platform.Session, c platform.Campaign) ([]platform.Stream, error) {
	b.mu.Lock()
	allowed := b.allowedLoginsByCampaign[c.ID]
	b.mu.Unlock()
	if len(allowed) == 0 {
		// No allow-list cached — fall back to "no eligible channels". The
		// watcher will sleep until something changes.
		return nil, nil
	}
	return b.chans.listEligible(ctx, s, c, allowed)
}

func (b *Backend) InventoryProgress(ctx context.Context, s platform.Session) ([]platform.Progress, error) {
	return b.disc.inventory(ctx, s)
}

func (b *Backend) StartWatch(ctx context.Context, s platform.Session, stream platform.Stream) (platform.WatchHandle, error) {
	return b.watch.start(ctx, s, stream)
}

func (b *Backend) Heartbeat(ctx context.Context, h platform.WatchHandle) error {
	return b.watch.heartbeat(ctx, h)
}

func (b *Backend) StopWatch(ctx context.Context, h platform.WatchHandle) error {
	return b.watch.stop(ctx, h)
}

func (b *Backend) Claim(ctx context.Context, s platform.Session, drop platform.DropBenefit) error {
	return b.claim.claim(ctx, s, drop)
}
```

Add the missing import:

```go
import (
	"context"
	"errors"
	"sync"
	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)
```

- [ ] **Step 2: Compile-time interface check (done by `var _ ...` above)**

```bash
go build ./internal/platform/twitch/...
```

If `Backend` is missing a method, the compiler will say so.

- [ ] **Step 3: Run all twitch tests**

```bash
go test ./internal/platform/twitch/... -v
```

Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/twitch/backend.go
git commit -m "$(cat <<'EOF'
feat(platform/twitch): Backend wrapper implementing platform.Backend

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Session persistence — cryptor wiring

The cryptor was added in Plan 1 but never used. Now we plug it in so Twitch session tokens land in the `sessions` table encrypted.

**Files:**
- Create: `internal/store/sessions.go`
- Test: `internal/store/sessions_test.go`

- [ ] **Step 1: Test**

```go
// internal/store/sessions_test.go
package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

func TestSessionStore_RoundTrip(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	// We need an account row first because of the FK.
	_, err = q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: "acc1", Platform: "twitch", Login: "demo", DisplayName: "demo",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	})
	require.NoError(t, err)

	c, err := NewCryptor(testKey)
	require.NoError(t, err)
	ss := NewSessionStore(db, q, c)

	in := platform.Session{
		AccessToken: "secret",
		RefreshToken: "ref",
		ExpiresAt:   time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	require.NoError(t, ss.Put(context.Background(), "acc1", in))

	out, ok, err := ss.Get(context.Background(), "acc1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "secret", out.AccessToken)
	assert.Equal(t, "ref", out.RefreshToken)
	assert.Equal(t, in.ExpiresAt, out.ExpiresAt)
	_ = json.Marshal
}

func TestSessionStore_MissingReturnsFalse(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	c, err := NewCryptor(testKey)
	require.NoError(t, err)
	ss := NewSessionStore(db, q, c)

	_, ok, err := ss.Get(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, ok)
}
```

- [ ] **Step 2: Implement**

```go
// internal/store/sessions.go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

// SessionStore persists encrypted platform.Session blobs in the sessions
// table.
type SessionStore struct {
	db *sql.DB
	q  *gen.Queries
	c  *Cryptor
}

func NewSessionStore(db *sql.DB, q *gen.Queries, c *Cryptor) *SessionStore {
	return &SessionStore{db: db, q: q, c: c}
}

func (s *SessionStore) Put(ctx context.Context, accountID string, sess platform.Session) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	ct, err := s.c.Encrypt(raw)
	if err != nil {
		return err
	}
	return s.q.UpsertSession(ctx, gen.UpsertSessionParams{
		AccountID: accountID,
		Ciphertext: ct,
		ExpiresAt: sess.ExpiresAt.Unix(),
	})
}

func (s *SessionStore) Get(ctx context.Context, accountID string) (platform.Session, bool, error) {
	row, err := s.q.GetSession(ctx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return platform.Session{}, false, nil
	}
	if err != nil {
		return platform.Session{}, false, err
	}
	raw, err := s.c.Decrypt(row.Ciphertext)
	if err != nil {
		return platform.Session{}, false, err
	}
	var sess platform.Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return platform.Session{}, false, err
	}
	return sess, true, nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/store/... -v -run Session
```

Expected: 2 PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/store/sessions.go internal/store/sessions_test.go
git commit -m "$(cat <<'EOF'
feat(store): SessionStore — encrypted platform.Session persistence

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Wire Twitch backend into main

**Files:**
- Modify: `cmd/miner/main.go`

- [ ] **Step 1: Register backend + load persisted sessions**

In `cmd/miner/main.go`, replace the platform registry block and the `build` closure:

```go
// Register both backends. Fake is still used for dev seed; Twitch is
// the real one.
registry := platform.NewRegistry()
registry.Register(fake.New(fake.WithFastTime()))
registry.Register(twitch.New())

sessions := store.NewSessionStore(db, q, cryptor)

build := func(a gen.Account) (scheduler.Entry, error) {
	b, ok := registry.Get(a.Platform)
	if !ok {
		return scheduler.Entry{}, fmt.Errorf("no backend for platform %q", a.Platform)
	}

	var sess platform.Session
	switch a.Platform {
	case "fake":
		// Fake backend logs in without any state.
		s, err := b.PollDeviceLogin(ctx, platform.DeviceChallenge{})
		if err != nil {
			return scheduler.Entry{}, fmt.Errorf("device login: %w", err)
		}
		sess = s
	default:
		// Real backends use the persisted session blob. If missing or
		// expired, the account stays paused until the GUI re-runs the
		// device-code flow (Task 11).
		s, ok, err := sessions.Get(ctx, a.ID)
		if err != nil {
			return scheduler.Entry{}, fmt.Errorf("load session: %w", err)
		}
		if !ok || s.ExpiresAt.Before(time.Now()) {
			return scheduler.NewEntry(a.ID, nopRunner{}), nil
		}
		sess = s
	}

	w := watcher.New(watcher.Config{
		AccountID: a.ID, Backend: b, Session: sess,
		Notifier: notifier, TickInterval: 500 * time.Millisecond,
	})
	return scheduler.NewEntry(a.ID, w), nil
}
```

Add the import:

```go
"github.com/chano-fernandez/rust-drops-miner/internal/platform/twitch"
```

- [ ] **Step 2: Make `sessions` reachable to the API handlers**

Extend `api.Deps`:

```go
// internal/api/server.go — inside type Deps struct
Sessions *store.SessionStore
```

And in main.go's `deps` literal:

```go
deps := api.Deps{
	DB: db, Q: q, Templates: templates, Session: sm,
	Scheduler: sched, Reload: loadAndStart, Sessions: sessions,
}
```

Plus the matching import in `internal/api/server.go`:

```go
"github.com/chano-fernandez/rust-drops-miner/internal/store"
```

- [ ] **Step 3: Build + run all tests**

```bash
go build ./...
go test -race ./...
```

Both clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/miner/main.go internal/api/server.go
git commit -m "$(cat <<'EOF'
feat(cmd/miner): register twitch backend + persisted session loading

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Twitch login GUI flow

**Files:**
- Create: `internal/api/handlers_login_twitch.go`
- Create: `internal/web/templates/login_twitch.html`
- Create: `internal/web/templates/login_twitch_status.html`
- Modify: `internal/web/templates/accounts_new.html`
- Modify: `internal/api/handlers_accounts.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Template — login page**

```html
<!-- internal/web/templates/login_twitch.html -->
{{define "title"}}Twitch login · Rust Drops Miner{{end}}
{{define "content"}}
{{with .Page}}
<h1>Authorize Twitch — {{.DisplayName}}</h1>
<p>Open the following URL in any browser and enter the code:</p>
<p><a href="{{.VerificationURL}}" target="_blank" rel="noopener">{{.VerificationURL}}</a></p>
<p style="font-size:2rem;letter-spacing:0.5rem;font-family:monospace">{{.UserCode}}</p>
<div id="status"
     hx-get="/accounts/{{.AccountID}}/login/poll"
     hx-trigger="every 3s"
     hx-swap="innerHTML">
  <em>waiting for authorization…</em>
</div>
<p><a href="/accounts">cancel</a></p>
{{end}}
{{end}}
```

```html
<!-- internal/web/templates/login_twitch_status.html -->
{{define "login_twitch_status"}}
{{if eq . "pending"}}<em>still waiting…</em>{{end}}
{{if eq . "done"}}<strong class="ok">authorized — redirecting</strong>
<script>setTimeout(()=>window.location='/accounts','800');</script>{{end}}
{{if eq . "expired"}}<strong class="err">code expired — return to <a href="/accounts">accounts</a> and re-add</strong>{{end}}
{{if eq . "error"}}<strong class="err">error — see logs</strong>{{end}}
{{end}}
```

- [ ] **Step 2: Handler**

```go
// internal/api/handlers_login_twitch.go
package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
	"github.com/chano-fernandez/rust-drops-miner/internal/store"
	"github.com/chano-fernandez/rust-drops-miner/internal/store/gen"
)

type loginTwitchDeps struct {
	q          *gen.Queries
	t          Renderer
	sm         *scs.SessionManager
	sessions   *store.SessionStore
	registry   *platform.Registry
	pending    sync.Map // accountID -> *twitchLoginState
}

type twitchLoginState struct {
	mu        sync.Mutex
	challenge platform.DeviceChallenge
	status    string // "pending" | "done" | "expired" | "error"
	startedAt time.Time
}

type loginPageData struct {
	AccountID       string
	DisplayName     string
	VerificationURL string
	UserCode        string
}

func (d *loginTwitchDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	backend, ok := d.registry.Get(acc.Platform)
	if !ok {
		http.Error(w, "no backend for platform", http.StatusBadRequest)
		return
	}

	ch, err := backend.StartDeviceLogin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	st := &twitchLoginState{challenge: ch, status: "pending", startedAt: time.Now()}
	d.pending.Store(id, st)
	go d.poll(id, acc.Platform, backend, st)

	render(w, d.t, "login_twitch.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginPageData{
			AccountID:       id,
			DisplayName:     acc.DisplayName,
			VerificationURL: ch.VerificationURL,
			UserCode:        ch.UserCode,
		},
	})
}

func (d *loginTwitchDeps) poll(accountID, platformName string, backend platform.Backend, st *twitchLoginState) {
	interval := st.challenge.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	deadline := st.challenge.ExpiresAt
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		sess, err := backend.PollDeviceLogin(nil, st.challenge) //nolint:contextcheck
		if err != nil {
			// Pending is expected; anything else is a real error.
			if errMsg := err.Error(); errMsg != "authorization_pending" &&
				!stringContains(errMsg, "authorization_pending") {
				st.mu.Lock()
				st.status = "error"
				st.mu.Unlock()
				return
			}
			continue
		}
		if err := d.sessions.Put(nil, accountID, sess); err != nil { //nolint:contextcheck
			st.mu.Lock()
			st.status = "error"
			st.mu.Unlock()
			return
		}
		st.mu.Lock()
		st.status = "done"
		st.mu.Unlock()
		return
	}
	st.mu.Lock()
	st.status = "expired"
	st.mu.Unlock()
}

func (d *loginTwitchDeps) status(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	v, ok := d.pending.Load(id)
	if !ok {
		renderPartial(w, d.t, "login_twitch_status", "error")
		return
	}
	st := v.(*twitchLoginState)
	st.mu.Lock()
	status := st.status
	st.mu.Unlock()
	renderPartial(w, d.t, "login_twitch_status", status)
}

func stringContains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (indexAny(s, substr) >= 0))))
}

func indexAny(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
```

> The `nil` context calls above (`backend.PollDeviceLogin(nil, ...)`) are a hack to keep this task small. Replace with a stored `context.Context` that the daemon's root signal context cancels — wrap in a follow-up step or accept the technical debt for this plan (the goroutine exits naturally at challenge expiry).

> Actually, do this properly: store the root ctx via a field on `loginTwitchDeps` initialized from `cmd/miner/main.go`. Don't ship nil contexts.

- [ ] **Step 3: Wire into accounts/new flow**

Edit `internal/web/templates/accounts_new.html` — add the Twitch option to the select:

```html
<option value="fake">fake (development)</option>
<option value="twitch">Twitch (drops)</option>
```

Edit `internal/api/handlers_accounts.go` `newPost` — after CreateAccount, redirect:

```go
// Twitch accounts need the device-code flow before they can mine.
if platform == "twitch" {
	http.Redirect(w, r, "/accounts/"+id+"/login", http.StatusSeeOther)
	return
}
d.sm.Put(r.Context(), "flash", "account added — click Apply changes to start mining")
http.Redirect(w, r, "/accounts", http.StatusSeeOther)
```

Edit `internal/api/server.go` to mount the new routes inside `authed`:

```go
login := &loginTwitchDeps{
	q: d.Q, t: d.Templates, sm: d.Session,
	sessions: d.Sessions, registry: d.Registry,
}
authed.Get("/accounts/{id}/login", login.get)
authed.Get("/accounts/{id}/login/poll", login.status)
```

Extend `Deps`:

```go
Registry *platform.Registry
```

And pass it from main:

```go
deps := api.Deps{
	...
	Registry: registry,
}
```

- [ ] **Step 4: Build + tests**

```bash
go build ./...
go test -race ./...
```

Clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/miner/main.go internal/api/ internal/web/templates/
git commit -m "$(cat <<'EOF'
feat(api): Twitch device-code login flow with HTMX-polled status

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Manual real-account end-to-end test (user-run)

**Files:** none (verification only).

This task cannot be fully automated because we need a real Twitch account. The implementer subagent should produce a short user runbook for the human operator.

- [ ] **Step 1: Compose up**

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
docker compose up --build -d
```

- [ ] **Step 2: Walk the flow**

1. Open http://127.0.0.1:8080 → `/setup` → set admin password.
2. Go to `/accounts/new`, choose "Twitch (drops)", enter your Twitch login, click Create.
3. You land on `/accounts/<id>/login` showing a USER CODE and `https://www.twitch.tv/activate` URL.
4. Open the URL on any browser logged into the throwaway Twitch account, enter the code.
5. Within ~5s the GUI flips to "authorized — redirecting" and lands you on `/accounts`.
6. Click "Apply changes". Dashboard now shows the account in `pick_campaign` state.
7. Wait. Within a few minutes the watcher should reach `watching` for any active Rust drops campaign. Discord (if configured) posts state/progress events.

- [ ] **Step 3: Capture log evidence**

```bash
docker compose logs miner | grep -E '"event":"(state|claim|error)"' | head -20
```

Expect: `state=pick_campaign`, `state=pick_stream`, eventually `state=watching`.

- [ ] **Step 4: Teardown**

```bash
docker compose down
```

The implementer's report should include a one-line note: "User must manually verify Step 2–7 against a real Twitch account."

---

## Done definition

After Task 12 (or after Task 11 if real-account verification is deferred):

1. `internal/platform/twitch` implements `platform.Backend` end-to-end against `httptest` fixtures.
2. `docker compose up` exposes the GUI; creating a Twitch account triggers the device-code page.
3. After a real Twitch operator completes the activate flow, the session is persisted (encrypted) and the watcher loads it on next Reload.
4. The watcher reaches `watching` state for at least one campaign within ~5 minutes when a live drops-enabled stream exists.
5. `go test -race ./...` green.

## Self-review notes

- DevilXD's GraphQL operations are the source of truth. If a test fixture's field path doesn't match what `discovery.go` reads, adjust the fixture (it's hand-crafted to drive parsing, not a real Twitch response).
- The allow-list cache (`allowedLoginsByCampaign`) is initialized empty. Until the implementer wires `dropCampaignDetails.allow.channels[].login` into it, `ListEligibleChannels` returns no streams, so the watcher will sleep. That's a known regression vs. the FakeBackend — the next plan revision should fill this in. Mark as DONE_WITH_CONCERNS and call out.
- The `loginTwitchDeps.poll` goroutine uses `nil` contexts in the plan draft. The implementer MUST replace those with a stored root context before committing. Don't ship `nil` ctx to public APIs.

## Next plan preview

Plan 4: Real Kick backend via the headless-browser sidecar. KickDropsMiner only works via browser, so this is a heavier lift: cmd/browser-sidecar binary, gRPC contract, headless Chromium via `rod`, screenshot streaming to the GUI for interactive login.
