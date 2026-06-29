# Channel whitelist for null-game drops — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator opt an account into a Kick/Twitch *channel* so drops with no game category (e.g. Kick Football drops) get mined.

**Architecture:** Mining is gated by one filter line in the watcher (`AllowGame(c.Game)`). Add an OR channel-match backed by a new `account_channels` table. Surface null-game active drops in a dedicated `/drops` section with a per-account WHITELIST+ that writes a channel row. The campaign's channel(s) are persisted into the existing unused `raw_json` column so the UI knows what to offer.

**Tech Stack:** Go, SQLite via sqlc (`internal/store/gen`) + goose migrations, `html/template` + HTMX, `internal/i18n` JSON locales, testify.

## Global Constraints

- After any edit: `go build ./...` AND `go test ./...` must pass. Run `gofmt -w .` before every commit (CI gofmt gate fails fast).
- sqlc: never put `?` or parentheses in a `queries/*.sql` comment (corrupts placeholder rewrite). Regenerate with `cd internal/store && sqlc generate` after touching `queries/` or `migrations/`.
- i18n: every user-facing string goes through `{{t "key"}}` (templates) / `i18n.T(lang, key)` (Go). Add each new key to **every** file under `internal/i18n/locales/` — locales must keep equal key sets. NOTE: another agent is concurrently adding a Spanish locale; do the i18n step LAST, and re-list `internal/i18n/locales/` immediately before editing so new files (e.g. `es.json`) are included.
- Do not delete the browser sidecar stack. Twitch keeps `Client-Integrity` OFF. `HeartbeatInterval` stays 60s. None of this plan touches those.
- Branch: work is on `feat/channel-whitelist-null-game`. Commit per task.
- Whitelist key = channel slug, lowercased. Per-account only (no global channel list). Flat list (no rank UI; `rank` column stored as 0).

---

### Task 1: `account_channels` storage

**Files:**
- Create: `internal/store/migrations/0013_account_channels.sql`
- Create: `internal/store/queries/channels.sql`
- Regen: `internal/store/gen/*` (via sqlc)
- Test: `internal/store/channels_queries_test.go`

**Interfaces:**
- Produces (sqlc-generated, consumed by Tasks 3 & 5):
  - `gen.ListAccountChannels(ctx, accountID string) ([]gen.ListAccountChannelsRow, error)` where `ListAccountChannelsRow{Channel string; Rank int64}`
  - `gen.AddAccountChannel(ctx, gen.AddAccountChannelParams{AccountID string; Channel string; Rank int64}) error`
  - `gen.RemoveAccountChannel(ctx, gen.RemoveAccountChannelParams{AccountID string; Channel string}) error`
  - `gen.ClearAccountChannels(ctx, accountID string) error`

- [ ] **Step 1: Write the migration**

Create `internal/store/migrations/0013_account_channels.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE account_channels (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL,
    rank        INTEGER NOT NULL,
    PRIMARY KEY (account_id, channel)
);

CREATE INDEX idx_account_channels_acct ON account_channels(account_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_channels_acct;
DROP TABLE IF EXISTS account_channels;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the queries**

Create `internal/store/queries/channels.sql` (no `?` or parentheses in comments):

```sql
-- name: ListAccountChannels :many
SELECT channel, rank
FROM account_channels
WHERE account_id = ?
ORDER BY rank ASC, channel ASC;

-- name: AddAccountChannel :exec
INSERT INTO account_channels (account_id, channel, rank)
VALUES (?, ?, ?)
ON CONFLICT(account_id, channel) DO UPDATE SET rank = excluded.rank;

-- name: RemoveAccountChannel :exec
DELETE FROM account_channels WHERE account_id = ? AND channel = ?;

-- name: ClearAccountChannels :exec
DELETE FROM account_channels WHERE account_id = ?;
```

- [ ] **Step 3: Regenerate sqlc**

Run: `cd internal/store && sqlc generate`
Expected: no errors; `internal/store/gen/` now contains `ListAccountChannels`, `AddAccountChannel`, `RemoveAccountChannel`, `ClearAccountChannels`.

- [ ] **Step 4: Write the failing test**

Create `internal/store/channels_queries_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestQueries_AccountChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	now := time.Now().Unix()

	_, err := q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "k",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "adrianozendejas32", Rank: 0,
	}))
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "xqc", Rank: 0,
	}))
	// Upsert same channel must not duplicate (PK conflict path).
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "xqc", Rank: 0,
	}))

	rows, err := q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	got := []string{}
	for _, r := range rows {
		got = append(got, r.Channel)
	}
	assert.ElementsMatch(t, []string{"adrianozendejas32", "xqc"}, got)

	require.NoError(t, q.RemoveAccountChannel(ctx, gen.RemoveAccountChannelParams{
		AccountID: "acc-1", Channel: "xqc",
	}))
	rows, err = q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "adrianozendejas32", rows[0].Channel)

	require.NoError(t, q.ClearAccountChannels(ctx, "acc-1"))
	rows, err = q.ListAccountChannels(ctx, "acc-1")
	require.NoError(t, err)
	assert.Empty(t, rows)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store -run TestQueries_AccountChannels -v`
Expected: PASS (migration applies, queries work).

- [ ] **Step 6: Format + commit**

```bash
gofmt -w .
git add internal/store/migrations/0013_account_channels.sql internal/store/queries/channels.sql internal/store/gen internal/store/channels_queries_test.go
git commit -m "feat(store): account_channels table + queries

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Persist a campaign's channels into `raw_json`

**Files:**
- Modify: `internal/store/campaign_persister.go` (the `UpsertCampaign` call, currently `RawJson: "{}"`)
- Test: `internal/store/campaign_persister_test.go` (add a test)

**Interfaces:**
- Produces: persisted campaigns carry `raw_json = {"allowed_channels":[...]}` (lowercased slugs). Task 4 parses this back. JSON shape: `{"allowed_channels": ["adrianozendejas32"]}`. Empty/absent channels persist `{}`.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/campaign_persister_test.go`:

```go
func TestCampaignPersister_PersistsAllowedChannels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drops.db")
	db, err := Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := gen.New(db)
	p := NewCampaignPersister(q)
	now := time.Now()

	camps := []platform.Campaign{{
		ID: "c-football", Platform: "kick", Game: "", Name: "Football Drop",
		Status: "active", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
		AllowedChannels: []string{"Adrianozendejas32"},
		Benefits: []platform.DropBenefit{
			{ID: "b1", CampaignID: "c-football", Name: "Jersey", RequiredMinutes: 600},
		},
	}}
	require.NoError(t, p.PersistCampaigns(context.Background(), camps))

	cur, err := q.ListCurrentCampaigns(context.Background(), gen.ListCurrentCampaignsParams{
		StartsAt: now.Unix(), EndsAt: now.Unix(), Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, cur, 1)

	var meta struct {
		AllowedChannels []string `json:"allowed_channels"`
	}
	require.NoError(t, json.Unmarshal([]byte(cur[0].RawJson), &meta))
	// Stored lowercased.
	assert.Equal(t, []string{"adrianozendejas32"}, meta.AllowedChannels)
}
```

Add `"encoding/json"` to the test file's imports if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run TestCampaignPersister_PersistsAllowedChannels -v`
Expected: FAIL — `RawJson` is `"{}"`, unmarshal yields empty slice.

- [ ] **Step 3: Implement raw_json marshalling**

In `internal/store/campaign_persister.go`, replace the `RawJson: "{}",` line in the `UpsertCampaign` params with a computed value. Just before the `UpsertCampaign` call, add:

```go
rawJSON := "{}"
if len(c.AllowedChannels) > 0 {
	chans := make([]string, 0, len(c.AllowedChannels))
	for _, ch := range c.AllowedChannels {
		ch = strings.ToLower(strings.TrimSpace(ch))
		if ch != "" {
			chans = append(chans, ch)
		}
	}
	if len(chans) > 0 {
		if b, err := json.Marshal(struct {
			AllowedChannels []string `json:"allowed_channels"`
		}{AllowedChannels: chans}); err == nil {
			rawJSON = string(b)
		}
	}
}
```

Then set `RawJson: rawJSON,` in the params. Ensure `encoding/json` and `strings` are imported in `campaign_persister.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run TestCampaignPersister -v`
Expected: PASS (both the existing persister test and the new one).

- [ ] **Step 5: Format + commit**

```bash
gofmt -w .
git add internal/store/campaign_persister.go internal/store/campaign_persister_test.go
git commit -m "feat(store): persist campaign allowed_channels into raw_json

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Watcher channel matching

**Files:**
- Modify: `internal/watcher/watcher.go` (add `Config.AllowChannel`; change the whitelist filter near line 815-824)
- Modify: `cmd/miner/main.go` (add `loadAccountChannels`; wire `AllowChannel` into `watcher.New`)
- Test: `internal/watcher/watcher_test.go` (mining behavior)
- Test: `cmd/miner/main_test.go` (create; `loadAccountChannels`)

**Interfaces:**
- Consumes: `gen.ListAccountChannels` (Task 1).
- Produces:
  - `watcher.Config.AllowChannel func(channels []string) bool` — nil when the account has no channel whitelist.
  - `loadAccountChannels(ctx context.Context, q *gen.Queries, accountID string) (func([]string) bool, error)` — returns `(nil, nil)` when the account has no channel rows.

- [ ] **Step 1: Write the failing watcher test**

Append to `internal/watcher/watcher_test.go`:

```go
// nullGameBackend returns a single ACTIVE campaign with no game and one
// participating channel — the Kick "Football Drop" shape. ListEligibleChannels
// is inherited from MockBackend (returns a live stream), so a campaign that
// passes the filter will accrue heartbeats.
type nullGameBackend struct{ *platformtest.MockBackend }

func (n *nullGameBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "c-football", Game: "", Status: "active", Name: "Football Drop",
		AllowedChannels: []string{"adrianozendejas32"},
		Benefits: []platform.DropBenefit{
			{ID: "drop1", CampaignID: "c-football", Name: "Jersey", RequiredMinutes: 2},
		},
	}}, nil
}

func TestWatcher_MinesNullGameWhenChannelWhitelisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	backend := &nullGameBackend{platformtest.New()}
	w := New(Config{
		AccountID:    "acc1",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" }, // null game NOT whitelisted
		AllowChannel: func(chs []string) bool {
			for _, c := range chs {
				if c == "adrianozendejas32" {
					return true
				}
			}
			return false
		},
	})
	_ = w.Run(ctx)
	assert.Greater(t, backend.Heartbeats(), int64(0),
		"null-game campaign with a whitelisted channel must be mined")
}

func TestWatcher_SkipsNullGameWhenChannelNotWhitelisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	backend := &nullGameBackend{platformtest.New()}
	w := New(Config{
		AccountID:    "acc1",
		Backend:      backend,
		Session:      platform.Session{AccessToken: "tok"},
		Notifier:     &recordingNotifier{},
		TickInterval: 5 * time.Millisecond,
		AllowGame:    func(g string) bool { return g == "Rust" },
		AllowChannel: func(chs []string) bool { return false },
	})
	_ = w.Run(ctx)
	assert.Equal(t, int64(0), backend.Heartbeats(),
		"null-game campaign with no matching channel must not be mined")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/watcher -run TestWatcher_MinesNullGame -v`
Expected: FAIL — `Config` has no `AllowChannel` field (compile error) or, once the field exists but filter unchanged, 0 heartbeats.

- [ ] **Step 3: Add the Config field**

In `internal/watcher/watcher.go`, in the `Config` struct just after the `AllowGame func(game string) bool` field (and its doc comment), add:

```go
	// AllowChannel returns true if a campaign whose AllowedChannels
	// include one of the account's whitelisted channels should be
	// mined, even when its Game is not whitelisted (or empty). This is
	// how null-game drops (Kick Football drops with no category) are
	// opted into per account. Nil when the account has no channel
	// whitelist.
	AllowChannel func(channels []string) bool
```

- [ ] **Step 4: Change the whitelist filter**

In `internal/watcher/watcher.go`, replace the existing whitelist block (currently around lines 814-824):

```go
	var whitelisted []platform.Campaign
	if w.cfg.AllowGame != nil {
		whitelisted = make([]platform.Campaign, 0, len(campaigns))
		for _, c := range campaigns {
			if w.cfg.AllowGame(c.Game) {
				whitelisted = append(whitelisted, c)
			}
		}
	} else {
		whitelisted = campaigns
	}
```

with:

```go
	var whitelisted []platform.Campaign
	if w.cfg.AllowGame != nil || w.cfg.AllowChannel != nil {
		whitelisted = make([]platform.Campaign, 0, len(campaigns))
		for _, c := range campaigns {
			gameOK := w.cfg.AllowGame != nil && w.cfg.AllowGame(c.Game)
			chanOK := w.cfg.AllowChannel != nil && w.cfg.AllowChannel(c.AllowedChannels)
			if gameOK || chanOK {
				whitelisted = append(whitelisted, c)
			}
		}
	} else {
		whitelisted = campaigns
	}
```

- [ ] **Step 5: Run watcher tests to verify they pass**

Run: `go test ./internal/watcher -run TestWatcher -v`
Expected: PASS (new null-game tests + all existing watcher tests, no regression).

- [ ] **Step 6: Write the failing loadAccountChannels test**

Create `cmd/miner/main_test.go`:

```go
package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestLoadAccountChannels(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/t.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	now := time.Now().Unix()

	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "kick", DisplayName: "k",
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	// No channels yet -> nil closure.
	allow, err := loadAccountChannels(ctx, q, "acc-1")
	require.NoError(t, err)
	assert.Nil(t, allow)

	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{
		AccountID: "acc-1", Channel: "adrianozendejas32", Rank: 0,
	}))

	allow, err = loadAccountChannels(ctx, q, "acc-1")
	require.NoError(t, err)
	require.NotNil(t, allow)
	// Case-insensitive match against a campaign's AllowedChannels.
	assert.True(t, allow([]string{"Adrianozendejas32"}))
	assert.False(t, allow([]string{"xqc"}))
	assert.False(t, allow(nil))
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test ./cmd/miner -run TestLoadAccountChannels -v`
Expected: FAIL — `loadAccountChannels` undefined.

- [ ] **Step 8: Implement loadAccountChannels + wire it**

In `cmd/miner/main.go`, add the function (place it next to `loadAccountWhitelist`):

```go
// loadAccountChannels materialises the per-account channel whitelist
// into a match closure the watcher consumes. Returns a nil closure when
// the account has no channel rows (so the watcher's filter ignores it).
// Used to mine null-game drops the user has opted an account into.
func loadAccountChannels(ctx context.Context, q *gen.Queries, accountID string) (func([]string) bool, error) {
	rows, err := q.ListAccountChannels(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("list account channels: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	set := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		set[strings.ToLower(strings.TrimSpace(r.Channel))] = struct{}{}
	}
	allow := func(channels []string) bool {
		for _, ch := range channels {
			if _, ok := set[strings.ToLower(strings.TrimSpace(ch))]; ok {
				return true
			}
		}
		return false
	}
	return allow, nil
}
```

Then, where `loadAccountWhitelist` is called for an account (the same place `allow, rank` are derived before `watcher.New`), add:

```go
		allowChannel, err := loadAccountChannels(ctx, q, a.ID)
		if err != nil {
			return nil, err
		}
```

(Match the surrounding error-handling style; the call site returns `(nil, err)` for `loadAccountWhitelist` — mirror it. Use the same `q`/`ctx`/account-id variables already in scope there.)

And add to the `watcher.New(watcher.Config{...})` literal, next to `AllowGame: allow, GameRank: rank,`:

```go
		AllowChannel: allowChannel,
```

- [ ] **Step 9: Run tests + build**

Run: `go build ./... && go test ./cmd/miner ./internal/watcher -v`
Expected: build OK; PASS.

- [ ] **Step 10: Format + commit**

```bash
gofmt -w .
git add internal/watcher/watcher.go internal/watcher/watcher_test.go cmd/miner/main.go cmd/miner/main_test.go
git commit -m "feat(watcher): mine null-game drops via per-account channel whitelist

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: /drops "Discoverable — Null Game" section + channel whitelist handler

**Files:**
- Modify: `internal/api/handlers_drops.go` (`dropsRow.Channels`; parse `raw_json`; partition null-game active rows into `dropsPage.NullGameRows`; new `addChannelWhitelist` handler)
- Modify: `internal/api/server.go` (register `POST /drops/whitelist/channel`)
- Modify: `internal/web/templates/_drops_table.html` (new section)
- Test: `internal/api/handlers_drops_channel_test.go` (create)

**Interfaces:**
- Consumes: `gen.AddAccountChannel` (Task 1); `raw_json` channels persisted (Task 2); `dropsPage`, `dropsRow`, `dropsAccount`, `collectAll`, `addWhitelist` (existing).
- Produces: `POST /drops/whitelist/channel` with form fields `account_id` (string) and one-or-more `channel` (string) → inserts `account_channels` rows; `dropsPage.NullGameRows []dropsRow`; `dropsRow.Channels []string`.

- [ ] **Step 1: Write the failing handler test**

Look at `internal/api/handlers_login_kick_test.go` and `internal/api/handlers_accounts_toggle_test.go` for the existing handler-test harness (how `dropsDeps`/router + a temp DB are built). Create `internal/api/handlers_drops_channel_test.go` following that harness. The test must:

```go
// Posting account_id + channel(s) to /drops/whitelist/channel inserts the
// rows into account_channels for that account.
func TestAddChannelWhitelist_InsertsRows(t *testing.T) {
	// ARRANGE: temp DB (store.Open), gen.New(db), create an account "acc-1".
	//   d := &dropsDeps{q: q, ...}  // mirror how other drops tests build it;
	//   reload can be nil if guarded, else a no-op func.
	// ACT: build an httptest request:
	//   form := url.Values{}
	//   form.Set("account_id", "acc-1")
	//   form.Add("channel", "adrianozendejas32")
	//   POST it through the handler (d.addChannelWhitelist) with a valid CSRF
	//   token if the test harness requires it (see existing drops tests).
	// ASSERT:
	//   rows, _ := q.ListAccountChannels(ctx, "acc-1")
	//   require.Len(t, rows, 1)
	//   assert.Equal(t, "adrianozendejas32", rows[0].Channel)
}
```

Fill in the body using the same constructor + CSRF pattern the other `internal/api` handler tests use (do not invent a new harness). The assertion content above is mandatory.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestAddChannelWhitelist -v`
Expected: FAIL — `addChannelWhitelist` undefined.

- [ ] **Step 3: Implement the handler**

In `internal/api/handlers_drops.go`, add (mirroring `addWhitelist` at the per-account branch, but for channels — no `__global__`):

```go
// addChannelWhitelist takes (account_id, channel[]) from the null-game
// section on /drops and opts that account into the channel(s). Null-game
// drops (Kick Football drops with no category) are mined when one of
// their AllowedChannels is on an account's channel whitelist.
func (d *dropsDeps) addChannelWhitelist(w http.ResponseWriter, r *http.Request) {
	accID := r.FormValue("account_id")
	if accID == "" {
		http.Redirect(w, r, "/drops", http.StatusSeeOther)
		return
	}
	if _, err := d.q.GetAccount(r.Context(), accID); err != nil {
		http.NotFound(w, r)
		return
	}
	seen := map[string]struct{}{}
	for _, raw := range r.Form["channel"] {
		ch := strings.ToLower(strings.TrimSpace(raw))
		if ch == "" {
			continue
		}
		if _, dup := seen[ch]; dup {
			continue
		}
		seen[ch] = struct{}{}
		if err := d.q.AddAccountChannel(r.Context(), gen.AddAccountChannelParams{
			AccountID: accID, Channel: ch, Rank: 0,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	d.applyReload(r.Context()) // mirror addWhitelist's reload; omit if addWhitelist does not call it
	http.Redirect(w, r, "/drops", http.StatusSeeOther)
}
```

Note: `addWhitelist` ends with a plain `http.Redirect` and does NOT itself call a reload helper in the snippet reviewed — check the actual code. If `dropsDeps` exposes a reload (it has a `reload` field), call it the same way other mutating drops handlers do; otherwise drop that line. The scheduler will also pick up the new channel on its next reload regardless.

- [ ] **Step 4: Register the route**

In `internal/api/server.go`, next to `authed.Post("/drops/whitelist/add", dropsH.addWhitelist)`, add:

```go
	authed.Post("/drops/whitelist/channel", dropsH.addChannelWhitelist)
```

- [ ] **Step 5: Run handler test to verify it passes**

Run: `go test ./internal/api -run TestAddChannelWhitelist -v`
Expected: PASS.

- [ ] **Step 6: Add `Channels` to dropsRow + `NullGameRows` to dropsPage**

In `internal/api/handlers_drops.go`:

In the `dropsRow` struct, add:

```go
	// Channels are the campaign's participating channel slugs (parsed
	// from raw_json). Used by the null-game section's WHITELIST+ form.
	Channels []string
```

In the `dropsPage` struct, add (after `UnlistedRows`):

```go
	// NullGameRows are ACTIVE campaigns with no game category (e.g. Kick
	// Football drops). They can't be game-whitelisted, so they get a
	// dedicated section whose WHITELIST+ opts an account into the
	// campaign's channel. Only populated for the current tab.
	NullGameRows []dropsRow
```

- [ ] **Step 7: Parse channels + partition null-game rows**

The unlisted rows are produced by `collectAll` (campaigns whose game is not whitelisted). A null-game campaign has empty `Game`, so it falls into the unlisted set. Add a helper to parse channels from a campaign's `raw_json`, and in `list()` partition the unlisted current rows.

First, add a parser near the top of `handlers_drops.go`:

```go
// channelsFromRawJSON extracts the persisted allowed_channels list from a
// campaign row's raw_json (written by the campaign persister). Returns nil
// on any parse miss.
func channelsFromRawJSON(raw string) []string {
	if raw == "" || raw == "{}" {
		return nil
	}
	var meta struct {
		AllowedChannels []string `json:"allowed_channels"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil
	}
	return meta.AllowedChannels
}
```

Ensure `encoding/json` is imported in `handlers_drops.go`.

Next, the `dropsRow` values built in `collectAll` need their `Channels` set from the source campaign's `RawJson`. In `collectAll`, where each `dropsRow` is constructed from a campaign row (the same place `Game`, `CampaignName`, `CampaignID` are set), add:

```go
		Channels: channelsFromRawJSON(<campaignRow>.RawJson),
```

(Use the actual loop variable for the campaign row; it has a `RawJson` field because the list queries `SELECT *`.)

Finally, in `list()`, after `unlistedRows` is returned and before building `page`, split the null-game ones out (only meaningful for the current tab, where active drops live):

```go
	var nullGameRows []dropsRow
	if tab == tabCurrent {
		kept := unlistedRows[:0]
		for _, row := range unlistedRows {
			if strings.TrimSpace(row.Game) == "" && len(row.Channels) > 0 {
				nullGameRows = append(nullGameRows, row)
			} else {
				kept = append(kept, row)
			}
		}
		unlistedRows = kept
	}
```

Then set `NullGameRows: nullGameRows,` in the `dropsPage` literal.

- [ ] **Step 8: Add the template section**

In `internal/web/templates/_drops_table.html`, insert a new `<section>` BETWEEN the `{{if .UnlinkedRows}}...{{end}}` block (ends at the line with `{{end}}` before the Discoverable section) and the Discoverable `<section>`:

```html
    {{if .NullGameRows}}
    <section class="drops-pane">
      <header class="drops-pane-h">
        <h3>{{t "drops_table.null_game"}}</h3>
        <span class="meta">{{len .NullGameRows}}</span>
      </header>
      <div class="events drops-rows scroll-cap">
        {{$accts := .Accounts}}
        {{$csrf := .CSRFToken}}
        {{range .NullGameRows}}
        <details class="ev kind-discovery ev-linkrow"
                 {{if .CampaignID}}hx-get="/drops/campaigns/{{.CampaignID}}/items"
                 hx-target="find .ev-detail-slot"
                 hx-trigger="toggle once from:closest details"
                 hx-swap="innerHTML"{{end}}>
          <summary>
            <span class="chev" aria-hidden="true">›</span>
            <span class="t">{{if .When}}{{.When}}{{else}}—{{end}}</span>
            <span class="lvl" style="color:var(--{{if eq .Platform "twitch"}}purple{{else if eq .Platform "kick"}}kick{{else}}muted{{end}})">●</span>
            <span class="body"><em style="color:var(--{{if eq .Platform "twitch"}}purple{{else if eq .Platform "kick"}}kick{{else}}muted{{end}})">{{.Platform}}</em> · <span style="color:var(--muted);">{{t "drops_table.no_game"}}</span> · {{.CampaignName}}</span>
            <span class="ac whitelist-add" onclick="event.stopPropagation()">
              {{if $accts}}
              <form method="post" action="/drops/whitelist/channel" class="disc-add-form" onsubmit="this.querySelector('button').disabled=true">
                <input type="hidden" name="csrf_token" value="{{$csrf}}">
                {{range .Channels}}<input type="hidden" name="channel" value="{{.}}">{{end}}
                <select name="account_id" onclick="event.stopPropagation()" title="{{t "drops_table.whitelist_add"}}">
                  {{range $accts}}<option value="{{.ID}}">{{.Label}}</option>{{end}}
                </select>
                <button class="btn sm" type="submit" onclick="event.stopPropagation()">{{t "drops_table.whitelist_add"}}</button>
              </form>
              {{else}}<span style="color:var(--muted);">{{t "drops_table.no_accounts"}}</span>{{end}}
            </span>
          </summary>
          <div class="ev-detail-slot">
            {{if .CampaignID}}
            <div class="ev-detail" style="color:var(--muted);font-size:11px;">{{t "drops_table.loading"}}</div>
            {{else}}
            <div class="ev-detail" style="color:var(--muted);font-size:11px;">{{t "drops_table.no_item_details"}}</div>
            {{end}}
          </div>
        </details>
        {{end}}
      </div>
    </section>
    {{end}}
```

(The `drops_table.null_game` i18n key is added in Task 6; the others — `no_game`, `whitelist_add`, `no_accounts`, `loading`, `no_item_details` — already exist.)

- [ ] **Step 9: Build + run API tests + manual smoke**

Run: `go build ./... && go test ./internal/api ./internal/store ./internal/watcher`
Expected: build OK; PASS. (Template missing-key for `null_game` will not break Go tests; it is added in Task 6. If a template-render test fails on the missing key, do Task 6 step for that key now.)

- [ ] **Step 10: Format + commit**

```bash
gofmt -w .
git add internal/api/handlers_drops.go internal/api/server.go internal/web/templates/_drops_table.html internal/api/handlers_drops_channel_test.go
git commit -m "feat(drops): null-game section with per-account channel whitelist

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Account settings channel editor

**Files:**
- Modify: `internal/api/handlers_accounts.go` (`addChannel`, `removeChannel`; load channels into the account-detail page data)
- Modify: `internal/api/server.go` (register the two routes)
- Modify: `internal/web/templates/accounts_detail.html` (Channels block)
- Test: `internal/api/handlers_accounts_channels_test.go` (create)

**Interfaces:**
- Consumes: `gen.AddAccountChannel`, `gen.RemoveAccountChannel`, `gen.ListAccountChannels` (Task 1); `applyReload` (existing, used by `addGame`).
- Produces: `POST /accounts/{id}/channels/add` (form `channel`), `POST /accounts/{id}/channels/remove` (form `channel`); account-detail template field `.Channels []string`.

- [ ] **Step 1: Write the failing handler test**

Following the harness in `internal/api/handlers_accounts_toggle_test.go`, create `internal/api/handlers_accounts_channels_test.go`:

```go
// addChannel inserts a lowercased channel row; removeChannel deletes it.
func TestAccountChannels_AddAndRemove(t *testing.T) {
	// ARRANGE: temp DB, gen.New(db), create account "acc-1",
	//   build accountsDeps the same way the toggle test does.
	// ACT add: POST /accounts/acc-1/channels/add with form "channel=XQC"
	//   (route param id=acc-1; set chi URL param like the toggle test does).
	// ASSERT: ListAccountChannels(acc-1) has exactly ["xqc"] (lowercased).
	// ACT remove: POST /accounts/acc-1/channels/remove with form "channel=xqc".
	// ASSERT: ListAccountChannels(acc-1) is empty.
}
```

Fill the body using the same harness/CSRF/chi-param pattern as the toggle test. The lowercasing assertion and the empty-after-remove assertion are mandatory.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestAccountChannels -v`
Expected: FAIL — handlers undefined.

- [ ] **Step 3: Implement the handlers**

In `internal/api/handlers_accounts.go`, add (mirror `addGame`/`useGlobal` style, including the post-mutation `applyReload`):

```go
// addChannel handles POST /accounts/:id/channels/add — opts the account
// into a channel so null-game drops on that channel get mined.
func (d accountsDeps) addChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	ch := strings.ToLower(strings.TrimSpace(r.FormValue("channel")))
	if ch != "" {
		if err := d.q.AddAccountChannel(r.Context(), gen.AddAccountChannelParams{
			AccountID: id, Channel: ch, Rank: 0,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.applyReload(r.Context())
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}

// removeChannel handles POST /accounts/:id/channels/remove.
func (d accountsDeps) removeChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := d.q.GetAccount(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	ch := strings.ToLower(strings.TrimSpace(r.FormValue("channel")))
	if ch != "" {
		if err := d.q.RemoveAccountChannel(r.Context(), gen.RemoveAccountChannelParams{
			AccountID: id, Channel: ch,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.applyReload(r.Context())
	}
	http.Redirect(w, r, "/accounts/"+id, http.StatusSeeOther)
}
```

Confirm the redirect target matches the account-detail route the other account handlers redirect to (e.g. `/accounts/`+id). Match whatever `addGame` uses.

- [ ] **Step 4: Register routes**

In `internal/api/server.go`, next to the `/accounts/{id}/games*` routes:

```go
	authed.Post("/accounts/{id}/channels/add", accs.addChannel)
	authed.Post("/accounts/{id}/channels/remove", accs.removeChannel)
```

- [ ] **Step 5: Load channels into the account-detail page**

Find the handler that renders `accounts_detail.html` (the account `get`/`detail` handler in `handlers_accounts.go`). Where it assembles the template page data (the struct that already carries the games list), add the account's channels:

```go
	var channels []string
	if rows, err := d.q.ListAccountChannels(r.Context(), id); err == nil {
		for _, rch := range rows {
			channels = append(channels, rch.Channel)
		}
	}
```

Add a `Channels []string` field to that page-data struct and set it. (The struct is whatever the detail handler currently passes as `Page:` — add the field there.)

- [ ] **Step 6: Add the template block**

In `internal/web/templates/accounts_detail.html`, after the games whitelist block (after the "use global" form, around line 115), add:

```html
  <section class="card">
    <h2>{{t "account.channels_title"}}</h2>
    <p class="muted" style="font-size:12px;">{{t "account.channels_desc"}}</p>
    {{if .Channels}}
    <div class="chips">
      {{range .Channels}}
      <form method="post" action="/accounts/{{$.Account.ID}}/channels/remove" style="display:inline" onsubmit="this.querySelector('button').disabled=true">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <input type="hidden" name="channel" value="{{.}}">
        <button class="chip" type="submit" title="{{t "account.channel_remove"}}">{{.}} ✕</button>
      </form>
      {{end}}
    </div>
    {{else}}
    <p class="muted" style="font-size:12px;">{{t "account.channels_empty"}}</p>
    {{end}}
    <form method="post" action="/accounts/{{.Account.ID}}/channels/add" class="add-by-name">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <div class="kvedit"><div class="row">
        <span class="k">{{t "account.channel_add"}}</span>
        <span class="d">{{t "account.channel_add_desc"}}</span>
        <span class="v grow"><input type="text" name="channel" placeholder="{{t "account.channel_placeholder"}}" required></span>
        <button class="btn-linear" type="submit">{{t "account.add"}} →</button>
      </div></div>
    </form>
  </section>
```

(Match the surrounding markup conventions of `accounts_detail.html`; the exact wrapper classes may differ — copy the games section's wrapper. The i18n keys are added in Task 6. `account.add` already exists.)

- [ ] **Step 7: Build + test**

Run: `go build ./... && go test ./internal/api`
Expected: build OK; PASS.

- [ ] **Step 8: Format + commit**

```bash
gofmt -w .
git add internal/api/handlers_accounts.go internal/api/server.go internal/web/templates/accounts_detail.html internal/api/handlers_accounts_channels_test.go
git commit -m "feat(accounts): channel whitelist editor on account detail

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: i18n keys, CHANGELOG, full verification

**Files:**
- Modify: every file under `internal/i18n/locales/` (currently `en.json`, `zh-CN.json` — re-list first; a concurrent agent may have added `es.json`)
- Modify: `docs/CHANGELOG.md`

**Interfaces:** consumes nothing; finalizes the feature.

- [ ] **Step 1: Re-list locales (concurrency check)**

Run: `ls internal/i18n/locales/`
Note every file present. New keys MUST be added to all of them with equal key sets.

- [ ] **Step 2: Add the new keys to every locale file**

Add these keys (nest under the existing `drops_table` and `account` objects, matching each file's structure). English values:

- `drops_table.null_game` → `"Discoverable — Null Game"`
- `account.channels_title` → `"Channels"`
- `account.channels_desc` → `"Mine drops on these channels even when the drop has no game category (e.g. Kick Football drops)."`
- `account.channels_empty` → `"No channels yet."`
- `account.channel_add` → `"Add channel"`
- `account.channel_add_desc` → `"Whitelist a channel by its slug."`
- `account.channel_placeholder` → `"channel slug (e.g. xqc)"`
- `account.channel_remove` → `"Remove channel"`

For `zh-CN.json` provide reasonable Simplified Chinese translations (match the tone of existing entries). For any other locale present (e.g. `es.json`), add the same keys; if you cannot translate confidently, use the English string as a placeholder value so key sets stay equal — but keep the keys present in every file.

- [ ] **Step 3: Verify locales have equal key sets**

Run:
```bash
cd internal/i18n/locales && for f in *.json; do echo "$f:"; python3 -c "import json,sys;print(len(json.load(open('$f'))))" 2>/dev/null || true; done
```
Then build (the i18n loader validates at startup/tests):
Run: `go test ./internal/i18n/...`
Expected: PASS (no missing/extra key errors).

- [ ] **Step 4: Update CHANGELOG**

In `docs/CHANGELOG.md` under `## [Unreleased]`, add under `### Added`:

```markdown
- **Channel whitelist for null-game drops** — opt an account into a Kick/Twitch
  channel so drops with no game category (e.g. Kick Football drops) get mined.
  New "Discoverable — Null Game" section on /drops with a per-account
  WHITELIST+, plus a channel editor on the account detail page. (#20)
```

- [ ] **Step 5: Full build + test + format**

Run: `gofmt -l . && go build ./... && go test ./...`
Expected: `gofmt -l .` prints nothing (all formatted); build OK; all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/i18n/locales docs/CHANGELOG.md
git commit -m "feat(i18n,docs): channel-whitelist strings + changelog (#20)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Live verification (release gate, before any tag)**

This changes campaign selection. Live null-game (Football) drops exist now.
Verify against a live drop before tagging a release:
1. Deploy/run the miner with a logged-in Kick account.
2. On `/drops` (current tab), confirm the "Discoverable — Null Game" section
   lists the Football drops with the channel(s) and a WHITELIST+ dropdown.
3. WHITELIST+ a Football drop's channel onto the account; confirm the watcher
   picks it (logs: watcher mining the null-game campaign) and watch-time
   accrues against that channel.
Do not tag until this passes (or hold in `[Unreleased]`).

---

## Self-Review

**Spec coverage:**
- Data model (account_channels) → Task 1. ✓
- Channel persistence via raw_json → Task 2. ✓
- Watcher AllowChannel + filter OR + loadAccountChannels wiring → Task 3. ✓
- /drops null-game section between unlinked + discoverable, only when open → Task 4 (template placement + `tab == tabCurrent` + `len(Channels)>0` guard). ✓
- New per-row channel WHITELIST+ (per-account only, no global) → Task 4. ✓
- Account settings channel editor (add/remove, flat list) → Task 5. ✓
- i18n en/zh(/es) → Task 6. ✓
- CHANGELOG + live-verify gate → Task 6. ✓

**Placeholder scan:** Two handler-test bodies (Task 4 Step 1, Task 5 Step 1) describe the harness rather than quoting it verbatim, because the existing `internal/api` test harness/CSRF setup must be copied from neighboring tests rather than guessed. The required ARRANGE/ACT/ASSERT and the mandatory assertions are spelled out. All production code steps contain full code.

**Type consistency:** `AllowChannel func([]string) bool` consistent across watcher.go, watcher_test.go, main.go, main_test.go. `loadAccountChannels` signature consistent (Task 3 def + Task 3 test). `gen.*AccountChannel*` names consistent across Tasks 1/3/4/5. `dropsRow.Channels` / `dropsPage.NullGameRows` defined in Task 4 and consumed by the same task's template. `channelsFromRawJSON` / `channelsFromRawJSON` naming consistent.

**Scope:** single feature, one plan. B and C remain backlog.
```
