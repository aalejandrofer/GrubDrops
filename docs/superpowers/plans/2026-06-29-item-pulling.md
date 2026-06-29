# Better item-pulling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `/drops` items panel always resolve (never hang), show sub/action (0-minute) drops without mining them, and backfill Kick campaign items on open.

**Architecture:** Web layer — items endpoint returns a 200 partial on any lookup failure + a `pathEscape` template func so synth ids resolve. Discovery — Twitch `fetchDetails` stops discarding 0-minute drops and the watcher's benefit pick gains a `RequiredMinutes<=0` skip so they display but never mine. Kick gains a `CampaignDetailer` reusing a pure reward→benefit mapping.

**Tech Stack:** Go, sqlc/SQLite, html/template, HTMX, testify, chi.

## Global Constraints

- **gofmt:** run `gofmt -w` on every changed `.go` file before committing (CI gofmt gate fails fast).
- **i18n:** new UI strings need keys in all three locales (`en.json`, `es.json`, `zh-CN.json`); `internal/i18n` `TestLocaleParity` fails on drift.
- **CHANGELOG:** log every change under `## [Unreleased]` in `docs/CHANGELOG.md`.
- **No `queries/*.sql` changes** are required by this plan; do not add any.
- **Discovery/accrual safety:** Tasks 3 and 4 touch Twitch discovery + the watcher mining pick. The 0-minute filter that currently lives in `fetchDetails` is being MOVED to the watcher pick — display gains 0-min drops, the miner must still never pick one. Existing watcher tests (`TestWatcher_*`, esp. mining/prune) must stay green. These two tasks require live-drop verification before any version tag.
- **Honest limit (icons):** synth scrape benefits have no image source; the `.ph` placeholder stays. Real Twitch (GQL `imageAssetURL`) and Kick (reward image) benefits already map images — this plan only preserves that; it does not fabricate images.

---

### Task 1: Items endpoint never returns non-2xx (kills "stuck Loading")

**Files:**
- Modify: `internal/api/handlers_drops.go` — `campaignDetailRow` struct (~775-785) + `renderCampaignItems` GetCampaign error branch (~1324-1327)
- Modify: `internal/web/templates/_drops_campaign_items.html` (the `{{else}}` no-benefits branch, line ~49)
- Modify: `internal/i18n/locales/en.json`, `es.json`, `zh-CN.json` (add `campaign_items.load_error`)
- Test: `internal/api/handlers_drops_items_test.go` (create)

**Interfaces:**
- Produces: `campaignDetailRow.LoadError bool`. When the items partial is rendered with `LoadError: true`, it shows the load-error message; HTMX swaps a 200 so the row never hangs.

- [ ] **Step 1: Add i18n key (all three locales).** In `internal/i18n/locales/en.json`, directly after the `"campaign_items.no_items": ...` line:

```json
  "campaign_items.load_error": "Couldn't load items for this campaign.",
```

In `es.json` after its `campaign_items.no_items`:

```json
  "campaign_items.load_error": "No se pudieron cargar los ítems de esta campaña.",
```

In `zh-CN.json` after its `campaign_items.no_items`:

```json
  "campaign_items.load_error": "无法加载此活动的物品。",
```

- [ ] **Step 2: Add the `LoadError` field.** In `internal/api/handlers_drops.go`, add to `campaignDetailRow` (after `CSRFToken`):

```go
	CSRFToken    string // for the inline "mark uncollected" action on each mark
	// LoadError is true when the campaign couldn't be loaded (e.g. unknown or
	// malformed id). The items partial then renders a friendly message with a
	// 200, so HTMX always swaps and the row never hangs on "Loading…".
	LoadError bool
```

- [ ] **Step 3: Write the failing test.** Create `internal/api/handlers_drops_items_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestRenderCampaignItems_UnknownID_Renders200LoadError proves the items panel
// never 404s on an unknown/malformed id (which left HTMX showing a permanent
// "Loading…"). It must return 200 with the load-error message instead.
func TestRenderCampaignItems_UnknownID_Renders200LoadError(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	d := &dropsDeps{q: q, t: testRenderer(t), loc: time.UTC}
	req := httptest.NewRequest(http.MethodGet, "/drops/campaigns/no-such-id/items", nil)
	rec := httptest.NewRecorder()
	d.renderCampaignItems(rec, req, "no-such-id")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Couldn't load items")
}
```

(`testRenderer(t)` already exists in the `api` test package, added with the mark-collected feature.)

- [ ] **Step 4: Run test to verify it fails.** Run: `go test ./internal/api/ -run TestRenderCampaignItems_UnknownID_Renders200LoadError -v`. Expected: FAIL — current code calls `http.NotFound` → 404.

- [ ] **Step 5: Replace the NotFound branch.** In `renderCampaignItems`, change:

```go
	camp, err := d.q.GetCampaign(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
```

to:

```go
	camp, err := d.q.GetCampaign(r.Context(), id)
	if err != nil {
		// Render a 200 partial (not 404) so HTMX always swaps the panel —
		// otherwise the row hangs on "Loading…" forever (e.g. malformed
		// scrape-synth ids that can't round-trip the URL).
		renderPartial(w, r, d.t, "drops_campaign_items", campaignDetailRow{LoadError: true})
		return
	}
```

- [ ] **Step 6: Render the load-error message in the template.** In `internal/web/templates/_drops_campaign_items.html`, replace the final `{{else}}` block:

```html
{{else}}
<div class="ev-detail" style="color:var(--muted);font-size:11px;">{{t "campaign_items.no_items"}}</div>
{{end}}
```

with:

```html
{{else}}
<div class="ev-detail" style="color:var(--muted);font-size:11px;">{{if .LoadError}}{{t "campaign_items.load_error"}}{{else}}{{t "campaign_items.no_items"}}{{end}}</div>
{{end}}
```

- [ ] **Step 7: Run tests.** Run: `go test ./internal/api/ -run TestRenderCampaignItems_UnknownID_Renders200LoadError -v` and `go test ./internal/i18n/ -run TestLocaleParity -v`. Expected: both PASS.

- [ ] **Step 8: Commit.**

```bash
gofmt -w internal/api/handlers_drops.go internal/api/handlers_drops_items_test.go
git add internal/api/ internal/web/ internal/i18n/
git commit -m "fix(drops): items panel renders 200 load-error instead of 404 (no more stuck Loading)"
```

---

### Task 2: `pathEscape` template func + encode campaign id in item-panel URLs

**Files:**
- Modify: `internal/web/embed.go` (`sharedFuncs` FuncMap ~59-79; add `net/url` import)
- Modify: `internal/web/templates/_drops_table.html` (4 `hx-get` occurrences: lines ~34, ~70, ~118, ~167)
- Test: `internal/api/handlers_drops_items_test.go` (append)

**Interfaces:**
- Produces: template func `pathEscape` = `url.PathEscape`. Used as `{{pathEscape .CampaignID}}`.

- [ ] **Step 1: Write the failing test.** Append to `internal/api/handlers_drops_items_test.go`:

```go
import "bytes"
// (add to the existing import block; also needs internal/web)

// TestDropsTable_EscapesCampaignIDInItemsURL proves a malformed scrape-synth
// campaign id (spaces, "|") is percent-encoded in the items hx-get URL so the
// request can route, while a normal uuid id is left intact.
func TestDropsTable_EscapesCampaignIDInItemsURL(t *testing.T) {
	page := dropsPage{
		Tab: tabCurrent,
		Rows: []dropsRow{
			{CampaignID: "Minecraft|Tubbo's WatchTime Sun", Platform: "twitch", When: "x"},
			{CampaignID: "a37ae57b-2268-4472-a8b3", Platform: "twitch", When: "y"},
		},
	}
	out := renderDropsTable(t, page)
	if !strings.Contains(out, "/drops/campaigns/Minecraft%7CTubbo's%20WatchTime%20Sun/items") {
		t.Errorf("synth id not path-escaped in hx-get:\n%s", out)
	}
	if !strings.Contains(out, "/drops/campaigns/a37ae57b-2268-4472-a8b3/items") {
		t.Errorf("uuid id should be unchanged")
	}
}
```

(`renderDropsTable(t, page)` already exists in `internal/api/handlers_drops_test.go`. Note `url.PathEscape` leaves `'` unescaped and encodes space→`%20`, `|`→`%7C`.)

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/api/ -run TestDropsTable_EscapesCampaignIDInItemsURL -v`. Expected: FAIL — the raw id (with space/`|`) is emitted, no `pathEscape` func exists.

- [ ] **Step 3: Register `pathEscape`.** In `internal/web/embed.go`, add `"net/url"` to imports, then add to the `sharedFuncs` map literal (after the `"t":` entry):

```go
	"t":          i18n.TemplateFunc(),
	"pathEscape": url.PathEscape,
```

- [ ] **Step 4: Encode the id in all four item-panel URLs.** In `internal/web/templates/_drops_table.html`, replace each of the four occurrences of:

```html
hx-get="/drops/campaigns/{{.CampaignID}}/items"
```

with:

```html
hx-get="/drops/campaigns/{{pathEscape .CampaignID}}/items"
```

(There are exactly four — the whitelisted, not-linked, null-game, and discoverable row sections. Replace all.)

- [ ] **Step 5: Run tests.** Run: `go test ./internal/api/ -run TestDropsTable_EscapesCampaignIDInItemsURL -v` then `go build ./...`. Expected: PASS + clean build.

- [ ] **Step 6: Commit.**

```bash
gofmt -w internal/web/embed.go
git add internal/web/
git commit -m "fix(drops): path-escape campaign id in item-panel URLs so synth ids resolve"
```

---

### Task 3: Twitch `fetchDetails` includes 0-minute (sub/action) drops

**Files:**
- Modify: `internal/platform/twitch/campaigns.go` — `fetchDetails` 0-min skip (~362-374)
- Test: `internal/platform/twitch/campaigns_test.go` (append)

**Interfaces:**
- Produces: `fetchDetails` now returns benefits for 0-minute drops with `RequiredMinutes == 0` (previously skipped). Image/name/preconditions still mapped. Consumed by Task 4 (watcher must skip these for mining) and by the items panel (displays them dim).

- [ ] **Step 1: Write the failing test.** First inspect `internal/platform/twitch/campaigns_test.go` and its `testdata/` to mirror the existing `fetchDetails`/details-fixture test setup (the suite uses `newTestClient` against an `httptest` server returning op-keyed fixtures; the details op is `OpDropCampaignDetails`). Add a test that drives `discovery.fetchDetails` with a details response containing BOTH a watch drop (RequiredMinutesWatched 60) and a 0-minute drop, asserting both appear:

```go
// TestFetchDetails_IncludesZeroMinuteDrops proves sub/action drops
// (requiredMinutesWatched == 0) are no longer discarded — they must be
// returned as benefits (RequiredMinutes 0) so the /drops panel can show them.
func TestFetchDetails_IncludesZeroMinuteDrops(t *testing.T) {
	// Build a details response with one 60-min watch drop and one 0-min sub drop.
	detailsJSON := `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"watch1","requiredMinutesWatched":60,"benefitEdges":[{"benefit":{"id":"b1","name":"Watch Item","imageAssetURL":"http://img/w.png"}}]},
		{"id":"sub1","requiredMinutesWatched":0,"benefitEdges":[{"benefit":{"id":"b2","name":"Sub Item","imageAssetURL":"http://img/s.png"}}]}
	]}}}}`

	d := newDetailsTestDiscovery(t, detailsJSON) // see note below
	benefits, _, err := d.fetchDetails(context.Background(), platform.Session{AccessToken: "tok"}, "camp-uuid")
	require.NoError(t, err)

	var minutes = map[string]int{}
	for _, b := range benefits {
		minutes[b.Name] = b.RequiredMinutes
	}
	require.Contains(t, minutes, "Sub Item", "0-minute drop must be included now")
	require.Equal(t, 0, minutes["Sub Item"])
	require.Equal(t, 60, minutes["Watch Item"])
}
```

> **Note for implementer:** wire `newDetailsTestDiscovery(t, detailsJSON)` using the SAME pattern the existing tests use to construct a `discovery` against an `httptest` server (look at `newTestClient`/the server setup in `campaigns_test.go`). The server must return `detailsJSON` for the `OpDropCampaignDetails` gql op. If the existing harness already exposes a helper to stub a single op response, reuse it; otherwise add a minimal `httptest.Server` whose handler returns `detailsJSON`. Match the real struct tags in `campaignDetailsData` (`timeBasedDrops`, `requiredMinutesWatched`, `benefitEdges`, `benefit.imageAssetURL`).

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/platform/twitch/ -run TestFetchDetails_IncludesZeroMinuteDrops -v`. Expected: FAIL — "Sub Item" missing (currently skipped).

- [ ] **Step 3: Remove the 0-min skip.** In `internal/platform/twitch/campaigns.go` `fetchDetails`, delete the skip block:

```go
		if td.RequiredMinutesWatched <= 0 {
			slog.Info("twitch skipping non-watch drop (0 required minutes)",
				"campaign", campaignID, "drop", td.ID)
			continue
		}
```

The loop body then proceeds straight to building `preconds` and the `BenefitEdges` mapping for every drop. `RequiredMinutes: td.RequiredMinutesWatched` carries 0 for sub/action drops; the image/name/rewardID/preconditions mapping is unchanged.

- [ ] **Step 4: Run test to verify it passes.** Run: `go test ./internal/platform/twitch/ -run TestFetchDetails_IncludesZeroMinuteDrops -v`. Expected: PASS. Then run the whole package: `go test ./internal/platform/twitch/` — confirm no existing test relied on the skip; fix any fixture expectation that counted skipped drops.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/platform/twitch/campaigns.go internal/platform/twitch/campaigns_test.go
git add internal/platform/twitch/
git commit -m "feat(twitch): include 0-minute (sub/action) drops as benefits for display"
```

---

### Task 4: Watcher skips 0-minute drops for mining

**Files:**
- Modify: `internal/watcher/watcher.go` — benefit pick loop (~1263, inside `for _, b := range benefits`)
- Test: `internal/watcher/watcher_test.go` (append)

**Interfaces:**
- Consumes: 0-minute benefits now present in `c.Benefits` (Task 3).
- Produces: the watcher never selects a `RequiredMinutes <= 0` benefit for mining.

- [ ] **Step 1: Write the failing test.** Append to `internal/watcher/watcher_test.go`, mirroring the existing backend-override + `Eventually` style (e.g. `TestWatcher_MinesUntilClaim`, `multiTierBackend`):

```go
// zeroMinBackend returns a single active campaign whose only drop is a
// 0-minute (sub/action) drop. The watcher must NOT pick it for mining.
type zeroMinBackend struct{ *platformtest.MockBackend }

func (b *zeroMinBackend) ListActiveCampaigns(_ context.Context, _ platform.Session) ([]platform.Campaign, error) {
	return []platform.Campaign{{
		ID: "subcamp", Game: "League of Legends", Status: "active", AccountLinked: true,
		Benefits: []platform.DropBenefit{{ID: "sub1", CampaignID: "subcamp", Name: "Sub Drop", RequiredMinutes: 0}},
	}}, nil
}
func (b *zeroMinBackend) ListEligibleChannels(_ context.Context, _ platform.Session, _ platform.Campaign) ([]platform.Stream, error) {
	return []platform.Stream{{Channel: "lolesports", DropsEnabled: true}}, nil
}

// TestWatcher_SkipsZeroMinuteDropForMining proves a 0-minute-only campaign is
// never picked: the watcher finds no eligible benefit and never starts watching.
func TestWatcher_SkipsZeroMinuteDropForMining(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	rec := &recordingClaimRecorder{}
	w := New(Config{
		AccountID:     "acc_zero",
		Backend:       &zeroMinBackend{MockBackend: platformtest.New()},
		ClaimRecorder: rec,
		Session:       platform.Session{AccessToken: "tok"},
		Notifier:      &recordingNotifier{},
		TickInterval:  2 * time.Millisecond,
		AllowGame:     func(g string) bool { return g == "League of Legends" },
	})
	go func() { _ = w.Run(ctx) }()
	<-ctx.Done()

	// Never claimed/recorded anything, and never settled into the watching state.
	recorded, pruned := rec.snapshot()
	if len(recorded) != 0 || len(pruned) != 0 {
		t.Fatalf("0-min campaign must not drive claims: recorded=%v pruned=%v", recorded, pruned)
	}
	if w.State() == StateWatching {
		t.Fatalf("watcher must not be watching a 0-minute-only campaign")
	}
}
```

> **Note for implementer:** confirm the public accessor for the watcher's state (`w.State()`) and the watching state constant (`StateWatching`) exist with those names; if they differ, adjust the final assertion to the real API (the key assertion is "no claim recorded/pruned and not mining"). Reuse `recordingClaimRecorder`, `recordingNotifier`, `platformtest` already in the test file.

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/watcher/ -run TestWatcher_SkipsZeroMinuteDropForMining -v`. Expected: FAIL — the watcher picks `sub1` (0-min sorts first) and proceeds to watch.

- [ ] **Step 3: Add the mining skip.** In `internal/watcher/watcher.go`, inside the `for _, b := range benefits {` loop (right after the sort, as the first guard in the loop body, before the `claimed[b.ID]` check):

```go
	for _, b := range benefits {
		// Watch-time drops only. 0-minute drops (sub/gift/action-gated, e.g.
		// the LoL "1 Sub or Gift Sub" drop) can't be earned by watching, so
		// the miner must never pick one. They are still discovered + shown on
		// /drops (dim, "action required"); they're just not mineable.
		if b.RequiredMinutes <= 0 {
			continue
		}
		if claimed[b.ID] || ownClaimed[b.ID] {
			continue
		}
```

- [ ] **Step 4: Run tests.** Run: `go test ./internal/watcher/ -run TestWatcher_SkipsZeroMinuteDropForMining -v`, then the full package `go test ./internal/watcher/`. Expected: new test PASS; all existing watcher tests still PASS.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/watcher/watcher.go internal/watcher/watcher_test.go
git add internal/watcher/
git commit -m "feat(watcher): never pick 0-minute drops for mining (display-only)"
```

---

### Task 5: Kick `CampaignDetailer` (backfill Kick item panels)

**Files:**
- Modify: `internal/platform/kick/backend.go` — extract a pure `kickRewardsToBenefits` helper, reuse it in `ListActiveCampaigns`, add `CampaignDetails`
- Test: `internal/platform/kick/backend_test.go` (create, or append if exists)

**Interfaces:**
- Produces: `func (b *Backend) CampaignDetails(ctx context.Context, s platform.Session, campaignID string) ([]platform.DropBenefit, error)` — satisfies `platform.CampaignDetailer`; `func kickRewardsToBenefits(campaignID string, rewards []kickReward) []platform.DropBenefit` (pure).

- [ ] **Step 1: Write the failing test.** Create `internal/platform/kick/backend_test.go` (package `kick`):

```go
package kick

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKickRewardsToBenefits_MapsAndDefaultsMinutes proves the reward→benefit
// mapping: fields map across, and a reward with no required minutes defaults to
// 120 (Kick drops typically need ~2h), matching ListActiveCampaigns.
func TestKickRewardsToBenefits_MapsAndDefaultsMinutes(t *testing.T) {
	rewards := []kickReward{
		{ID: "r1", Name: "Skin", RequiredMinutes: 90, ImageURL: "http://img/r1.png"},
		{ID: "r2", Name: "Emote", RequiredMinutes: 0, ImageURL: "http://img/r2.png"},
	}
	got := kickRewardsToBenefits("camp-1", rewards)

	require.Len(t, got, 2)
	require.Equal(t, "camp-1", got[0].CampaignID)
	require.Equal(t, 90, got[0].RequiredMinutes)
	require.Equal(t, "Skin", got[0].Name)
	require.Equal(t, "http://img/r1.png", got[0].ImageURL)
	require.Equal(t, 120, got[1].RequiredMinutes, "0 required minutes defaults to 120")
}
```

> **Note for implementer:** confirm the `kickReward` field names (`ID, Name, RequiredMinutes, ImageURL`) against `internal/platform/kick/api.go`; adjust the literal if they differ.

- [ ] **Step 2: Run test to verify it fails.** Run: `go test ./internal/platform/kick/ -run TestKickRewardsToBenefits_MapsAndDefaultsMinutes -v`. Expected: FAIL — `kickRewardsToBenefits` undefined.

- [ ] **Step 3: Add the pure helper.** In `internal/platform/kick/backend.go`, add (near `ListActiveCampaigns`):

```go
// kickRewardsToBenefits maps a Kick campaign's rewards to platform benefits.
// A reward with no required minutes defaults to 120 (Kick drops typically need
// ~2h). Shared by ListActiveCampaigns and CampaignDetails.
func kickRewardsToBenefits(campaignID string, rewards []kickReward) []platform.DropBenefit {
	benefits := make([]platform.DropBenefit, 0, len(rewards))
	for _, ben := range rewards {
		required := ben.RequiredMinutes
		if required <= 0 {
			required = 120
		}
		benefits = append(benefits, platform.DropBenefit{
			ID:              ben.ID,
			CampaignID:      campaignID,
			Name:            ben.Name,
			RequiredMinutes: required,
			ImageURL:        ben.ImageURL,
		})
	}
	return benefits
}
```

- [ ] **Step 4: Reuse it in `ListActiveCampaigns`.** In `ListActiveCampaigns`, replace the inline benefit-building loop:

```go
		benefits := make([]platform.DropBenefit, 0, len(c.Rewards))
		for _, ben := range c.Rewards {
			required := ben.RequiredMinutes
			if required <= 0 {
				required = 120 // Kick drops typically require ~2h
			}
			benefits = append(benefits, platform.DropBenefit{
				ID:              ben.ID,
				CampaignID:      c.ID,
				Name:            ben.Name,
				RequiredMinutes: required,
				ImageURL:        ben.ImageURL,
			})
		}
```

with:

```go
		benefits := kickRewardsToBenefits(c.ID, c.Rewards)
```

- [ ] **Step 5: Add `CampaignDetails`.** In `internal/platform/kick/backend.go`, add:

```go
// CampaignDetails fetches one campaign's benefits on demand so the /drops items
// panel can backfill Kick campaigns (Kick has no per-campaign detail endpoint,
// so we list campaigns and pick the matching one). Returns (nil, nil) when the
// campaign isn't found — the panel then shows the clean "no items" state.
// Satisfies platform.CampaignDetailer.
func (b *Backend) CampaignDetails(ctx context.Context, s platform.Session, campaignID string) ([]platform.DropBenefit, error) {
	camps, err := b.api.Campaigns(ctx, s)
	if err != nil {
		return nil, err
	}
	for _, c := range camps {
		if c.ID == campaignID {
			return kickRewardsToBenefits(c.ID, c.Rewards), nil
		}
	}
	return nil, nil
}
```

- [ ] **Step 6: Run tests.** Run: `go test ./internal/platform/kick/ -run TestKickRewardsToBenefits_MapsAndDefaultsMinutes -v`, then `go build ./...` (confirms `*Backend` still compiles and now satisfies `CampaignDetailer` where used). Expected: PASS + clean build.

- [ ] **Step 7: Commit.**

```bash
gofmt -w internal/platform/kick/backend.go internal/platform/kick/backend_test.go
git add internal/platform/kick/
git commit -m "feat(kick): implement CampaignDetailer so item panels backfill on open"
```

---

### Task 6: Changelog + full verification gate

**Files:**
- Modify: `docs/CHANGELOG.md`

- [ ] **Step 1: Add the changelog entry.** Under `## [Unreleased]`, add (merge into existing `### Fixed`/`### Added` if present, else add the headers):

```markdown
### Fixed

- **Drop item lists no longer get stuck on "Loading…".** The items panel now
  always renders (it returns a friendly "couldn't load items" message instead
  of a silent 404), and campaign ids from the scrape fallback are URL-encoded
  so they resolve.

### Added

- **Sub / action drops now show in a campaign's item list** (e.g. a "1 Sub or
  Gift Sub" drop), rendered as "action required". The miner still ignores them
  — they can't be earned by watching.
- **Kick campaign item lists backfill on open**, matching Twitch.
```

- [ ] **Step 2: Full gate.** Run:

```bash
gofmt -l internal/ cmd/
go build ./...
go test ./internal/api/ ./internal/watcher/ ./internal/store/ ./internal/i18n/ ./internal/platform/twitch/ ./internal/platform/kick/
```

Expected: `gofmt -l` prints nothing; build clean; all packages `ok`.

- [ ] **Step 3: Commit.**

```bash
git add docs/CHANGELOG.md
git commit -m "docs(changelog): item-pulling fixes"
```

---

## Self-Review

**Spec coverage:**
- #1 never-hang + pathEscape → Tasks 1 + 2. ✓
- #2 show sub/action drops + watcher skip → Tasks 3 + 4. ✓
- #3 Kick CampaignDetailer (+ non-whitelisted Twitch via #2 + merged session fallback) → Task 5. ✓
- #4 icons: real GQL/Kick mapping preserved (Task 3 keeps `ImageURL` in the BenefitEdges loop; Task 5 keeps `ben.ImageURL`); synth has no source (honest limit, no task). ✓
- Live-drop gate for #2/#3 noted in Global Constraints. ✓

**Placeholder scan:** Two implementer-confirmation notes (Task 3 details-test harness wiring, Task 4 `w.State()`/`StateWatching` accessor names, Task 5 `kickReward` field names) — these are "verify the real symbol then use it", with the exact assertion intent given; not vague directives. No TBDs.

**Type consistency:** `kickRewardsToBenefits(campaignID string, rewards []kickReward) []platform.DropBenefit` identical in Tasks 5 step-3/4/5. `campaignDetailRow.LoadError bool` defined Task 1, used Task 1 template. `pathEscape` registered Task 2, used Task 2 template. Watcher guard `b.RequiredMinutes <= 0` (Task 4) matches the field on `platform.DropBenefit` (int). Twitch `fetchDetails` returning `RequiredMinutes == 0` (Task 3) is exactly what Task 4 skips and the template renders dim (`{{if le .RequiredMinutes 0}}`).
