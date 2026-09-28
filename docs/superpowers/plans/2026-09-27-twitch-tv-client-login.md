# Twitch TV-Client Login (#48 / DevilXD #1165) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** New Twitch accounts can log in and mine again. Twitch blocked device-code login for the Android client_id on about 2026-09-18, so new logins now use the TV client, and every request for a session goes out under the Client-Id that session was issued to.

**Architecture:** Each `platform.Session` records the Twitch client it was minted by (`ClientID`: `""` = legacy Android, `"tv"` = TV). The Twitch `client` keeps a token→profile registry, and `setCommonHeaders` picks Client-Id/User-Agent from it. Twitch hides `ViewerDropsDashboard` and `DropCampaignDetails` from TV tokens (`campaigns=0` / `dropCampaign:null`). So for TV sessions, discovery is channel-first: game directory (DROPS_ENABLED) → `DropsHighlightService_AvailableDrops` per channel (which returns full drop data), merged with Inventory's in-progress campaigns (which carry their allow-lists). Legacy Android sessions keep the existing dashboard path untouched.

**Tech Stack:** Go 1.x, net/http, httptest, testify. Twitch GQL persisted queries (hashes already in `internal/platform/twitch/ops.go`).

**Spec:** Evidence from live probes 2026-09-24..27, recorded in memory `project_twitch_device_invalid_client_2026-09-24.md`. Key facts this plan relies on:
- `POST id.twitch.tv/oauth2/device` with client_id `kd1unb4b3q4t58fwlpcbzcbnm76a8fp` → `400 {"message":"invalid client"}`; with `ue6666qo983tsx6so1t0vnawi233wa` (TV) → 200.
- A TV token sent under the Android Client-Id: GQL reads work, but the **Spade beacon earns 0 minutes**. Under the TV Client-Id it **earns minutes** (5→9 in 7 min; Dragonfire 170→180 in 13 min), and a claim via inventory `dropInstanceID` succeeded (reward in `gameEventDrops`).
- A TV token sees: `ViewerDropsDashboard` → 0 campaigns, `DropCampaignDetails` → `dropCampaign:null`, Inventory → full data incl. `allow.channels` and `requiredSubs`, `AvailableDrops` → full `timeBasedDrops` (id, requiredMinutesWatched, requiredSubs, benefitEdges).
- `claimDropRewards: null` (bad/missing instance id) currently counts as success in `claim.go`.

## Global Constraints

- Go/HTMX only. No SPA and no JS port (project decision).
- Never use the name "chano-fernandez" anywhere.
- CI gate: `gofmt -l .` must print nothing; `go build ./... && go test ./...` must pass.
- Existing Android sessions must keep working with **zero** behavior change (they hold the only Android tokens anyone will ever have again).
- TV client constants: ID `ue6666qo983tsx6so1t0vnawi233wa`; User-Agent `Mozilla/5.0 (Linux; Android 9; SHIELD Android TV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36`.
- Persisted `Session` JSON must stay backward compatible: a new field with `omitempty`; an absent field means legacy Android.
- Heartbeat stays locked at 60s (see memory `project_todo_lock_heartbeat_60s`).
- Never post to GitHub (issues/PRs/comments) without explicit user approval.

## Review Focus

1. **Refresh drops the client:** `authFlow.refresh` rebuilds the Session from scratch, so a refreshed TV session would silently become "Android" and stop earning. Refresh must keep `ClientID` and use the matching client_id in the form. Pinned in Task 2.
2. **Heartbeat after restart:** the watch handle holds only the token. If the registry isn't bound before the first heartbeat (restart → StartWatch → Heartbeat), beacons go out under the Android Client-Id and earn nothing, with no error. Pinned in Task 1 (bind on StartWatch) and Task 1's heartbeat test.
3. **A game not live anywhere:** a whitelisted game with zero drops-enabled streams must yield no campaigns and no error; it must not abort discovery for other games. Pinned in Task 4.
4. **Sub-only drops via AvailableDrops:** `requiredSubs > 0` must map to `RequiredMinutes = 0` (same rule as #47) so the watcher never mines them. Pinned in Task 4.
5. **Null claim response:** must return an error, not success, so the watcher doesn't mark a drop claimed that Twitch still shows unclaimed (a likely cause of Abu's "claimed but shown unclaimed"). Pinned in Task 3.

---

## File Structure

- `internal/platform/types.go`: add `Session.ClientID` (persisted) and `Session.Games []string` (runtime, `json:"-"`).
- `internal/platform/twitch/profile.go` (**new**): `clientProfile`, `profileAndroid`, `profileTV`, `profileFor(id string)`.
- `internal/platform/twitch/client.go`: registry (`bind`, `profileForToken`); `setCommonHeaders` uses it.
- `internal/platform/twitch/backend.go`: call `b.c.bind(s)` at every public entry that takes a Session; route `ListActiveCampaigns` by client.
- `internal/platform/twitch/auth.go`: device flow on TV; refresh by session profile; error body in the device-start error.
- `internal/platform/twitch/claim.go`: empty status → error.
- `internal/platform/twitch/chandisc.go` (**new**): channel-first discovery for TV sessions.
- `internal/discovery/twitch.go`, `internal/watcher/watcher.go`, `cmd/miner/main.go`: plumb whitelist game names into `Session.Games`.
- Tests next to each file.

Base branch: `main`, with #47 commit `be53a1f` (branch `fix/issue-47-sub-drops`) cherry-picked first, because Task 4 reuses its `requiredSubs` rule.

---

### Task 0: Branch setup

- [ ] **Step 1: Create the branch and bring in #47**

```bash
git checkout main && git pull
git checkout -b fix/twitch-tv-client-login
git fetch origin fix/issue-47-sub-drops 2>/dev/null || true
git cherry-pick be53a1f
go build ./... && go test ./internal/platform/twitch/
```
Expected: build OK, tests PASS. (If `be53a1f` isn't reachable, re-apply: in `fetchDetails`, add `RequiredSubs int \`json:"requiredSubs"\`` to the timeBasedDrops struct and set `RequiredMinutes` to 0 when `RequiredSubs > 0`.)

---

### Task 1: Per-session Twitch client profile

**Files:**
- Create: `internal/platform/twitch/profile.go`
- Modify: `internal/platform/types.go` (Session), `internal/platform/twitch/client.go:35-57,226-240`, `internal/platform/twitch/backend.go` (public methods)
- Test: `internal/platform/twitch/profile_test.go`

**Interfaces:**
- Produces: `platform.Session.ClientID string` (`json:"client_id,omitempty"`), `platform.Session.Games []string` (`json:"-"`); `const ClientTV = "tv"` (exported, in package twitch); `type clientProfile struct{ ID, UserAgent string }`; `func profileFor(clientID string) clientProfile`; `func (c *client) bind(s platform.Session)`; `func (c *client) profileForToken(token string) clientProfile`.

- [ ] **Step 1: Add Session fields**

In `internal/platform/types.go` `type Session struct`, after `Fingerprint`:

```go
	// ClientID names the Twitch OAuth client this token was minted by.
	// "" = legacy Android app client (all sessions before 2026-09-27);
	// "tv" = Twitch for TV (device-code logins after Twitch blocked the
	// Android client, #48). Requests MUST go out under the same client:
	// Twitch accepts GQL reads under a mismatched Client-Id but silently
	// credits zero watch-minutes.
	ClientID string `json:"client_id,omitempty"`
	// Games is the account's whitelisted game names, plumbed at use time
	// (like GameFilter). TV sessions can't see the drops dashboard, so
	// channel-first discovery needs the names to know which game
	// directories to walk. Not persisted.
	Games []string `json:"-"`
```

- [ ] **Step 2: Write the failing test**

Create `internal/platform/twitch/profile_test.go`:

```go
package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

func TestProfileFor(t *testing.T) {
	assert.Equal(t, clientID, profileFor("").ID, "legacy sessions stay Android")
	assert.Equal(t, "ue6666qo983tsx6so1t0vnawi233wa", profileFor(ClientTV).ID)
	assert.Equal(t, clientID, profileFor("bogus").ID, "unknown falls back to Android")
}

// Each token must be sent under the Client-Id of the client that minted it.
func TestClientIDFollowsSession(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{} // Authorization -> Client-Id
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")] = r.Header.Get("Client-Id")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[]}}}}`))
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	ctx := context.Background()
	_, err := b.InventoryProgress(ctx, platform.Session{AccessToken: "tok_android"})
	require.NoError(t, err)
	_, err = b.InventoryProgress(ctx, platform.Session{AccessToken: "tok_tv", ClientID: ClientTV})
	require.NoError(t, err)

	assert.Equal(t, clientID, seen["OAuth tok_android"])
	assert.Equal(t, profileTV.ID, seen["OAuth tok_tv"])
}

// Review Focus #2: the heartbeat carries only the token; StartWatch must
// bind the session so the beacon uses the TV Client-Id.
func TestHeartbeatUsesBoundClient(t *testing.T) {
	var beaconClient string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/beacon":
			beaconClient = r.Header.Get("Client-Id")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet:
			// channel page scraped for spade_url
			_, _ = w.Write([]byte(`"spade_url":"` + "http://" + r.Host + `/beacon"`))
		default:
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"42"}}}`))
		}
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	b.c.beaconHostAllow = func(string) bool { return true } // test-only: see Step 4
	s := platform.Session{AccessToken: "tok_tv", ClientID: ClientTV}
	h, err := b.StartWatch(context.Background(), s, platform.Stream{Channel: "chan", ChannelID: "1", BroadcastID: "2", GameID: "3", Game: "G"})
	require.NoError(t, err)
	require.NoError(t, b.Heartbeat(context.Background(), h))
	assert.Equal(t, profileTV.ID, beaconClient)
}
```

Before relying on `TestHeartbeatUsesBoundClient`, read `resolveSpadeURL`, `hostAllowedForBeacon` and `fetchText` in `client.go` (lines ~401-520). If the channel-page URL or host-pin logic differs from what the test assumes, adapt the test server to the real URL shapes (e.g. `c.homeURL + "/" + channel`) and add the smallest test seam that already has a precedent in this file. Do not weaken the production host pin.

Also add a guard, per LCBRST/TwitchDropsMiner-CLI `8a7f516`: pages served to app-client profiles don't link the spade URL, so the channel page must always be the web page. In `resolveSpadeURL`, assert (via the test server's recorded request) that the channel page is fetched from `c.homeURL + "/" + channel`, the same as today, for a TV-bound token. In production `homeURL` is `https://www.twitch.tv`, and it must never be derived from the client profile. Add this assertion to `TestHeartbeatUsesBoundClient` by recording `r.URL.Path` of the GET and checking it equals `/chan`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/platform/twitch/ -run 'TestProfileFor|TestClientIDFollowsSession|TestHeartbeatUsesBoundClient' -v`
Expected: FAIL / compile errors (`profileFor`, `ClientTV`, `profileTV` undefined).

- [ ] **Step 4: Implement**

Create `internal/platform/twitch/profile.go`:

```go
package twitch

// ClientTV marks a session minted by the Twitch for TV OAuth client.
// Twitch blocked device-code login for the Android client ~2026-09-18
// (#48, DevilXD #1165); the TV client still accepts it.
const ClientTV = "tv"

type clientProfile struct {
	ID        string
	UserAgent string
}

var (
	profileAndroid = clientProfile{ID: clientID, UserAgent: userAgent}
	profileTV      = clientProfile{
		ID:        "ue6666qo983tsx6so1t0vnawi233wa",
		UserAgent: "Mozilla/5.0 (Linux; Android 9; SHIELD Android TV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36",
	}
)

// profileFor maps a persisted Session.ClientID to its request profile.
// Empty/unknown = legacy Android, so pre-#48 sessions are untouched.
func profileFor(id string) clientProfile {
	if id == ClientTV {
		return profileTV
	}
	return profileAndroid
}
```

In `client.go`, add to `type client struct`:

```go
	// profiles maps an OAuth token to the client profile it was minted
	// by. Populated by bind() at every Backend entry point; tokens are
	// unique per client, so the map is unambiguous.
	profiles sync.Map // token -> clientProfile
```

Add methods:

```go
// bind records which Twitch client s.AccessToken belongs to so every
// request made with that token (including heartbeats, which carry only
// the token) uses the matching Client-Id.
func (c *client) bind(s platform.Session) {
	if s.AccessToken == "" {
		return
	}
	c.profiles.Store(s.AccessToken, profileFor(s.ClientID))
}

func (c *client) profileForToken(token string) clientProfile {
	if v, ok := c.profiles.Load(token); ok {
		return v.(clientProfile)
	}
	return profileAndroid
}
```

In `setCommonHeaders`, replace the two hardcoded lines:

```go
	p := c.profileForToken(oauthToken)
	req.Header.Set("Client-Id", p.ID)
	req.Header.Set("User-Agent", p.UserAgent)
```

In `backend.go`, add `b.c.bind(s)` as the **first line** of every method that takes `s platform.Session`: `VerifyAuth`, `FetchAvatar`, `CampaignDetails`, `ListActiveCampaigns`, `ListEligibleChannels`, `InventoryProgress`, `StartWatch`, `Claim`, `AvailableDropIDs`, `CurrentSession`, `RefreshSession` (bind the *returned* session too; see Task 2). Check with `grep -n "s platform.Session" internal/platform/twitch/backend.go` that none is missed. If `internal/platform/twitch/browser_backend.go` builds requests through the same `client`, give it the same `bind` calls.

Beacon host-pin test seam (unexported, so only tests in this package can set it). Add to `type client struct`:

```go
	// beaconHostAllow overrides hostAllowedForBeacon in tests only (the
	// httptest server isn't a twitch.tv host). nil in production.
	beaconHostAllow func(rawURL string) bool
```
and as the first lines of `hostAllowedForBeacon`:

```go
	if c.beaconHostAllow != nil {
		return c.beaconHostAllow(rawURL)
	}
```
(Use the function's real parameter name.) Never loosen the production check itself.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/platform/twitch/ -v -run 'TestProfileFor|TestClientIDFollowsSession|TestHeartbeatUsesBoundClient' && go test ./...`
Expected: PASS, and the whole suite green.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ && git add -A internal/
git commit -m "feat(twitch): per-session client profile (Client-Id follows the minting client)"
```

---

### Task 2: Device login on the TV client; refresh keeps the client

**Files:**
- Modify: `internal/platform/twitch/auth.go:45-60,80-90,146-190,190-200`
- Test: `internal/platform/twitch/auth_test.go`

**Interfaces:**
- Consumes: `profileTV`, `profileFor`, `ClientTV`, `Session.ClientID` (Task 1).
- Produces: `authFlow.start` posts TV client_id; `authFlow.poll` returns `Session{ClientID: ClientTV}`; `authFlow.refresh` preserves `ClientID` and `Games`.

- [ ] **Step 1: Update the existing test and add new failing ones**

In `TestAuth_StartDeviceLogin_ParsesResponse`, change `assert.Equal(t, clientID, r.Form.Get("client_id"))` to `assert.Equal(t, profileTV.ID, r.Form.Get("client_id"))`. In `TestAuth_PollDeviceLogin_ReturnsSessionOnAccess`, add `assert.Equal(t, profileTV.ID, r.Form.Get("client_id"))` inside the handler and `assert.Equal(t, ClientTV, sess.ClientID)` after the poll.

Append:

```go
func TestAuth_StartDeviceLogin_ErrorIncludesTwitchBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"message":"invalid client"}`))
	}))
	defer srv.Close()
	a := &authFlow{deviceURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := a.start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client")
}

// Review Focus #1: a refreshed TV session must stay TV.
func TestAuth_RefreshKeepsClient(t *testing.T) {
	var gotClient string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotClient = r.Form.Get("client_id")
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"r2","expires_in":0}`))
	}))
	defer srv.Close()
	a := &authFlow{tokenURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	out, err := a.refresh(context.Background(), platform.Session{RefreshToken: "r1", ClientID: ClientTV, Games: []string{"Rust"}})
	require.NoError(t, err)
	assert.Equal(t, profileTV.ID, gotClient)
	assert.Equal(t, ClientTV, out.ClientID)
	assert.Equal(t, []string{"Rust"}, out.Games)

	_, err = a.refresh(context.Background(), platform.Session{RefreshToken: "r1"})
	require.NoError(t, err)
	assert.Equal(t, clientID, gotClient, "legacy session refreshes as Android")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/platform/twitch/ -run 'TestAuth_' -v`
Expected: FAIL (client_id mismatch, `ClientID` empty, error lacks body).

- [ ] **Step 3: Implement**

In `auth.go`:
- `start`: `"client_id": {profileTV.ID},`. Replace the status check with:

```go
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return platform.DeviceChallenge{}, fmt.Errorf("device authorize: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
```
- `poll`: `"client_id": {profileTV.ID},` and set `ClientID: ClientTV,` in the returned Session.
- `refresh`: `p := profileFor(s.ClientID)`, form `"client_id": {p.ID}`, and add `ClientID: s.ClientID, Games: s.Games,` to the returned Session.
- `postForm` takes the profile's UA: change its signature to `postForm(ctx, target string, form url.Values, ua string)` and pass `profileTV.UserAgent` from start/poll and `p.UserAgent` from refresh.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/platform/twitch/ -v -run 'TestAuth_' && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ && git add -A internal/
git commit -m "fix(twitch): device login via TV client; refresh keeps the session's client (#48)"
```

---

### Task 3: A null claim response is not a success

**Files:**
- Modify: `internal/platform/twitch/claim.go:14-18,43-48`
- Test: `internal/platform/twitch/claim_test.go`

**Interfaces:**
- Produces: `claimer.claim` returns a non-nil error when `claimDropRewards` is null.

- [ ] **Step 1: Write the failing test**

Append to `claim_test.go`:

```go
// Review Focus #5: Twitch answers a bad/missing instance id with
// claimDropRewards:null. That must not count as claimed.
func TestClaim_NullResultIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"claimDropRewards":null}}`))
	}))
	defer srv.Close()
	b := newForTest(srv.URL)
	err := b.Claim(context.Background(), platform.Session{AccessToken: "t"}, platform.DropBenefit{ID: "d1", InstanceID: "i1"})
	require.Error(t, err)
}
```
(Add the imports that `claim_test.go` is missing, matching `auth_test.go`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/platform/twitch/ -run TestClaim_NullResultIsError -v`
Expected: FAIL (err is nil).

- [ ] **Step 3: Implement**

In `claim.go`, make the result a pointer so null is detectable:

```go
type claimResult struct {
	ClaimDropRewards *struct {
		Status string `json:"status"`
	} `json:"claimDropRewards"`
}
```
and:

```go
	if out.ClaimDropRewards == nil {
		return fmt.Errorf("claim %s: twitch returned no claim result (unknown or invalid drop instance)", id)
	}
	switch out.ClaimDropRewards.Status {
	case "ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED", "":
		return nil
	default:
		return fmt.Errorf("claim status: %s", out.ClaimDropRewards.Status)
	}
```
(A present object with an empty status stays success: it's the pre-existing contract, and `claim_ok.json` fixtures rely on it.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/platform/twitch/ ./internal/watcher/ -v -run 'Claim' && go test ./...`
Expected: PASS. If a watcher test relied on null-as-success, fix its fixture to return a real status. Don't loosen the check.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ && git add -A internal/
git commit -m "fix(twitch): treat null claimDropRewards as a failed claim"
```

---

### Task 4: Channel-first discovery for TV sessions

**Files:**
- Create: `internal/platform/twitch/chandisc.go`
- Modify: `internal/platform/twitch/backend.go` (`ListActiveCampaigns`, `CampaignDetails`), `internal/platform/twitch/campaigns.go` (`inventoryData`)
- Test: `internal/platform/twitch/chandisc_test.go`

**Interfaces:**
- Consumes: `Session.ClientID`, `Session.Games`, `channels.listForGameDirectory(ctx, sess, slug) ([]platform.Stream, error)`, `gameslug.Slug(name) string`, `OpAvailableDrops`, `OpInventory`.
- Produces: `func (d *discovery) listByChannels(ctx context.Context, sess platform.Session, ch *channels) ([]platform.Campaign, map[string][]string, error)`, which returns campaigns plus campaignID→allowed logins.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/twitch/chandisc_test.go`:

```go
package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeGQL routes persisted ops by operationName.
func fakeGQL(t *testing.T, byOp map[string]func(vars map[string]any) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(raw, &req))
		f, ok := byOp[req.OperationName]
		if !ok {
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}
		_, _ = w.Write([]byte(f(req.Variables)))
	}))
}

const dirRust = `{"data":{"game":{"streams":{"edges":[
 {"node":{"id":"s1","viewersCount":10,"broadcaster":{"id":"c1","login":"alpha"},"game":{"id":"263490","displayName":"Rust"}}},
 {"node":{"id":"s2","viewersCount":5,"broadcaster":{"id":"c2","login":"beta"},"game":{"id":"263490","displayName":"Rust"}}}]}}}}`

const availAlpha = `{"data":{"channel":{"viewerDropCampaigns":[{"id":"campA","name":"Rust Isles General","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
 "timeBasedDrops":[
  {"id":"dWatch","name":"Box","requiredMinutesWatched":60,"requiredSubs":0,"benefitEdges":[{"benefit":{"id":"bW","name":"Box","imageAssetURL":"http://img/b.png"}}]},
  {"id":"dSub","name":"Sub Badge","requiredMinutesWatched":30,"requiredSubs":1,"benefitEdges":[{"benefit":{"id":"bS","name":"Badge","imageAssetURL":""}}]}]}]}}}`

const inventoryTV = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
 {"id":"campB","name":"Rust Isles Tac Gloves","game":{"id":"263490","name":"Rust"},"endAt":"2030-01-01T00:00:00Z",
  "allow":{"channels":[{"id":"c9","name":"welyn"}]},
  "timeBasedDrops":[{"id":"dG","name":"Gloves","requiredMinutesWatched":60,"requiredSubs":0,
    "benefitEdges":[{"benefit":{"id":"bG","name":"Gloves","imageAssetURL":""}}],
    "self":{"currentMinutesWatched":14,"isClaimed":false,"dropInstanceID":"i1"}}]}],
 "gameEventDrops":[]}}}}`

func TestListByChannels_TVSession(t *testing.T) {
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"DirectoryPage_Game": func(v map[string]any) string {
			if v["slug"] == "rust" {
				return dirRust
			}
			return `{"data":{"game":{"streams":{"edges":[]}}}}` // Review Focus #3
		},
		"DropsHighlightService_AvailableDrops": func(v map[string]any) string {
			if v["channelID"] == "c1" {
				return availAlpha
			}
			return `{"data":{"channel":{"viewerDropCampaigns":[]}}}`
		},
		"Inventory": func(map[string]any) string { return inventoryTV },
	})
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: "tv", ClientID: ClientTV, Games: []string{"Rust", "Offline Game"}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)

	byID := map[string]platform.Campaign{}
	for _, c := range camps {
		byID[c.ID] = c
	}
	require.Contains(t, byID, "campA")
	require.Contains(t, byID, "campB")

	a := byID["campA"]
	assert.Equal(t, "Rust", a.Game)
	assert.Equal(t, "active", a.Status)
	mins := map[string]int{}
	for _, bn := range a.Benefits {
		mins[bn.ID] = bn.RequiredMinutes
		assert.Equal(t, "campA", bn.CampaignID)
	}
	assert.Equal(t, 60, mins["dWatch"])
	assert.Equal(t, 0, mins["dSub"], "Review Focus #4: sub-gated drop is not watch-earnable")

	// Channel-seen campaigns are restricted to channels actually serving
	// them (alpha only; beta served nothing).
	assert.Equal(t, 1, b.AllowedChannelCount("campA"))
	// Inventory campaign keeps its own allow-list.
	assert.Equal(t, 1, b.AllowedChannelCount("campB"))
}

func TestListActiveCampaigns_LegacySessionUsesDashboard(t *testing.T) {
	called := map[string]bool{}
	srv := fakeGQL(t, map[string]func(map[string]any) string{
		"ViewerDropsDashboard": func(map[string]any) string {
			called["dash"] = true
			return `{"data":{"currentUser":{"dropCampaigns":[]}}}`
		},
		"DirectoryPage_Game": func(map[string]any) string { called["dir"] = true; return dirRust },
	})
	defer srv.Close()
	b := newForTest(srv.URL)
	_, err := b.ListActiveCampaigns(context.Background(), platform.Session{AccessToken: "a", Games: []string{"Rust"}})
	require.NoError(t, err)
	assert.True(t, called["dash"])
	assert.False(t, called["dir"], "Android sessions must not change discovery path")
}
```

Check the directory fixture against `gameDirectoryData` in `channels.go`, and the dashboard fixture against `campaignsData` in `campaigns.go`. Adjust the JSON (not the production structs) if field names differ. `resolveCurrentLogin` may also call a `CurrentUser` query; if the legacy test needs it, add `"CurrentUser"` to the fake map returning `{"data":{"currentUser":{"login":"me"}}}`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/platform/twitch/ -run 'TestListByChannels|TestListActiveCampaigns_Legacy' -v`
Expected: FAIL (TV session returns 0 campaigns).

- [ ] **Step 3: Extend the inventory decode**

In `campaigns.go` `inventoryData.CurrentUser.Inventory.DropCampaignsInProgress` element struct, add (keeping existing fields):

```go
				Name  string `json:"name"`
				EndAt string `json:"endAt"`
				Game  struct {
					Name string `json:"name"`
				} `json:"game"`
				Allow struct {
					Channels []struct {
						Name string `json:"name"`
					} `json:"channels"`
				} `json:"allow"`
```
and to each `TimeBasedDrops` element:

```go
					Name                   string `json:"name"`
					RequiredMinutesWatched int    `json:"requiredMinutesWatched"`
					RequiredSubs           int    `json:"requiredSubs"`
					BenefitEdges           []struct {
						Benefit struct {
							ID            string `json:"id"`
							Name          string `json:"name"`
							ImageAssetURL string `json:"imageAssetURL"`
						} `json:"benefit"`
					} `json:"benefitEdges"`
```
`inventory()` output is unchanged (it reads only `ID`/`Self`).

- [ ] **Step 4: Implement `chandisc.go`**

```go
package twitch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aalejandrofer/grubdrops/internal/gameslug"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// maxChannelsPerGame bounds the AvailableDrops fan-out per whitelisted
// game. The directory is sorted by viewers; campaigns a game runs show
// up on its top channels, and in-progress ones come from Inventory.
const maxChannelsPerGame = 10

// availableDropsFull decodes the full AvailableDrops payload. Unlike the
// dashboard/details queries, Twitch serves it to TV-client tokens.
type availableDropsFull struct {
	Channel struct {
		ViewerDropCampaigns []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			EndAt string `json:"endAt"`
			Game  struct {
				Name string `json:"name"`
			} `json:"game"`
			TimeBasedDrops []tvDrop `json:"timeBasedDrops"`
		} `json:"viewerDropCampaigns"`
	} `json:"channel"`
}

type tvDrop struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	RequiredMinutesWatched int    `json:"requiredMinutesWatched"`
	RequiredSubs           int    `json:"requiredSubs"`
	BenefitEdges           []struct {
		Benefit struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ImageAssetURL string `json:"imageAssetURL"`
		} `json:"benefit"`
	} `json:"benefitEdges"`
}

// toBenefits flattens drops the same way fetchDetails does, including
// the #47 rule: sub-gated drops report 0 minutes (not watch-earnable).
func toBenefits(campaignID string, drops []tvDrop) []platform.DropBenefit {
	var out []platform.DropBenefit
	for _, td := range drops {
		req := td.RequiredMinutesWatched
		if td.RequiredSubs > 0 {
			req = 0
		}
		for _, be := range td.BenefitEdges {
			out = append(out, platform.DropBenefit{
				ID: td.ID, CampaignID: campaignID, Name: be.Benefit.Name,
				RequiredMinutes: req, ImageURL: be.Benefit.ImageAssetURL, RewardID: be.Benefit.ID,
			})
		}
	}
	return out
}

// listByChannels discovers campaigns for sessions that can't see the
// drops dashboard (TV client). Directory(DROPS_ENABLED) per whitelisted
// game → AvailableDrops per top channel, merged with Inventory's
// in-progress campaigns (which AvailableDrops may omit, and which carry
// their own allow-lists). Returns campaigns + campaignID→allowed logins.
func (d *discovery) listByChannels(ctx context.Context, sess platform.Session, ch *channels) ([]platform.Campaign, map[string][]string, error) {
	camps := map[string]*platform.Campaign{}
	allowed := map[string][]string{}
	order := []string{}
	add := func(c platform.Campaign) *platform.Campaign {
		if ex, ok := camps[c.ID]; ok {
			return ex
		}
		cc := c
		camps[c.ID] = &cc
		order = append(order, c.ID)
		return &cc
	}

	for _, game := range sess.Games {
		streams, err := ch.listForGameDirectory(ctx, sess, gameslug.Slug(game))
		if err != nil {
			slog.Warn("tv discovery: directory failed", "game", game, "err", err)
			continue // one bad game must not sink the rest
		}
		if len(streams) > maxChannelsPerGame {
			streams = streams[:maxChannelsPerGame]
		}
		for _, s := range streams {
			var resp availableDropsFull
			if err := d.c.gql(ctx, sess.AccessToken, OpAvailableDrops, map[string]any{"channelID": s.ChannelID}, &resp); err != nil {
				slog.Warn("tv discovery: available drops failed", "channel", s.Channel, "err", err)
				continue
			}
			for _, vc := range resp.Channel.ViewerDropCampaigns {
				c := add(platform.Campaign{
					ID: vc.ID, Platform: "twitch", Game: vc.Game.Name, Name: vc.Name,
					EndsAt: parseISO(vc.EndAt), Status: "active", Kind: "drop",
					// Link state is unknowable without DropCampaignDetails;
					// optimistic like scrape-sourced campaigns.
					AccountLinked: true, AccountLinkChecked: false,
					Benefits: toBenefits(vc.ID, vc.TimeBasedDrops),
				})
				_ = c
				allowed[vc.ID] = appendUnique(allowed[vc.ID], s.Channel)
			}
		}
	}

	var inv inventoryData
	if err := d.c.gql(ctx, sess.AccessToken, OpInventory, nil, &inv); err != nil {
		return nil, nil, fmt.Errorf("tv discovery inventory: %w", err)
	}
	for _, ic := range inv.CurrentUser.Inventory.DropCampaignsInProgress {
		drops := make([]tvDrop, 0, len(ic.TimeBasedDrops))
		for _, td := range ic.TimeBasedDrops {
			t := tvDrop{ID: td.ID, Name: td.Name, RequiredMinutesWatched: td.RequiredMinutesWatched, RequiredSubs: td.RequiredSubs}
			for _, be := range td.BenefitEdges {
				t.BenefitEdges = append(t.BenefitEdges, be)
			}
			drops = append(drops, t)
		}
		add(platform.Campaign{
			ID: ic.ID, Platform: "twitch", Game: ic.Game.Name, Name: ic.Name,
			EndsAt: parseISO(ic.EndAt), Status: "active", Kind: "drop",
			AccountLinked: true, AccountLinkChecked: false,
			Benefits: toBenefits(ic.ID, drops),
		})
		if len(ic.Allow.Channels) > 0 {
			var logins []string
			for _, c := range ic.Allow.Channels {
				logins = appendUnique(logins, c.Name)
			}
			allowed[ic.ID] = logins // Twitch's own allow-list wins
		}
	}

	out := make([]platform.Campaign, 0, len(order))
	for _, id := range order {
		out = append(out, *camps[id])
	}
	return out, allowed, nil
}

func appendUnique(xs []string, x string) []string {
	for _, e := range xs {
		if e == x {
			return xs
		}
	}
	return append(xs, x)
}
```

`t.BenefitEdges = append(t.BenefitEdges, be)` only compiles if the inventory benefit-edge struct type is identical to `tvDrop`'s. If Go rejects it (distinct anonymous struct types with tags are assignable only when identical), declare a named `type tvBenefitEdge struct{...}` and use it in both `tvDrop` and the inventory decode. Remove the `_ = c` line if the linter flags it; it exists only because `add` returns the pointer for future use.

- [ ] **Step 5: Route by client in `backend.go`**

In `ListActiveCampaigns`, after `b.c.bind(s)`:

```go
	if s.ClientID == ClientTV {
		camps, allowed, err := b.disc.listByChannels(ctx, s, b.chans)
		if err != nil {
			return nil, err
		}
		b.mu.Lock()
		for cid, logins := range allowed {
			b.allowedLoginsByCampaign[cid] = logins
		}
		b.mu.Unlock()
		b.ensurePubSub(s)
		return camps, nil
	}
```
In `CampaignDetails` (used by /drops lazy item fetch), for a TV session return the benefits of the matching campaign from a fresh `listByChannels` result (or nil, nil if not found) instead of calling `DropCampaignDetails`, which returns null for TV tokens.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/platform/twitch/ -v -run 'TestListByChannels|TestListActiveCampaigns_Legacy' && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/ && git add -A internal/
git commit -m "feat(twitch): channel-first discovery for TV-client sessions (#48)"
```

---

### Task 5: Plumb whitelist names into Session.Games

**Files:**
- Modify: `internal/discovery/twitch.go:101`, `internal/watcher/watcher.go` (Config + New), `cmd/miner/main.go:430-480,920-960`
- Test: `internal/discovery/providers_test.go` (or the existing twitch scraper test file), `internal/watcher/watcher_test.go`

**Interfaces:**
- Consumes: `Session.Games` (Task 1).
- Produces: `watcher.Config.Games []string`; `loadAccountWhitelist` returns `(allow func(string) bool, rank func(string) int, names []string, err error)`.

- [ ] **Step 1: Write failing tests**

In the discovery test file, add a test with a fake `platform.Backend` whose `ListActiveCampaigns` records `s.Games`. Call `NewTwitchScraper(fake, src).Scrape(ctx, []string{"rust","apex legends"})` and assert the recorded `Games` equals the whitelist. Follow the existing fake-backend pattern in that file; `grep -n "ListActiveCampaigns" internal/discovery/*_test.go` shows it.

In `watcher_test.go`, assert that `New(Config{..., Games: []string{"Rust"}})` yields a session with `Games == []string{"Rust"}` when `Session.Games` was nil. Use the existing test constructor helper in that file (`grep -n "func newTestWatcher\|watcher.New(" internal/watcher/watcher_test.go`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/discovery/ ./internal/watcher/ -v -run Games`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `internal/discovery/twitch.go` Scrape: after `sess.GameFilter = buildAllowList(whitelist)` add `sess.Games = append([]string(nil), whitelist...)`.
- `watcher.Config`: add `Games []string // whitelisted game names; feeds Session.Games for TV-client discovery`. In `New`, next to the GameFilter line: `if cfg.Session.Games == nil { cfg.Session.Games = cfg.Games }`.
- `cmd/miner/main.go` `loadAccountWhitelist`: also return `names` (each row's `r.Name`); update all callers (`grep -n loadAccountWhitelist cmd/`) and pass `Games: names` into `watcher.Config`.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w . && git add -A
git commit -m "feat: plumb whitelisted game names into Session.Games"
```

---

### Task 6: Staging live verification (manual, user present)

Follow memories `feedback_test_on_staging` and `project_backlog_staging_env`: local amd64 build, or the host build if local Docker is down. Staging has a fresh DB with no Twitch accounts.

- [ ] **Step 1:** Build and deploy the branch to `staging.drops.ryuzec.dev`; verify a fresh `Up` and healthz 200.
- [ ] **Step 2:** Log in to staging, add a Twitch account via device code (the user approves on twitch.tv/activate). Expect success and no "Device Error HTTP 400". Check that the stored session has `client_id":"tv"`: decrypt on the host, don't print the token; checking for the key is enough.
- [ ] **Step 3:** Whitelist 2–3 games with live drops. The user **disables that account on prod** so its beacons don't mix in (memory: prod beacons get credited to whichever channel was last set as the current drop session). Within 10 min, `/drops` lists campaigns, the watcher picks a watch drop (never one with `requiredSubs>0`), and Inventory minutes climb.
- [ ] **Step 4:** Leave it running until one drop completes. Confirm the claim notification, and that the drop leaves in-progress and appears as claimed.
- [ ] **Step 5:** Re-enable the account on prod, then `docker compose down` staging (memory `feedback_staging_teardown`).
- [ ] **Step 6:** Report results. Do not tag or release until the user approves.

---

## Out of scope (Plan B, separate)

- Retire the scrape-synth campaigns, reward-ID dedupe, ghost-skip overrides and manual mark-collected, in favour of Inventory as the only claim/progress truth.
- Move Android sessions to channel-first discovery too (one discovery path).
- Accrual-rate investigation: TV measured ~0.75× then ~0.33× in the 2026-09-27 run. Compare with the Android control (`ctrl.py`) before changing cadence.
- Abu's streamer-priority fallback and auth-check retry.
