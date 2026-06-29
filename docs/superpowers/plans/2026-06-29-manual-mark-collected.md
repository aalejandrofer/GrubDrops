# Manual "mark collected" Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user manually mark a drop benefit as collected per-account from the `/drops` items panel, protected from the reconcile prune, and make item panels load even when the relevant account is disabled.

**Architecture:** Marking writes a real `claims` row (so existing COLLECTED rendering + counts work) plus a `collect_override:<benefitID>:<accountID>` kv flag. The watcher gains a `ForceCollected` hook (twin of `ForceLinked`) that skips overridden marks at both reconcile-prune sites. The items handler broadens its session lookup to any account (incl disabled) so benefits can be lazy-fetched and rendered. UI adds a `+` menu of eligible accounts per benefit row, mirroring the existing click-to-uncollect chip.

**Tech Stack:** Go, sqlc (SQLite), html/template, HTMX, testify.

## Global Constraints

- **sqlc:** never put `?` or parentheses in a `queries/*.sql` comment (corrupts placeholder rewriting). This plan adds no new SQL queries, so no `queries/*.sql` edits are required.
- **i18n:** every new UI string needs keys in all three locales (`en.json`, `es.json`, `zh-CN.json`); `internal/i18n` `TestLocaleParity` fails on any drift.
- **gofmt:** run `gofmt -w` on every changed `.go` file before committing (CI gofmt gate fails fast).
- **CHANGELOG:** log every change under `## [Unreleased]` in `docs/CHANGELOG.md` as you go.
- **Reconcile safety:** the prune logic caused a prod incident (deleting genuine claims). Do NOT change which marks get pruned except to ADD the `ForceCollected` skip. The existing `TestWatcher_ReconcilePrunesStaleClaims` must stay green.
- **Claim identity:** one claim per `(account_id, benefit_id)` — `claims` has `UNIQUE(account_id, benefit_id)`; `InsertClaim` upserts.
- **Override key format (verbatim, used in 3 places):** `store.CollectOverridePrefix + benefitID + ":" + accountID`.

---

### Task 1: Store constants + exported claim-id helper

**Files:**
- Modify: `internal/store/campaign_persister.go:18` (add sibling const)
- Modify: `internal/store/claim_recorder.go:123-127` (export the id helper)
- Test: `internal/store/claim_recorder_test.go` (create if absent)

**Interfaces:**
- Produces: `store.CollectOverridePrefix` (string const `"collect_override:"`); `store.NewClaimID() string` (exported wrapper used by the API handler).

- [ ] **Step 1: Add the override prefix constant.** In `internal/store/campaign_persister.go`, directly below the existing `LinkOverridePrefix`:

```go
const LinkOverridePrefix = "link_override:"

// CollectOverridePrefix keys a manual "mark collected" assertion in kv.
// Full key: CollectOverridePrefix + benefitID + ":" + accountID, value "1".
// The watcher's ForceCollected hook reads these so the reconcile prune never
// removes a claim the user manually asserted.
const CollectOverridePrefix = "collect_override:"
```

- [ ] **Step 2: Export the claim-id generator.** In `internal/store/claim_recorder.go`, replace the unexported `newClaimID` with an exported `NewClaimID` and keep the internal callers working:

```go
// NewClaimID returns a fresh random claims-table primary key. Exported so
// non-recorder callers (e.g. the manual mark-collected handler) can insert a
// claim row directly.
func NewClaimID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "clm_" + hex.EncodeToString(b[:])
}
```

Then update the one internal caller at `RecordClaimWithCode` (was `ID: newClaimID()`) to `ID: NewClaimID()`.

- [ ] **Step 3: Write the failing test.** In `internal/store/claim_recorder_test.go`:

```go
package store

import (
	"strings"
	"testing"
)

func TestNewClaimID_PrefixedAndUnique(t *testing.T) {
	a := NewClaimID()
	b := NewClaimID()
	if !strings.HasPrefix(a, "clm_") {
		t.Fatalf("id %q missing clm_ prefix", a)
	}
	if a == b {
		t.Fatalf("two ids collided: %q", a)
	}
}
```

- [ ] **Step 4: Run tests.** Run: `go test ./internal/store/ -run TestNewClaimID -v`. Expected: PASS. Also `go build ./...` to confirm no caller of the old `newClaimID` remains.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/store/campaign_persister.go internal/store/claim_recorder.go internal/store/claim_recorder_test.go
git add internal/store/ && git commit -m "feat(store): collect-override prefix + exported NewClaimID"
```

---

### Task 2: Watcher `ForceCollected` hook + reconcile guard

**Files:**
- Modify: `internal/watcher/watcher.go` (Config struct ~line 114; prune sites ~line 974 and ~1008)
- Test: `internal/watcher/watcher_test.go` (add one test, reuse existing `multiTierBackend` + `recordingClaimRecorder`)

**Interfaces:**
- Produces: `watcher.Config.ForceCollected func(accountID, benefitID string) bool`. Nil means "no overrides" (treated as never-protected, preserving current behaviour).
- Consumes: nothing new.

- [ ] **Step 1: Write the failing test.** In `internal/watcher/watcher_test.go` (after `TestWatcher_ReconcilePrunesStaleClaims`):

```go
// TestWatcher_ForceCollected_ProtectsManualMark verifies the manual
// mark-collected override: a benefit covered by ForceCollected must NOT be
// pruned by the reconcile self-heal even while inventory reports it
// in-progress and unclaimed; the other unclaimed tiers must still be pruned.
func TestWatcher_ForceCollected_ProtectsManualMark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec := &recordingClaimRecorder{}
	backend := &multiTierBackend{MockBackend: platformtest.New()}
	w := New(Config{
		AccountID:     "acc_r6s_protect",
		Backend:       backend,
		ClaimRecorder: rec,
		Session:       platform.Session{AccessToken: "tok"},
		Notifier:      &recordingNotifier{},
		TickInterval:  2 * time.Millisecond,
		AllowGame:     func(g string) bool { return g == "Rainbow Six Siege" },
		ForceCollected: func(_ string, benefitID string) bool {
			return benefitID == "t180" // user manually marked this tier collected
		},
	})

	go func() { _ = w.Run(ctx) }()

	// The unprotected unclaimed tiers must still be pruned.
	require.Eventually(t, func() bool {
		_, pruned := rec.snapshot()
		return contains(pruned, "t360") && contains(pruned, "t540")
	}, time.Second, 5*time.Millisecond, "unprotected stale tiers must still prune")

	_, pruned := rec.snapshot()
	assert.NotContains(t, pruned, "t180", "a ForceCollected benefit must never be pruned")
}
```

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/watcher/ -run TestWatcher_ForceCollected_ProtectsManualMark -v`. Expected: FAIL — `Config` has no `ForceCollected` field (compile error), or (once the field exists but isn't consulted) t180 appears in `pruned`.

- [ ] **Step 3: Add the Config field.** In `internal/watcher/watcher.go`, immediately after the `ForceLinked func(campaignID string) bool` field:

```go
	// ForceCollected, when set and returning true for (accountID, benefitID),
	// marks that benefit as user-asserted collected. The reconcile prune skips
	// it, so a manual "mark collected" survives even while inventory reports the
	// drop in-progress and unclaimed. Nil = no overrides. Backs the /drops
	// manual mark-collected control.
	ForceCollected func(accountID, benefitID string) bool
```

- [ ] **Step 4: Guard prune site 1 (in-progress branch).** In the reconcile loop, change the `if canPrune && tracked[b.ID] && !claimed[b.ID] {` block so it skips protected marks before pruning:

```go
		if canPrune && tracked[b.ID] && !claimed[b.ID] {
			if w.cfg.ForceCollected != nil && w.cfg.ForceCollected(w.cfg.AccountID, b.ID) {
				continue // user manually marked collected — never prune an asserted mark
			}
			if pruned, err := pruner.PruneClaim(ctx, w.cfg.AccountID, b); err == nil && pruned {
				slog.Info("watcher pruned stale claim",
					"kind", "claim", "account", w.cfg.AccountID,
					"benefit", b.ID, "benefit_name", b.Name)
			}
			continue
		}
```

- [ ] **Step 5: Guard prune site 2 (inventory sweep).** In the `if canPrune { for id := range tracked { ... } }` block, add the same skip:

```go
	if canPrune {
		for id := range tracked {
			if claimed[id] {
				continue
			}
			if w.cfg.ForceCollected != nil && w.cfg.ForceCollected(w.cfg.AccountID, id) {
				continue // protected manual mark
			}
			if pruned, err := pruner.PruneClaim(ctx, w.cfg.AccountID, platform.DropBenefit{ID: id}); err == nil && pruned {
				slog.Info("watcher pruned stale claim (inventory sweep)",
					"kind", "claim", "account", w.cfg.AccountID, "benefit", id)
			}
		}
	}
```

- [ ] **Step 6: Run tests.** Run: `go test ./internal/watcher/ -run 'TestWatcher_ForceCollected_ProtectsManualMark|TestWatcher_ReconcilePrunesStaleClaims' -v`. Expected: both PASS (new one protects t180; the existing self-heal still prunes t180/t360/t540 — note it has no ForceCollected so nothing is protected there).

- [ ] **Step 7: Commit.**

```bash
gofmt -w internal/watcher/watcher.go internal/watcher/watcher_test.go
git add internal/watcher/ && git commit -m "feat(watcher): ForceCollected hook protects manual marks from reconcile prune"
```

---

### Task 3: Wire `ForceCollected` from kv overrides

**Files:**
- Modify: `cmd/miner/main.go` (add `loadCollectOverrides` near `loadLinkOverrides` ~line 655; set `ForceCollected` in the `watcher.Config{...}` literal ~line 427)

**Interfaces:**
- Consumes: `store.CollectOverridePrefix` (Task 1), `watcher.Config.ForceCollected` (Task 2).
- Produces: `loadCollectOverrides(ctx, q) func(accountID, benefitID string) bool`.

- [ ] **Step 1: Add the loader.** In `cmd/miner/main.go`, directly after `loadLinkOverrides`:

```go
// loadCollectOverrides reads the manual "mark collected" assertions from kv
// (keys prefixed store.CollectOverridePrefix, value "1") and returns a
// membership predicate keyed by (accountID, benefitID). The kv key encodes
// benefitID + ":" + accountID. Errors degrade to "no overrides" (nil), so the
// reconcile prune keeps its normal behaviour. Loaded per build (per Reload) so
// marking + reloading takes effect immediately.
func loadCollectOverrides(ctx context.Context, q *gen.Queries) func(accountID, benefitID string) bool {
	set := map[string]bool{}
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.CollectOverridePrefix, Valid: true})
	if err == nil {
		for _, kv := range rows {
			if string(kv.Value) != "1" {
				continue
			}
			set[strings.TrimPrefix(kv.Key, store.CollectOverridePrefix)] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return func(accountID, benefitID string) bool { return set[benefitID+":"+accountID] }
}
```

- [ ] **Step 2: Wire it into the watcher config.** Near where `forceLinked := loadLinkOverrides(ctx, q)` is set, add:

```go
	forceLinked := loadLinkOverrides(ctx, q)
	forceCollected := loadCollectOverrides(ctx, q)
```

Then in the `watcher.New(watcher.Config{...})` literal, directly after `ForceLinked:   forceLinked,`:

```go
		ForceLinked:    forceLinked,
		ForceCollected: forceCollected,
```

(Re-gofmt aligns the struct keys; exact spacing is cosmetic.)

- [ ] **Step 3: Build.** Run: `go build ./...`. Expected: success (no test for this glue — it's a one-line wiring mirror of `forceLinked`, covered end-to-end by the watcher test in Task 2 and the handler tests in Task 5).

- [ ] **Step 4: Commit.**

```bash
gofmt -w cmd/miner/main.go
git add cmd/miner/main.go && git commit -m "feat(miner): wire ForceCollected from collect_override kv keys"
```

---

### Task 4: Item panel loads via any account session (fix the blocker)

**Files:**
- Modify: `internal/api/handlers_drops.go:83-98` (`sessionForPlatform`)
- Test: `internal/api/handlers_drops_collect_test.go` (create)

**Interfaces:**
- Produces: `sessionForPlatform` now falls back to any account (incl disabled) with a stored session when no enabled account on the platform has one. Signature unchanged: `func (d *dropsDeps) sessionForPlatform(ctx context.Context, plat string) (platform.Session, bool)`.

- [ ] **Step 1: Write the failing test.** In `internal/api/handlers_drops_collect_test.go`:

```go
package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestSessionForPlatform_FallsBackToDisabledAccount proves a drop collected on
// a now-disabled account is still serviceable: sessionForPlatform must return a
// session from a DISABLED account when no enabled account on the platform has
// one, so lazyFetchBenefits can populate the items panel.
func TestSessionForPlatform_FallsBackToDisabledAccount(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	sessions := store.NewSessionStore(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-off", Platform: "twitch", DisplayName: "Phluses",
		Status: "idle", FingerprintJson: "{}", Enabled: 0, // DISABLED
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	// Give the disabled account a stored session.
	require.NoError(t, sessions.Put(ctx, "acc-off", platformSessionFixture()))

	d := &dropsDeps{q: q, sessions: sessions}
	sess, ok := d.sessionForPlatform(ctx, "twitch")
	require.True(t, ok, "must fall back to the disabled account's session")
	require.Equal(t, "acc-off", sess.AccountID)
}
```

> **Note for implementer:** confirm the real `store.SessionStore.Put` signature and the `platform.Session` zero value before finalising `platformSessionFixture()`. If `SessionStore.Put` takes a `platform.Session`, define:
> ```go
> func platformSessionFixture() platform.Session { return platform.Session{AccessToken: "tok"} }
> ```
> (Add the `platform` import.) Adjust to the actual `Put` signature if it differs.

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/api/ -run TestSessionForPlatform_FallsBackToDisabledAccount -v`. Expected: FAIL (`ok` is false — current code only scans enabled accounts).

- [ ] **Step 3: Implement the fallback.** Replace `sessionForPlatform` in `internal/api/handlers_drops.go`:

```go
// sessionForPlatform returns a usable session for the platform. It prefers an
// enabled account's session (the common case), then falls back to ANY account
// with a stored session — including disabled ones. Reading public campaign
// details is account-state-agnostic, and the fallback is what lets a drop
// collected on a since-disabled account still load its item list.
func (d *dropsDeps) sessionForPlatform(ctx context.Context, plat string) (platform.Session, bool) {
	if sess, ok := d.firstStoredSession(ctx, plat, true); ok {
		return sess, true
	}
	return d.firstStoredSession(ctx, plat, false)
}

// firstStoredSession returns the first session on the platform among either
// the enabled accounts (enabledOnly=true) or all accounts (enabledOnly=false).
func (d *dropsDeps) firstStoredSession(ctx context.Context, plat string, enabledOnly bool) (platform.Session, bool) {
	var (
		accs []gen.Account
		err  error
	)
	if enabledOnly {
		accs, err = d.q.ListEnabledAccounts(ctx)
	} else {
		accs, err = d.q.ListAllAccounts(ctx)
	}
	if err != nil {
		return platform.Session{}, false
	}
	for _, a := range accs {
		if a.Platform != plat {
			continue
		}
		if sess, ok, err := d.sessions.Get(ctx, a.ID); err == nil && ok {
			sess.AccountID = a.ID
			return sess, true
		}
	}
	return platform.Session{}, false
}
```

- [ ] **Step 4: Run test to verify it passes.** Run: `go test ./internal/api/ -run TestSessionForPlatform_FallsBackToDisabledAccount -v`. Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/api/handlers_drops.go internal/api/handlers_drops_collect_test.go
git add internal/api/ && git commit -m "fix(drops): load item panel via any account session, incl disabled"
```

---

### Task 5: `addClaim` handler + `removeClaim` override cleanup + route

**Files:**
- Modify: `internal/api/handlers_drops.go` (add `addClaim` near `removeClaim` ~1186; edit `removeClaim`)
- Modify: `internal/api/server.go:359` (register route)
- Test: `internal/api/handlers_drops_collect_test.go` (append)

**Interfaces:**
- Consumes: `store.CollectOverridePrefix`, `store.NewClaimID` (Task 1).
- Produces: `POST /drops/claim/add` → `(d *dropsDeps) addClaim`. Form fields `account_id`, `benefit_id`, `campaign_id`.

- [ ] **Step 1: Write the failing tests.** Append to `internal/api/handlers_drops_collect_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	// (plus the imports already present in the file)
)

// seedCampaignWithBenefit inserts a campaign + one benefit for collect tests.
func seedCampaignWithBenefit(t *testing.T, ctx context.Context, q *gen.Queries, campID, benID, plat string) {
	t.Helper()
	now := time.Now().Unix()
	require.NoError(t, q.UpsertCampaign(ctx, gen.UpsertCampaignParams{
		ID: campID, Platform: plat, Game: "Minecraft", Name: "MC Drop",
		StartsAt: now - 60, EndsAt: now + 3600, Status: "active",
		RawJson: "{}", DiscoveredAt: now, Kind: "drop",
		AccountLinked: 1, AccountLinkUrl: "",
	}))
	require.NoError(t, q.UpsertBenefit(ctx, gen.UpsertBenefitParams{
		ID: benID, CampaignID: campID, Name: "Builder Cape", RequiredMinutes: 5, ImageUrl: "",
	}))
}

func TestAddClaim_WritesClaimAndOverride(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "twitch", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	seedCampaignWithBenefit(t, ctx, q, "camp-1", "ben-1", "twitch")

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}

	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Set("benefit_id", "ben-1")
	form.Set("campaign_id", "camp-1")
	req := httptest.NewRequest(http.MethodPost, "/drops/claim/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.addClaim(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// Claim row exists.
	n, err := q.CountClaimsFor(ctx, gen.CountClaimsForParams{AccountID: "acc-1", BenefitID: "ben-1"})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	// Override key exists.
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.CollectOverridePrefix, Valid: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, store.CollectOverridePrefix+"ben-1:acc-1", rows[0].Key)
}

func TestRemoveClaim_DeletesClaimAndOverride(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	now := time.Now().Unix()
	_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
		ID: "acc-1", Platform: "twitch", DisplayName: "TTik3r",
		Status: "idle", FingerprintJson: "{}", Enabled: 1, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	seedCampaignWithBenefit(t, ctx, q, "camp-1", "ben-1", "twitch")
	require.NoError(t, q.InsertClaim(ctx, gen.InsertClaimParams{
		ID: store.NewClaimID(), AccountID: "acc-1", BenefitID: "ben-1",
		ClaimedAt: now, ValueMetaJson: `{"manual":true}`,
	}))
	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{
		Key: store.CollectOverridePrefix + "ben-1:acc-1", Value: []byte("1"),
	}))

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}
	form := url.Values{}
	form.Set("account_id", "acc-1")
	form.Set("benefit_id", "ben-1")
	form.Set("campaign_id", "camp-1")
	req := httptest.NewRequest(http.MethodPost, "/drops/claim/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.removeClaim(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	n, err := q.CountClaimsFor(ctx, gen.CountClaimsForParams{AccountID: "acc-1", BenefitID: "ben-1"})
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
	rows, err := q.ListKVByPrefix(ctx, sql.NullString{String: store.CollectOverridePrefix, Valid: true})
	require.NoError(t, err)
	require.Empty(t, rows, "uncollecting must clear the protection override")
}
```

> **Note for implementer:** `testRenderer(t)` must return a `Renderer` backed by `web.Templates()` (so `renderCampaignItems` can execute `drops_campaign_items`). If a helper already exists in the `api` test package that builds a Renderer, use it; otherwise add:
> ```go
> func testRenderer(t *testing.T) Renderer {
> 	t.Helper()
> 	tmpl, err := web.Templates()
> 	require.NoError(t, err)
> 	return tmpl
> }
> ```
> Confirm `web.Templates()` returns a type satisfying the `Renderer` interface (it backs the real server). Add the `web` import.

- [ ] **Step 2: Run tests to verify they fail.** Run: `go test ./internal/api/ -run 'TestAddClaim_WritesClaimAndOverride|TestRemoveClaim_DeletesClaimAndOverride' -v`. Expected: FAIL — `addClaim` undefined; `removeClaim` doesn't delete the override yet.

- [ ] **Step 3: Implement `addClaim`.** In `internal/api/handlers_drops.go`, directly above `removeClaim`:

```go
// addClaim handles the manual "mark collected" control on the /drops items
// panel. It writes a claims row (tagged manual) for (account_id, benefit_id)
// AND sets a collect_override kv flag the watcher's ForceCollected reads, so
// the reconcile prune never removes the user-asserted mark. Mirrors removeClaim
// and re-renders the same items partial.
func (d *dropsDeps) addClaim(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.FormValue("account_id"))
	benefitID := strings.TrimSpace(r.FormValue("benefit_id"))
	campaignID := strings.TrimSpace(r.FormValue("campaign_id"))
	if accountID == "" || benefitID == "" || campaignID == "" {
		http.Error(w, "missing account_id, benefit_id, or campaign_id", http.StatusBadRequest)
		return
	}
	acc, err := d.q.GetAccount(r.Context(), accountID)
	if err != nil {
		http.Error(w, "unknown account", http.StatusBadRequest)
		return
	}
	camp, err := d.q.GetCampaign(r.Context(), campaignID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if acc.Platform != camp.Platform {
		http.Error(w, "account/campaign platform mismatch", http.StatusBadRequest)
		return
	}
	// The benefit must belong to this campaign (guards against spoofed ids).
	bens, _ := d.q.ListBenefitsForCampaign(r.Context(), campaignID)
	known := false
	for _, b := range bens {
		if b.ID == benefitID {
			known = true
			break
		}
	}
	if !known {
		http.Error(w, "unknown benefit for campaign", http.StatusBadRequest)
		return
	}
	if err := d.q.InsertClaim(r.Context(), gen.InsertClaimParams{
		ID:            store.NewClaimID(),
		AccountID:     accountID,
		BenefitID:     benefitID,
		ClaimedAt:     time.Now().Unix(),
		ValueMetaJson: `{"manual":true}`,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := d.q.UpsertSettingString(r.Context(), gen.UpsertSettingStringParams{
		Key:   store.CollectOverridePrefix + benefitID + ":" + accountID,
		Value: []byte("1"),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("manual claim mark", "kind", "claim", "account", accountID, "benefit", benefitID, "campaign", campaignID)
	// Reload so ForceCollected picks up the new override without waiting for the
	// next discovery cycle (best-effort; the override is persisted regardless).
	if d.reload != nil {
		if err := d.reload(r.Context()); err != nil {
			slog.Warn("scheduler reload after manual collect failed", "benefit", benefitID, "err", err)
		}
	}
	d.renderCampaignItems(w, r, campaignID)
}
```

- [ ] **Step 4: Make `removeClaim` clear the override.** In `removeClaim`, after the successful `DeleteClaimFor` call and before the `slog.Info`, add:

```go
	// Drop any manual-collect protection so an uncollected mark isn't left with
	// a stale override (no-op for auto claims, which have no override key).
	_ = d.q.DeleteKV(r.Context(), store.CollectOverridePrefix+benefitID+":"+accountID)
```

- [ ] **Step 5: Register the route.** In `internal/api/server.go`, directly below the `/drops/claim/remove` line:

```go
	authed.Post("/drops/claim/remove", dropsH.removeClaim)
	authed.Post("/drops/claim/add", dropsH.addClaim)
```

- [ ] **Step 6: Run tests.** Run: `go test ./internal/api/ -run 'TestAddClaim_WritesClaimAndOverride|TestRemoveClaim_DeletesClaimAndOverride' -v`. Expected: both PASS. Confirm `store` is imported in `handlers_drops.go` (it already is, via `store.LinkOverridePrefix`).

- [ ] **Step 7: Commit.**

```bash
gofmt -w internal/api/handlers_drops.go internal/api/server.go internal/api/handlers_drops_collect_test.go
git add internal/api/ && git commit -m "feat(drops): POST /drops/claim/add manual mark-collected + clear override on uncollect"
```

---

### Task 6: Compute eligible "addable" accounts per benefit

**Files:**
- Modify: `internal/api/handlers_drops.go` (add `addableAccount` type + helper; populate in `renderCampaignItems`; add field to `campaignBenefitRow`)
- Test: `internal/api/handlers_drops_collect_test.go` (append a pure-helper test)

**Interfaces:**
- Consumes: `gen.Account`, `collectedMark` (existing).
- Produces: `type addableAccount struct { Login, Platform, AccountID string }`; `func addableAccounts(accs []gen.Account, plat string, collected []collectedMark) []addableAccount`; new field `campaignBenefitRow.Addable []addableAccount`.

- [ ] **Step 1: Write the failing test.** Append to `internal/api/handlers_drops_collect_test.go`:

```go
func TestAddableAccounts_ExcludesCollectedAndCrossPlatform(t *testing.T) {
	accs := []gen.Account{
		{ID: "a1", Platform: "twitch", DisplayName: "TTik3r", Enabled: 1},
		{ID: "a2", Platform: "twitch", DisplayName: "Phluses", Enabled: 0}, // disabled still offered
		{ID: "a3", Platform: "kick", DisplayName: "KickOnly", Enabled: 1},  // wrong platform
	}
	collected := []collectedMark{{AccountID: "a1", BenefitID: "ben-1"}}

	got := addableAccounts(accs, "twitch", collected)

	if len(got) != 1 {
		t.Fatalf("got %d addable, want 1 (a2 only)", len(got))
	}
	if got[0].AccountID != "a2" {
		t.Fatalf("addable = %q, want a2", got[0].AccountID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/api/ -run TestAddableAccounts_ExcludesCollectedAndCrossPlatform -v`. Expected: FAIL — `addableAccount`/`addableAccounts` undefined.

- [ ] **Step 3: Add the type + helper.** In `internal/api/handlers_drops.go`, near the `collectedMark` type:

```go
// addableAccount is one account offered in a benefit row's "+ mark collected"
// menu: a matching-platform account that has NOT already collected that benefit.
type addableAccount struct {
	Login     string
	Platform  string
	AccountID string
}

// addableAccounts returns the matching-platform accounts (enabled or disabled)
// not already present in `collected`. Disabled accounts are intentionally
// included: the user may have collected on an account they later disabled.
func addableAccounts(accs []gen.Account, plat string, collected []collectedMark) []addableAccount {
	done := map[string]bool{}
	for _, c := range collected {
		done[c.AccountID] = true
	}
	var out []addableAccount
	for _, a := range accs {
		if a.Platform != plat || done[a.ID] {
			continue
		}
		out = append(out, addableAccount{Login: a.DisplayName, Platform: a.Platform, AccountID: a.ID})
	}
	return out
}
```

- [ ] **Step 4: Add the field to `campaignBenefitRow`.** Add to the struct:

```go
	Collected       []collectedMark
	Addable         []addableAccount // accounts that can still be marked collected
```

- [ ] **Step 5: Populate it in `renderCampaignItems`.** Load all accounts once before the benefit loop, and set `Addable` per benefit:

```go
	allAccts, _ := d.q.ListAllAccounts(r.Context())
	for _, b := range bens {
		img := b.ImageUrl
		if img != "" && detail.Platform == "kick" {
			img = "/img/kick?u=" + url.QueryEscape(img)
		}
		collected := collectedByBenefit[b.ID]
		detail.Benefits = append(detail.Benefits, campaignBenefitRow{
			Name:            b.Name,
			RequiredMinutes: b.RequiredMinutes,
			ImageURL:        img,
			Collected:       collected,
			Addable:         addableAccounts(allAccts, detail.Platform, collected),
		})
	}
```

- [ ] **Step 6: Run tests.** Run: `go test ./internal/api/ -run TestAddableAccounts -v` then `go build ./...`. Expected: PASS + build clean.

- [ ] **Step 7: Commit.**

```bash
gofmt -w internal/api/handlers_drops.go internal/api/handlers_drops_collect_test.go
git add internal/api/ && git commit -m "feat(drops): compute addable accounts per benefit for mark-collected menu"
```

---

### Task 7: UI — `+` menu, CSS, i18n, render test

**Files:**
- Modify: `internal/web/templates/_drops_campaign_items.html` (COLLECTED cell)
- Modify: `internal/web/static/css/app.css` (menu styles, near `.cmark`)
- Modify: `internal/i18n/locales/en.json`, `es.json`, `zh-CN.json`
- Test: `internal/api/handlers_drops_collect_test.go` (append a render test)

**Interfaces:**
- Consumes: `campaignBenefitRow.Addable`, `campaignDetailRow.ID`, `.CSRFToken` (Task 6 + existing).

- [ ] **Step 1: Add i18n keys (all three locales).** In `en.json`, after `"campaign_items.no_items": ...`:

```json
  "campaign_items.mark_collected": "mark collected",
  "campaign_items.mark_collected_for": "mark collected for",
```

In `es.json` (same keys):

```json
  "campaign_items.mark_collected": "marcar recogido",
  "campaign_items.mark_collected_for": "marcar recogido para",
```

In `zh-CN.json` (same keys):

```json
  "campaign_items.mark_collected": "标记为已领取",
  "campaign_items.mark_collected_for": "标记已领取账号",
```

- [ ] **Step 2: Add the `+` menu to the COLLECTED cell.** In `_drops_campaign_items.html`, replace the `<span class="drop-item-collected">…</span>` block (the `{{if .Collected}}…{{else}}—{{end}}`) with chips + an add menu:

```html
    <span class="drop-item-collected">
      {{if .Collected}}
        {{range .Collected}}<button type="button" class="cmark cmark-{{.Platform}} cmark-btn"
          title="{{t "campaign_items.claimed_by"}} {{.Login}} — {{t "campaign_items.uncollect_hint"}}"
          hx-post="/drops/claim/remove"
          hx-vals='{"account_id":"{{.AccountID}}","benefit_id":"{{.BenefitID}}","campaign_id":"{{$.ID}}"}'
          hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}'
          hx-confirm="{{t "campaign_items.uncollect_confirm"}}"
          hx-target="closest .drop-items" hx-swap="outerHTML">{{.Login}}</button>{{end}}
      {{else}}
        <span class="cmark-none">—</span>
      {{end}}
      {{if .Addable}}
      <details class="cmark-add" onclick="event.stopPropagation()">
        <summary class="cmark-add-btn" title="{{t "campaign_items.mark_collected"}}">+</summary>
        <div class="cmark-add-menu">
          <span class="cmark-add-head">{{t "campaign_items.mark_collected_for"}}</span>
          {{$bid := .BenefitIDForAdd}}
          {{range .Addable}}<button type="button" class="cmark-add-opt cmark-{{.Platform}}"
            hx-post="/drops/claim/add"
            hx-vals='{"account_id":"{{.AccountID}}","benefit_id":"{{$bid}}","campaign_id":"{{$.ID}}"}'
            hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}'
            hx-target="closest .drop-items" hx-swap="outerHTML">{{.Login}}</button>{{end}}
        </div>
      </details>
      {{end}}
    </span>
```

> The add buttons need the benefit id. `collectedMark` carries `BenefitID`, but a row with zero existing `Collected` has none to read. Add a `BenefitID` to `campaignBenefitRow` so the template always has it:

- [ ] **Step 3: Carry the benefit id on the row.** In `internal/api/handlers_drops.go`, add `BenefitID string` to `campaignBenefitRow` and set it in `renderCampaignItems` (`BenefitID: b.ID`). Then in the template use `.BenefitID` instead of the `BenefitIDForAdd` placeholder — i.e. replace `{{$bid := .BenefitIDForAdd}}` with `{{$bid := .BenefitID}}`.

- [ ] **Step 4: Add CSS.** In `internal/web/static/css/app.css`, near the `.cmark` rules:

```css
  /* Manual "mark collected" add menu on a benefit row. */
  .cmark-add { position: relative; display: inline-block; }
  .cmark-add-btn { cursor: pointer; list-style: none; padding: 1px 7px; border: 1px dashed var(--muted);
    border-radius: 10px; color: var(--muted); font-size: 11px; }
  .cmark-add-btn::-webkit-details-marker { display: none; }
  .cmark-add[open] .cmark-add-btn { color: var(--accent); border-color: var(--accent); }
  .cmark-add-menu { position: absolute; right: 0; z-index: 20; margin-top: 4px; padding: 6px;
    background: var(--panel, #161616); border: 1px solid var(--border, #2a2a2a); border-radius: 8px;
    display: flex; flex-direction: column; gap: 4px; min-width: 140px; box-shadow: 0 4px 14px rgba(0,0,0,0.4); }
  .cmark-add-head { color: var(--muted); font-size: 10px; text-transform: uppercase; letter-spacing: 0.04em; }
  .cmark-add-opt { cursor: pointer; text-align: left; padding: 3px 6px; border-radius: 6px;
    border: 1px solid transparent; background: transparent; color: var(--text, #e8e8e8); font-size: 11px; }
  .cmark-add-opt:hover { border-color: var(--accent); }
```

> Use existing CSS variables; if `--panel`/`--border`/`--text` aren't defined in this stylesheet, fall back to the literal hexes shown (already provided as the second value).

- [ ] **Step 5: Write a render test.** Append to `internal/api/handlers_drops_collect_test.go`:

```go
func TestCampaignItems_AddMenuListsEligibleAccounts(t *testing.T) {
	detail := campaignDetailRow{
		ID: "camp-1", Platform: "twitch", CSRFToken: "csrf",
		Benefits: []campaignBenefitRow{{
			Name: "Builder Cape", RequiredMinutes: 5, BenefitID: "ben-1",
			Collected: nil,
			Addable:   []addableAccount{{Login: "TTik3r", Platform: "twitch", AccountID: "a1"}},
		}},
	}
	out := renderCampaignItems_render(t, detail)
	if !strings.Contains(out, `hx-post="/drops/claim/add"`) {
		t.Errorf("add menu missing the claim/add post")
	}
	if !strings.Contains(out, `"benefit_id":"ben-1"`) {
		t.Errorf("add option must carry the benefit id")
	}
	if !strings.Contains(out, ">TTik3r<") {
		t.Errorf("add menu must list the eligible account")
	}
}

// renderCampaignItems_render executes the items partial directly.
func renderCampaignItems_render(t *testing.T, detail campaignDetailRow) string {
	t.Helper()
	tmpl, err := web.Templates()
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "drops_campaign_items", detail))
	return buf.String()
}
```

(Add `bytes` + `github.com/aalejandrofer/grubdrops/internal/web` imports if not already present.)

- [ ] **Step 6: Run tests.** Run: `go test ./internal/api/ -run TestCampaignItems_AddMenuListsEligibleAccounts -v` and `go test ./internal/i18n/ -run TestLocaleParity -v`. Expected: both PASS.

- [ ] **Step 7: Commit.**

```bash
gofmt -w internal/api/handlers_drops.go internal/api/handlers_drops_collect_test.go
git add internal/api/ internal/web/ internal/i18n/
git commit -m "feat(drops): + menu to manually mark a benefit collected per account"
```

---

### Task 8: Changelog + full verification gate

**Files:**
- Modify: `docs/CHANGELOG.md`

- [ ] **Step 1: Add the changelog entry.** Under `## [Unreleased]`, add an `### Added`:

```markdown
### Added

- **Manually mark a drop collected.** If you claimed a drop outside GrubDrops
  (e.g. redeemed it directly), open the drop on `/drops`, click the `+` in a
  benefit's COLLECTED column, and pick the account. The mark is protected: the
  watcher's self-heal will not clear it (unlike an auto claim), so it survives
  even while the drop is still in progress. Uncollecting it (click the chip)
  removes the protection. Item lists now also load for drops on disabled
  accounts.
```

- [ ] **Step 2: Full build + test.** Run:

```bash
gofmt -l internal/ cmd/
go build ./...
go test ./internal/api/ ./internal/watcher/ ./internal/store/ ./internal/i18n/
```

Expected: `gofmt -l` prints nothing; build clean; all packages `ok`.

- [ ] **Step 3: Commit.**

```bash
git add docs/CHANGELOG.md && git commit -m "docs(changelog): manual mark-collected"
```

---

## Self-Review

**Spec coverage:**
- Items always load (broaden `sessionForPlatform`) → Task 4. ✓
- Mark writes claim + override → Task 5. ✓
- Watcher coexistence (`ForceCollected`, both prune sites) → Task 2 + wiring Task 3. ✓
- API `POST /drops/claim/add` + `removeClaim` clears override → Task 5. ✓
- UI `+` menu of matching-platform, not-already-collected accounts (incl disabled) → Tasks 6 + 7. ✓
- Itemless-campaign limitation → no task needed (the existing `no_items` branch renders; `Addable` only appears on real benefit rows). ✓
- Tests: addClaim writes both / removeClaim clears both (T5), eligible-account filter (T6), reconcile skips overridden + still prunes others (T2), session fallback (T4), render menu (T7). ✓

**Type consistency:** override key `store.CollectOverridePrefix + benefitID + ":" + accountID` is identical in Task 5 (write), Task 5 (delete), and Task 3 (loader reads `benefitID:accountID`). `ForceCollected func(accountID, benefitID string) bool` signature matches in Task 2 (field/call), Task 3 (loader return). `campaignBenefitRow` gains `Addable []addableAccount` (T6) and `BenefitID string` (T7), both set in `renderCampaignItems`. `addableAccount{Login,Platform,AccountID}` consistent T6↔T7.

**Implementer confirmations flagged inline:** `store.SessionStore.Put` signature (T4), `web.Templates()` satisfies `Renderer` + a `testRenderer` helper (T5), CSS variable names (T7). These are small, local checks; defaults provided.
