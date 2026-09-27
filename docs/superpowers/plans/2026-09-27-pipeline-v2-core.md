# Pipeline v2 Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the headless pipeline v2 (platform-authoritative drop state, reconciler, pure planner with streamer priority, claimer, session, per-account loop), selectable per account behind `GRUB_PIPELINE=v2`, with v1 untouched.

**Architecture:** One `drop_state` row per (account, drop) is the single mining truth. A reconciler folds platform per-campaign detail into it; a pure planner turns state + live channels into a decision (channel + drops served); a session runs accrual on that channel; a claimer turns claimable rows into claimed/blocked. One goroutine per account (the loop) owns every state transition. v1 (`internal/watcher`) is not modified except where noted (scheduler interfaces, a Twitch claim refactor that keeps behaviour identical).

**Tech Stack:** Go 1.x, SQLite via sqlc + goose migrations, testify. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-27-pipeline-v2-design.md`

**Scope split:** This is plan 1 of 2. Plan 2 (written after this lands) covers the UI: inventory page reading `drop_state`, mark/skip/retry actions via `Loop.Nudge`, streamer-priority editor, per-account pipeline toggle. Plan 1 is usable on its own: v2 mines headlessly, and the existing UI keeps working (manual marks reach v2 through a claims-table bridge on loop start, because the mark handler already triggers a reload).

**Refinements to the spec made here (recorded in the spec in Task 13):**
- Reconcile runs serially on the loop goroutine (not its own goroutine). Single writer, no result races; session and PubSub events buffer meanwhile.
- Timestamps are INTEGER unix seconds (repo convention), `0` = unset. `block_reason` is `NOT NULL DEFAULT ''`.
- `user_skip` has no auto-expiry (`retry_after = 0`); it is explicit user intent and is cleared only by the user (Plan 2 "retry").
- Platform interface names: `DropProgressSource`, `DropClaimer`, `ChannelProber` in `internal/platform`.

## Global Constraints

- Twitch heartbeat cadence is **fixed at 60s** (`BeatEvery = 60 * time.Second`, not configurable). Anything above 60s under-credits Twitch.
- Never send a `Client-Integrity` header (no Twitch HTTP changes in this plan add headers).
- **Never put `?` or parentheses in a `queries/*.sql` comment.**
- Do not delete or modify `internal/watcher`, the browser sidecar stack, `proto/`, `internal/dockerctl`, `internal/auth/browser`.
- `gofmt -w .` before every commit; `go build ./...` and `go test ./...` green before every commit.
- Every change gets a line in `docs/CHANGELOG.md` under `## [Unreleased]` (done in Task 13 for the whole feature).
- Default pipeline stays `v1`. v2 is enabled only by `GRUB_PIPELINE=v2` or kv `pipeline_override:<accountID>` = `v2`.
- Accrual/claim change: **no version tag** until live-drop verification on staging (Task 14) passes on Twitch and Kick.
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Review Focus

1. **Platform error mid-reconcile** (Twitch details 500, Kick progress 403): state must not change at all. Pinned in Task 7 (`TestRun_ProgressErrorChangesNothing`).
2. **Stale session events after a channel swap**: a `StreamDown` from the old channel must not put the new channel on cooldown. Pinned in Task 11 (`TestLoop_StaleStreamDownIgnored`).
3. **Twitch drop with `self: null`** (campaign not yet enrolled): must be `eligible`, never `claimed`. Pinned in Task 5 (`TestDropProgress_NullSelfIsUnclaimed`).
4. **Kick reward absent from `/drops/progress`**: never-watched stays `eligible`; absent after accruing goes `blocked:not_enrolled` then re-derives after 1h (no permanent skip). Pinned in Task 2 table cases.
5. **Multi-item Twitch drop** (one drop id repeated across benefit edges): exactly one state row and one claim. Pinned in Task 5 (`TestDropProgress_DedupesMultiItemDrop`) and Task 7 (`TestRun_MultiItemDropOneRow`).

---

### Task 1: Live verification probes (gate)

Answers spec §9 before anything builds on it. Probes are committed behind the `live` build tag so they never run in CI.

**Files:**
- Create: `internal/store/live_session_test.go`
- Create: `internal/platform/twitch/live_probe_test.go`
- Create: `internal/platform/kick/live_probe_test.go`
- Modify: `docs/superpowers/specs/2026-09-27-pipeline-v2-design.md` (§9 results)

**Interfaces:**
- Consumes: `store.Open`, `store.NewCryptor`, `store.NewSessionStore`, `(*SessionStore).Get`; twitch `New()`, `(*discovery).resolveCurrentLogin`, `(*client).gql`, `OpDropCampaignDetails`; kick `New(...)`, `b.api.d.do`, `dropsBase`.
- Produces: nothing used by later tasks except the recorded answers.

- [ ] **Step 1: Session dump helper**

```go
//go:build live

package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestLive_DumpSession prints one account's decrypted session as JSON.
// Run against a COPY of the DB file: Open runs migrations. Output holds
// live tokens; do not paste it anywhere.
//
//	GRUB_DB_PATH=/tmp/miner-copy.db GRUB_MASTER_KEY=... GRUB_LIVE_ACCOUNT=acc_x \
//	  go test -tags live -run TestLive_DumpSession -v ./internal/store/
func TestLive_DumpSession(t *testing.T) {
	path, key, acct := os.Getenv("GRUB_DB_PATH"), os.Getenv("GRUB_MASTER_KEY"), os.Getenv("GRUB_LIVE_ACCOUNT")
	if path == "" || key == "" || acct == "" {
		t.Skip("set GRUB_DB_PATH, GRUB_MASTER_KEY, GRUB_LIVE_ACCOUNT")
	}
	ctx := context.Background()
	db, err := Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	c, err := NewCryptor(key)
	require.NoError(t, err)
	s, ok, err := NewSessionStore(db, gen.New(db), c).Get(ctx, acct)
	require.NoError(t, err)
	require.True(t, ok, "no session for account")
	b, err := json.Marshal(s)
	require.NoError(t, err)
	t.Logf("%s", b)
}
```

- [ ] **Step 2: Twitch probe**

```go
//go:build live

package twitch

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestLive_DetailsSelf dumps raw DropCampaignDetails for a campaign the
// account is enrolled in (ideally one fully claimed), to confirm
// timeBasedDrops[].self carries isClaimed + currentMinutesWatched.
//
//	GRUB_LIVE_TWITCH_TOKEN=... GRUB_LIVE_CAMPAIGN_ID=... \
//	  go test -tags live -run TestLive_DetailsSelf -v ./internal/platform/twitch/
func TestLive_DetailsSelf(t *testing.T) {
	tok, camp := os.Getenv("GRUB_LIVE_TWITCH_TOKEN"), os.Getenv("GRUB_LIVE_CAMPAIGN_ID")
	if tok == "" || camp == "" {
		t.Skip("set GRUB_LIVE_TWITCH_TOKEN, GRUB_LIVE_CAMPAIGN_ID")
	}
	ctx := context.Background()
	b := New()
	login, err := b.disc.resolveCurrentLogin(ctx, platform.Session{AccessToken: tok})
	require.NoError(t, err)
	var raw json.RawMessage
	require.NoError(t, b.c.gql(ctx, tok, OpDropCampaignDetails,
		map[string]any{"dropID": camp, "channelLogin": login}, &raw))
	t.Logf("%s", raw)
}
```

- [ ] **Step 3: Kick probe**

```go
//go:build live

package kick

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// TestLive_ProgressRaw dumps raw /drops/progress for a Kick account that has
// at least one CLAIMED reward, to learn whether claimed rewards stay listed.
//
//	GRUB_LIVE_KICK_SESSION_JSON='{"cookies":{...}}' \
//	  go test -tags live -run TestLive_ProgressRaw -v ./internal/platform/kick/
func TestLive_ProgressRaw(t *testing.T) {
	js := os.Getenv("GRUB_LIVE_KICK_SESSION_JSON")
	if js == "" {
		t.Skip("set GRUB_LIVE_KICK_SESSION_JSON")
	}
	var s platform.Session
	require.NoError(t, json.Unmarshal([]byte(js), &s))
	b := New(nil, nil, "grubdrops-browser-{slug}", 9090, 10*time.Minute)
	body, status, err := b.api.d.do(context.Background(), s, http.MethodGet, dropsBase+"/api/v1/drops/progress", nil)
	require.NoError(t, err)
	t.Logf("status=%d body=%s", status, body)
}
```

- [ ] **Step 4: Confirm they compile and skip without env**

Run: `go vet -tags live ./internal/store/ ./internal/platform/twitch/ ./internal/platform/kick/ && go test -tags live -run 'TestLive_' ./internal/store/ ./internal/platform/twitch/ ./internal/platform/kick/ -v`
Expected: three `--- SKIP` lines, exit 0. If `b.api.d` does not compile (field names differ), read `internal/platform/kick/api.go` for the doer field and adjust.

- [ ] **Step 5: HUMAN step: run against real accounts**

Ask the user to run Steps 1 to 3 with real values (DB copy from prod host, one Twitch account with a completed claimed campaign, one Kick account with a claimed reward). Do not run them yourself with prod secrets unless the user hands them over.

- [ ] **Step 6: Record answers in spec §9**

Replace §9 body with the two answers, for example:

```markdown
## 9. Verification results (2026-09-XX)

1. Twitch `DropCampaignDetails` for an enrolled completed campaign: `self` present = YES/NO.
   Sample: `{"id":"...","self":{"isClaimed":true,"currentMinutesWatched":120,...}}`
2. Kick `/drops/progress` keeps claimed rewards listed: YES/NO.
```

If Twitch answer is NO: stop and tell the user; §4.2 needs revisiting before Task 5. If Kick answer is NO: no code change; the absent-reward rule in Task 2 already covers it.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/store internal/platform
git add internal/store/live_session_test.go internal/platform/twitch/live_probe_test.go internal/platform/kick/live_probe_test.go docs/superpowers/specs/2026-09-27-pipeline-v2-design.md
git commit -m "test(pipeline-v2): live probes for Twitch details self and Kick claimed retention

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Drop state model (`dropstate`)

**Files:**
- Create: `internal/pipeline/dropstate/dropstate.go`
- Test: `internal/pipeline/dropstate/dropstate_test.go`

**Interfaces:**
- Consumes: nothing (stdlib `time` only).
- Produces:
  - `type Status string` with `Eligible, Accruing, Claimable, Claimed, Blocked`
  - `type Reason string` with `NoReason, NeedsLink, ClaimFailed, SubOnly, NoChannels, Expired, NotEnrolled, UserSkip`
  - `type Source string` with `FromPlatform, FromUser`
  - `type Row struct { AccountID, DropID, CampaignID, Platform string; Status Status; Reason Reason; Minutes, Required int; Source Source; FailCount int; RetryAfter, SyncedAt, UpdatedAt time.Time }`, `func (r Row) IsZero() bool`
  - `type Observation struct { Known, Unmineable, Claimed bool; Minutes, Required int }`
  - `func Apply(prev Row, obs Observation, now time.Time) Row`
  - `func ClaimOK(prev Row, now time.Time) Row`, `func ClaimNeedsLink(prev Row, now time.Time) Row`, `func ClaimFailedAttempt(prev Row, now time.Time) Row`
  - `func MarkCollected(prev Row, now time.Time) Row`, `func Skip(prev Row, now time.Time) Row`, `func Retry(prev Row, now time.Time) Row`, `func Expire(prev Row, now time.Time) Row`, `func BlockLink(prev Row, now time.Time) Row`
  - `func Mineable(r Row) bool`, `func ReadyToClaim(r Row, now time.Time) bool`
  - consts `RetryNeedsLink=24h, RetryClaimFailed=6h, RetryNotEnrolled=1h, RetrySubOnly=24h, RetryExpired=24h, MaxClaimFailures=5`

- [ ] **Step 1: Write the failing tests**

```go
package dropstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Unix(1_000_000, 0)

func base() Row {
	return Row{AccountID: "a", DropID: "d", CampaignID: "c", Platform: "twitch", Source: FromPlatform}
}

func row(s Status, r Reason, min, req int) Row {
	x := base()
	x.Status, x.Reason, x.Minutes, x.Required = s, r, min, req
	return x
}

func TestApply(t *testing.T) {
	userClaimed := row(Claimed, NoReason, 0, 60)
	userClaimed.Source = FromUser
	notEnrolled := row(Blocked, NotEnrolled, 10, 60)
	notEnrolled.RetryAfter = t0.Add(time.Hour)
	skipped := row(Blocked, UserSkip, 0, 60)
	skipped.Source = FromUser
	needsLink := row(Blocked, NeedsLink, 60, 60)
	needsLink.RetryAfter = t0.Add(RetryNeedsLink)

	cases := []struct {
		name   string
		prev   Row
		obs    Observation
		at     time.Time
		want   Status
		reason Reason
		src    Source
	}{
		{"new drop, platform silent -> eligible", base(), Observation{Required: 60}, t0, Eligible, NoReason, FromPlatform},
		{"new drop, platform silent, zero required -> sub_only", base(), Observation{}, t0, Blocked, SubOnly, FromPlatform},
		{"known progress -> accruing", row(Eligible, NoReason, 0, 60), Observation{Known: true, Minutes: 10, Required: 60}, t0, Accruing, NoReason, FromPlatform},
		{"known full -> claimable", row(Accruing, NoReason, 50, 60), Observation{Known: true, Minutes: 60, Required: 60}, t0, Claimable, NoReason, FromPlatform},
		{"known claimed -> claimed", row(Accruing, NoReason, 10, 60), Observation{Known: true, Claimed: true, Minutes: 60, Required: 60}, t0, Claimed, NoReason, FromPlatform},
		{"platform claimed never demoted", row(Claimed, NoReason, 60, 60), Observation{Known: true, Minutes: 0, Required: 60}, t0, Claimed, NoReason, FromPlatform},
		{"user mark kept while platform silent", userClaimed, Observation{Required: 60}, t0, Claimed, NoReason, FromUser},
		{"user mark overwritten by definite platform answer", userClaimed, Observation{Known: true, Minutes: 5, Required: 60}, t0, Accruing, NoReason, FromPlatform},
		{"accruing then platform silent -> not_enrolled", row(Accruing, NoReason, 10, 60), Observation{Required: 60}, t0, Blocked, NotEnrolled, FromPlatform},
		{"not_enrolled held inside window", notEnrolled, Observation{Required: 60}, t0, Blocked, NotEnrolled, FromPlatform},
		{"not_enrolled re-derived after window", notEnrolled, Observation{Required: 60}, t0.Add(2 * time.Hour), Accruing, NoReason, FromPlatform},
		{"unmineable placeholder -> not_enrolled", base(), Observation{Known: true, Unmineable: true}, t0, Blocked, NotEnrolled, FromPlatform},
		{"user skip survives progress", skipped, Observation{Known: true, Minutes: 30, Required: 60}, t0, Blocked, UserSkip, FromUser},
		{"claimed beats user skip", skipped, Observation{Known: true, Claimed: true, Minutes: 60, Required: 60}, t0, Claimed, NoReason, FromPlatform},
		{"needs_link held inside window", needsLink, Observation{Known: true, Minutes: 60, Required: 60}, t0, Blocked, NeedsLink, FromPlatform},
		{"needs_link re-derived after window", needsLink, Observation{Known: true, Minutes: 60, Required: 60}, t0.Add(25 * time.Hour), Claimable, NoReason, FromPlatform},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(tc.prev, tc.obs, tc.at)
			assert.Equal(t, tc.want, got.Status)
			assert.Equal(t, tc.reason, got.Reason)
			assert.Equal(t, tc.src, got.Source)
			assert.Equal(t, "d", got.DropID, "identity preserved")
		})
	}
}

func TestApply_PlatformClaimedKeepsHighestMinutes(t *testing.T) {
	got := Apply(row(Claimed, NoReason, 60, 60), Observation{Known: true, Minutes: 0, Required: 60}, t0)
	assert.Equal(t, 60, got.Minutes)
}

func TestApply_KeepsClaimBackoff(t *testing.T) {
	r := ClaimFailedAttempt(row(Claimable, NoReason, 60, 60), t0)
	got := Apply(r, Observation{Known: true, Minutes: 60, Required: 60}, t0.Add(10*time.Second))
	assert.Equal(t, Claimable, got.Status)
	assert.Equal(t, r.RetryAfter, got.RetryAfter, "sync must not reset claim backoff")
	assert.Equal(t, 1, got.FailCount)
}

func TestClaimFailedAttempt_Ladder(t *testing.T) {
	r := row(Claimable, NoReason, 60, 60)
	for _, wait := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		r = ClaimFailedAttempt(r, t0)
		require.Equal(t, Claimable, r.Status)
		require.Equal(t, t0.Add(wait), r.RetryAfter)
		require.False(t, ReadyToClaim(r, t0))
		require.True(t, ReadyToClaim(r, t0.Add(wait)))
	}
	r = ClaimFailedAttempt(r, t0)
	assert.Equal(t, Blocked, r.Status)
	assert.Equal(t, ClaimFailed, r.Reason)
	assert.Equal(t, t0.Add(RetryClaimFailed), r.RetryAfter)
}

func TestClaimOutcomes(t *testing.T) {
	ok := ClaimOK(row(Claimable, NoReason, 60, 60), t0)
	assert.Equal(t, Claimed, ok.Status)
	assert.Equal(t, FromPlatform, ok.Source)
	link := ClaimNeedsLink(row(Claimable, NoReason, 60, 60), t0)
	assert.Equal(t, Blocked, link.Status)
	assert.Equal(t, NeedsLink, link.Reason)
	assert.Equal(t, t0.Add(RetryNeedsLink), link.RetryAfter)
}

func TestUserActions(t *testing.T) {
	m := MarkCollected(row(Accruing, NoReason, 10, 60), t0)
	assert.Equal(t, Claimed, m.Status)
	assert.Equal(t, FromUser, m.Source)

	s := Skip(row(Accruing, NoReason, 10, 60), t0)
	assert.Equal(t, Blocked, s.Status)
	assert.Equal(t, UserSkip, s.Reason)
	assert.True(t, s.RetryAfter.IsZero(), "user_skip never auto-expires")

	failed := row(Blocked, ClaimFailed, 60, 60)
	failed.FailCount = 5
	r := Retry(failed, t0)
	assert.Equal(t, Claimable, r.Status)
	assert.Equal(t, 0, r.FailCount)
}

func TestExpireAndBlockLink_LeaveClaimedAlone(t *testing.T) {
	c := row(Claimed, NoReason, 60, 60)
	assert.Equal(t, Claimed, Expire(c, t0).Status)
	assert.Equal(t, Claimed, BlockLink(c, t0).Status)
	assert.Equal(t, Expired, Expire(row(Accruing, NoReason, 10, 60), t0).Reason)
	assert.Equal(t, NeedsLink, BlockLink(row(Eligible, NoReason, 0, 60), t0).Reason)
	sk := row(Blocked, UserSkip, 0, 60)
	assert.Equal(t, UserSkip, BlockLink(sk, t0).Reason)
}

func TestMineable(t *testing.T) {
	assert.True(t, Mineable(row(Eligible, NoReason, 0, 60)))
	assert.True(t, Mineable(row(Accruing, NoReason, 1, 60)))
	assert.False(t, Mineable(row(Claimable, NoReason, 60, 60)))
	assert.False(t, Mineable(row(Blocked, NotEnrolled, 0, 60)))
	assert.False(t, Mineable(Row{}))
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/pipeline/dropstate/ -v`
Expected: FAIL, `undefined: Row` (package has no source yet).

- [ ] **Step 3: Implement**

```go
// Package dropstate is the per-(account, drop) state model for pipeline v2.
// Every transition is a pure function of (previous row, input, now) so the
// rules are table-testable and the account loop is the only writer.
package dropstate

import "time"

type Status string

const (
	Eligible  Status = "eligible"
	Accruing  Status = "accruing"
	Claimable Status = "claimable"
	Claimed   Status = "claimed"
	Blocked   Status = "blocked"
)

type Reason string

const (
	NoReason    Reason = ""
	NeedsLink   Reason = "needs_link"
	ClaimFailed Reason = "claim_failed"
	SubOnly     Reason = "sub_only"
	NoChannels  Reason = "no_channels"
	Expired     Reason = "expired"
	NotEnrolled Reason = "not_enrolled"
	UserSkip    Reason = "user_skip"
)

type Source string

const (
	FromPlatform Source = "platform"
	FromUser     Source = "user"
)

// Retry windows for blocked rows. user_skip never auto-expires.
const (
	RetryNeedsLink   = 24 * time.Hour
	RetryClaimFailed = 6 * time.Hour
	RetryNotEnrolled = time.Hour
	RetrySubOnly     = 24 * time.Hour
	RetryExpired     = 24 * time.Hour
	MaxClaimFailures = 5
)

var claimBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

type Row struct {
	AccountID  string
	DropID     string
	CampaignID string
	Platform   string
	Status     Status
	Reason     Reason
	Minutes    int
	Required   int
	Source     Source
	FailCount  int
	RetryAfter time.Time
	SyncedAt   time.Time
	UpdatedAt  time.Time
}

// IsZero reports a row that has never been stored or derived.
func (r Row) IsZero() bool { return r.Status == "" }

// Observation is what the platform said about one drop in one sync.
type Observation struct {
	// Known is true when the platform returned a definite record for the
	// drop. False means it said nothing, which proves nothing.
	Known bool
	// Unmineable marks drops the platform cannot track (scrape placeholders).
	Unmineable bool
	Claimed    bool
	Minutes    int
	Required   int
}

func derive(minutes, required int) (Status, Reason) {
	switch {
	case required <= 0:
		return Blocked, SubOnly
	case minutes >= required:
		return Claimable, NoReason
	case minutes > 0:
		return Accruing, NoReason
	default:
		return Eligible, NoReason
	}
}

func retryFor(reason Reason, now time.Time) time.Time {
	switch reason {
	case NeedsLink:
		return now.Add(RetryNeedsLink)
	case ClaimFailed:
		return now.Add(RetryClaimFailed)
	case NotEnrolled, NoChannels:
		return now.Add(RetryNotEnrolled)
	case SubOnly:
		return now.Add(RetrySubOnly)
	case Expired:
		return now.Add(RetryExpired)
	}
	return time.Time{}
}

func (r Row) with(s Status, reason Reason, now time.Time) Row {
	r.Status, r.Reason, r.UpdatedAt = s, reason, now
	r.RetryAfter = retryFor(reason, now)
	return r
}

// Apply folds one platform observation into the previous row. The caller
// seeds identity fields (AccountID, DropID, CampaignID, Platform) on a new row.
func Apply(prev Row, obs Observation, now time.Time) Row {
	r := prev
	if obs.Known && !obs.Unmineable {
		r.Minutes, r.Required, r.SyncedAt = obs.Minutes, obs.Required, now
	}
	// A platform-confirmed claim is final.
	if prev.Status == Claimed && prev.Source == FromPlatform {
		if r.Minutes < prev.Minutes {
			r.Minutes = prev.Minutes
		}
		return r
	}
	if !obs.Known {
		switch {
		case prev.IsZero():
			r.Required = obs.Required
			r.Source = FromPlatform
			st, rs := derive(0, obs.Required)
			return r.with(st, rs, now)
		case (prev.Status == Accruing || prev.Status == Claimable) && prev.Source == FromPlatform:
			return r.with(Blocked, NotEnrolled, now)
		case prev.Status == Blocked && prev.Reason == NotEnrolled && !now.Before(prev.RetryAfter):
			st, rs := derive(prev.Minutes, prev.Required)
			return r.with(st, rs, now)
		}
		return r
	}
	if obs.Unmineable {
		if prev.Status == Claimed {
			return r
		}
		r.Source = FromPlatform
		return r.with(Blocked, NotEnrolled, now)
	}
	if obs.Claimed {
		r.Source, r.FailCount = FromPlatform, 0
		return r.with(Claimed, NoReason, now)
	}
	if prev.Status == Blocked && prev.Reason == UserSkip {
		return r
	}
	if prev.Status == Blocked && (prev.Reason == NeedsLink || prev.Reason == ClaimFailed) && now.Before(prev.RetryAfter) {
		return r
	}
	st, rs := derive(r.Minutes, r.Required)
	if st == Claimable && prev.Status == Claimable && now.Before(prev.RetryAfter) {
		return r // keep claim backoff
	}
	if prev.Status == Blocked {
		r.FailCount = 0
	}
	r.Source = FromPlatform
	return r.with(st, rs, now)
}

// ClaimOK records a successful or already-claimed claim.
func ClaimOK(prev Row, now time.Time) Row {
	r := prev.with(Claimed, NoReason, now)
	r.Source, r.FailCount = FromPlatform, 0
	return r
}

// ClaimNeedsLink blocks a drop whose claim needs an external account link.
func ClaimNeedsLink(prev Row, now time.Time) Row {
	r := prev.with(Blocked, NeedsLink, now)
	r.Source = FromPlatform
	return r
}

// ClaimFailedAttempt counts a failed claim: backoff 1m, 5m, 30m, then
// blocked:claim_failed after MaxClaimFailures.
func ClaimFailedAttempt(prev Row, now time.Time) Row {
	r := prev
	r.FailCount++
	r.UpdatedAt = now
	if r.FailCount >= MaxClaimFailures {
		return r.with(Blocked, ClaimFailed, now)
	}
	i := r.FailCount - 1
	if i >= len(claimBackoff) {
		i = len(claimBackoff) - 1
	}
	r.RetryAfter = now.Add(claimBackoff[i])
	return r
}

// MarkCollected is the user asserting the drop is claimed.
func MarkCollected(prev Row, now time.Time) Row {
	r := prev.with(Claimed, NoReason, now)
	r.Source = FromUser
	return r
}

// Skip is the user excluding a drop from mining until they retry it.
func Skip(prev Row, now time.Time) Row {
	r := prev.with(Blocked, UserSkip, now)
	r.Source = FromUser
	return r
}

// Retry clears any block and re-derives from the last known minutes.
func Retry(prev Row, now time.Time) Row {
	st, rs := derive(prev.Minutes, prev.Required)
	r := prev.with(st, rs, now)
	r.FailCount, r.Source = 0, FromPlatform
	return r
}

// Expire blocks an unclaimed drop whose campaign has ended.
func Expire(prev Row, now time.Time) Row {
	if prev.Status == Claimed {
		return prev
	}
	return prev.with(Blocked, Expired, now)
}

// BlockLink blocks a drop whose campaign needs an unlinked external account.
func BlockLink(prev Row, now time.Time) Row {
	if prev.Status == Claimed || (prev.Status == Blocked && prev.Reason == UserSkip) {
		return prev
	}
	return prev.with(Blocked, NeedsLink, now)
}

// Mineable reports whether watching can still advance the drop.
func Mineable(r Row) bool { return r.Status == Eligible || r.Status == Accruing }

// ReadyToClaim reports a claimable row whose backoff has elapsed.
func ReadyToClaim(r Row, now time.Time) bool {
	return r.Status == Claimable && !now.Before(r.RetryAfter)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/pipeline/dropstate/ -v`
Expected: PASS, all cases.

- [ ] **Step 5: Revert-proof check**

Temporarily change the `prev.Status == Claimed && prev.Source == FromPlatform` early return to `false &&`. Run tests: `platform claimed never demoted` must FAIL. Restore.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/pipeline
git add internal/pipeline/dropstate
git commit -m "feat(pipeline-v2): per-drop state model with pure transitions

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `drop_state` + streamer priority storage

**Files:**
- Create: `internal/store/migrations/0016_pipeline_v2.sql`
- Create: `internal/store/queries/drop_state.sql`
- Create: `internal/store/queries/streamer_priority.sql`
- Regenerate: `internal/store/gen/*` (sqlc)
- Create: `internal/store/dropstate_store.go`
- Test: `internal/store/dropstate_store_test.go`

**Interfaces:**
- Consumes: `dropstate.Row` and constants (Task 2).
- Produces:
  - `type DropStateStore struct{ Q *gen.Queries }`, `func NewDropStateStore(q *gen.Queries) *DropStateStore`
  - `func (s *DropStateStore) List(ctx context.Context, accountID string) ([]dropstate.Row, error)`
  - `func (s *DropStateStore) Upsert(ctx context.Context, r dropstate.Row) error`
  - `func (s *DropStateStore) StreamerPriority(ctx context.Context, accountID string) ([]string, error)`
  - gen: `ListDropStates`, `UpsertDropState`, `ListStreamerPriority`, models `gen.DropState`, `gen.UpsertDropStateParams`

- [ ] **Step 1: Migration**

```sql
-- +goose Up
-- +goose StatementBegin
-- Pipeline v2: one authoritative mining state row per account and drop.
-- Timestamps are unix seconds, 0 means unset.
CREATE TABLE drop_state (
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    drop_id       TEXT NOT NULL,
    campaign_id   TEXT NOT NULL,
    platform      TEXT NOT NULL,
    status        TEXT NOT NULL,
    block_reason  TEXT NOT NULL DEFAULT '',
    minutes       INTEGER NOT NULL DEFAULT 0,
    required      INTEGER NOT NULL DEFAULT 0,
    source        TEXT NOT NULL DEFAULT 'platform',
    fail_count    INTEGER NOT NULL DEFAULT 0,
    retry_after   INTEGER NOT NULL DEFAULT 0,
    synced_at     INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (account_id, drop_id)
);
CREATE INDEX idx_drop_state_status ON drop_state(account_id, status);

-- Ordered per-account priority streamers. Seeded from account_channels,
-- the null-game channel whitelist, which is the same concept.
CREATE TABLE account_streamer_priority (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform    TEXT NOT NULL,
    login       TEXT NOT NULL,
    rank        INTEGER NOT NULL,
    PRIMARY KEY (account_id, login)
);
CREATE INDEX idx_account_streamer_priority_acct ON account_streamer_priority(account_id, rank);

INSERT INTO account_streamer_priority (account_id, platform, login, rank)
SELECT ac.account_id, a.platform, ac.channel, ac.rank
FROM account_channels ac
JOIN accounts a ON a.id = ac.account_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_streamer_priority_acct;
DROP TABLE IF EXISTS account_streamer_priority;
DROP INDEX IF EXISTS idx_drop_state_status;
DROP TABLE IF EXISTS drop_state;
-- +goose StatementEnd
```

- [ ] **Step 2: Queries**

`internal/store/queries/drop_state.sql`:

```sql
-- name: ListDropStates :many
SELECT * FROM drop_state WHERE account_id = ? ORDER BY campaign_id, drop_id;

-- name: UpsertDropState :exec
INSERT INTO drop_state (
    account_id, drop_id, campaign_id, platform, status, block_reason,
    minutes, required, source, fail_count, retry_after, synced_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, drop_id) DO UPDATE SET
    campaign_id = excluded.campaign_id,
    platform = excluded.platform,
    status = excluded.status,
    block_reason = excluded.block_reason,
    minutes = excluded.minutes,
    required = excluded.required,
    source = excluded.source,
    fail_count = excluded.fail_count,
    retry_after = excluded.retry_after,
    synced_at = excluded.synced_at,
    updated_at = excluded.updated_at;
```

`internal/store/queries/streamer_priority.sql`:

```sql
-- name: ListStreamerPriority :many
SELECT login, rank FROM account_streamer_priority
WHERE account_id = ?
ORDER BY rank ASC, login ASC;
```

- [ ] **Step 3: Generate**

Run: `cd internal/store && sqlc generate && cd ../.. && go build ./...`
Expected: new files `internal/store/gen/drop_state.sql.go`, `streamer_priority.sql.go`; `models.go` gains `DropState` and `AccountStreamerPriority`; build OK. Open `gen/drop_state.sql.go` and confirm `UpsertDropStateParams` field names match Step 5 below (`AccountID, DropID, CampaignID, Platform, Status, BlockReason, Minutes, Required, Source, FailCount, RetryAfter, SyncedAt, UpdatedAt`, integers as `int64`). If sqlc named anything differently, use its names in Step 5.

- [ ] **Step 4: Failing store test**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func seedAccount(t *testing.T, q *gen.Queries, id, platform string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
		ID: id, Platform: platform, DisplayName: id,
		Status: "idle", FingerprintJson: "{}", Enabled: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

func TestDropStateStore_RoundTrip(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "twitch")
	s := NewDropStateStore(q)

	at := time.Unix(1_700_000_000, 0)
	in := dropstate.Row{
		AccountID: "acc-1", DropID: "d1", CampaignID: "c1", Platform: "twitch",
		Status: dropstate.Blocked, Reason: dropstate.ClaimFailed,
		Minutes: 60, Required: 60, Source: dropstate.FromPlatform, FailCount: 5,
		RetryAfter: at.Add(time.Hour), SyncedAt: at, UpdatedAt: at,
	}
	require.NoError(t, s.Upsert(ctx, in))
	got, err := s.List(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, in, got[0])

	in.Status, in.Reason, in.RetryAfter = dropstate.Claimed, dropstate.NoReason, time.Time{}
	require.NoError(t, s.Upsert(ctx, in))
	got, err = s.List(ctx, "acc-1")
	require.NoError(t, err)
	require.Len(t, got, 1, "upsert must not duplicate")
	assert.Equal(t, dropstate.Claimed, got[0].Status)
	assert.True(t, got[0].RetryAfter.IsZero(), "0 maps back to zero time")
}

func TestDropStateStore_StreamerPrioritySeededFromAccountChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "kick")
	s := NewDropStateStore(q)

	// Migration already ran; insert straight into the new table like the
	// seed does, then read ordered by rank.
	_, err := db.ExecContext(ctx, `INSERT INTO account_streamer_priority (account_id, platform, login, rank) VALUES
		('acc-1','kick','zeta',1), ('acc-1','kick','alpha',0)`)
	require.NoError(t, err)
	got, err := s.StreamerPriority(ctx, "acc-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, got)
}
```

Run: `go test ./internal/store/ -run DropStateStore -v`
Expected: FAIL, `undefined: NewDropStateStore`.

- [ ] **Step 5: Implement store**

```go
package store

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// DropStateStore persists pipeline v2 drop_state rows and reads the
// per-account streamer priority list.
type DropStateStore struct {
	Q *gen.Queries
}

func NewDropStateStore(q *gen.Queries) *DropStateStore { return &DropStateStore{Q: q} }

func dsUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func dsTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func (s *DropStateStore) List(ctx context.Context, accountID string) ([]dropstate.Row, error) {
	rows, err := s.Q.ListDropStates(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]dropstate.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, dropstate.Row{
			AccountID:  r.AccountID,
			DropID:     r.DropID,
			CampaignID: r.CampaignID,
			Platform:   r.Platform,
			Status:     dropstate.Status(r.Status),
			Reason:     dropstate.Reason(r.BlockReason),
			Minutes:    int(r.Minutes),
			Required:   int(r.Required),
			Source:     dropstate.Source(r.Source),
			FailCount:  int(r.FailCount),
			RetryAfter: dsTime(r.RetryAfter),
			SyncedAt:   dsTime(r.SyncedAt),
			UpdatedAt:  dsTime(r.UpdatedAt),
		})
	}
	return out, nil
}

func (s *DropStateStore) Upsert(ctx context.Context, r dropstate.Row) error {
	return s.Q.UpsertDropState(ctx, gen.UpsertDropStateParams{
		AccountID:   r.AccountID,
		DropID:      r.DropID,
		CampaignID:  r.CampaignID,
		Platform:    r.Platform,
		Status:      string(r.Status),
		BlockReason: string(r.Reason),
		Minutes:     int64(r.Minutes),
		Required:    int64(r.Required),
		Source:      string(r.Source),
		FailCount:   int64(r.FailCount),
		RetryAfter:  dsUnix(r.RetryAfter),
		SyncedAt:    dsUnix(r.SyncedAt),
		UpdatedAt:   dsUnix(r.UpdatedAt),
	})
}

func (s *DropStateStore) StreamerPriority(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.Q.ListStreamerPriority(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Login)
	}
	return out, nil
}
```

- [ ] **Step 6: Migration seed test**

Append to `dropstate_store_test.go`. It proves the `INSERT ... SELECT` seed ran by re-running goose Down/Up is overkill; instead assert on an account_channels row inserted BEFORE the migration is impossible here, so test the seed SQL directly:

```go
func TestMigration0016_SeedCopiesAccountChannels(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "kick")
	require.NoError(t, q.AddAccountChannel(ctx, gen.AddAccountChannelParams{AccountID: "acc-1", Channel: "oilrats", Rank: 0}))
	// Re-run the seed statement from 0016 exactly.
	_, err := db.ExecContext(ctx, `INSERT INTO account_streamer_priority (account_id, platform, login, rank)
		SELECT ac.account_id, a.platform, ac.channel, ac.rank FROM account_channels ac JOIN accounts a ON a.id = ac.account_id`)
	require.NoError(t, err)
	got, err := NewDropStateStore(q).StreamerPriority(ctx, "acc-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"oilrats"}, got)
}
```

- [ ] **Step 7: Run all store tests**

Run: `go test ./internal/store/ -v 2>&1 | tail -20`
Expected: PASS (existing tests unaffected by the new migration).

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/store
git add internal/store
git commit -m "feat(pipeline-v2): drop_state and streamer priority tables

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: One-shot backfill from v1 state

**Files:**
- Modify: `internal/store/queries/drop_state.sql` (append two queries)
- Regenerate: `internal/store/gen/*`
- Create: `internal/store/dropstate_backfill.go`
- Test: `internal/store/dropstate_backfill_test.go`

**Interfaces:**
- Consumes: `DropStateStore` (Task 3), `SkipOverridePrefix`, `CollectOverridePrefix` (existing), gen `ListKVByPrefix(ctx, sql.NullString) ([]gen.Kv, error)`, `GetSettingString(ctx, key) ([]byte, error)`, `UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key string; Value []byte})`.
- Produces: `func BackfillDropState(ctx context.Context, q *gen.Queries, now time.Time) (int, error)`, const `DropStateBackfilledKey = "drop_state_backfilled"`.

- [ ] **Step 1: Queries** (append to `drop_state.sql`)

```sql
-- name: ListClaimsForBackfill :many
SELECT c.account_id, c.benefit_id, b.campaign_id, a.platform
FROM claims c
JOIN benefits b ON b.id = c.benefit_id
JOIN accounts a ON a.id = c.account_id;

-- name: GetBenefitCampaign :one
SELECT b.campaign_id, cp.platform
FROM benefits b
JOIN campaigns cp ON cp.id = b.campaign_id
WHERE b.id = ?;
```

Run: `cd internal/store && sqlc generate && cd ../.. && go build ./...`
Expected: `ListClaimsForBackfillRow{AccountID, BenefitID, CampaignID, Platform}` and `GetBenefitCampaignRow{CampaignID, Platform}` generated.

- [ ] **Step 2: Failing test**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func TestBackfillDropState(t *testing.T) {
	db := openTest(t)
	q := gen.New(db)
	ctx := context.Background()
	seedAccount(t, q, "acc-1", "twitch")
	require.NoError(t, NewCampaignPersister(q).PersistCampaigns(ctx, []platform.Campaign{{
		ID: "c1", Platform: "twitch", Game: "G", Name: "C", Status: "active",
		Benefits: []platform.DropBenefit{
			{ID: "claimed1", CampaignID: "c1", Name: "A", RequiredMinutes: 60},
			{ID: "marked1", CampaignID: "c1", Name: "B", RequiredMinutes: 60},
			{ID: "skipped1", CampaignID: "c1", Name: "C", RequiredMinutes: 60},
			{ID: "both1", CampaignID: "c1", Name: "D", RequiredMinutes: 60},
		},
	}}))
	rec := NewClaimRecorder(q)
	require.NoError(t, rec.RecordClaim(ctx, "acc-1", platform.DropBenefit{ID: "claimed1"}))
	require.NoError(t, rec.RecordClaim(ctx, "acc-1", platform.DropBenefit{ID: "both1"}))
	for _, k := range []string{
		CollectOverridePrefix + "marked1:acc-1",
		SkipOverridePrefix + "skipped1:acc-1",
		SkipOverridePrefix + "both1:acc-1", // claim history must win over a skip
		SkipOverridePrefix + "gone:acc-1",  // benefit row missing: ignored
	} {
		require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: k, Value: []byte("1")}))
	}

	now := time.Unix(1_700_000_000, 0)
	n, err := BackfillDropState(ctx, q, now)
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	rows, err := NewDropStateStore(q).List(ctx, "acc-1")
	require.NoError(t, err)
	by := map[string]dropstate.Row{}
	for _, r := range rows {
		by[r.DropID] = r
	}
	assert.Equal(t, dropstate.Claimed, by["claimed1"].Status)
	assert.Equal(t, dropstate.FromPlatform, by["claimed1"].Source)
	assert.Equal(t, dropstate.Claimed, by["marked1"].Status)
	assert.Equal(t, dropstate.FromUser, by["marked1"].Source)
	assert.Equal(t, dropstate.Blocked, by["skipped1"].Status)
	assert.Equal(t, dropstate.NotEnrolled, by["skipped1"].Reason)
	assert.Equal(t, now, by["skipped1"].RetryAfter, "skips get one fresh evaluation")
	assert.Equal(t, dropstate.Claimed, by["both1"].Status)
	assert.Equal(t, "c1", by["claimed1"].CampaignID)

	// Idempotent: second run is a no-op.
	n, err = BackfillDropState(ctx, q, now)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
```

Run: `go test ./internal/store/ -run TestBackfillDropState -v`
Expected: FAIL, `undefined: BackfillDropState`.

- [ ] **Step 3: Implement**

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// DropStateBackfilledKey marks the one-shot v1 to v2 state backfill as done.
const DropStateBackfilledKey = "drop_state_backfilled"

// BackfillDropState seeds drop_state from v1 state: ghost-skips become
// not_enrolled with an immediate retry, manual marks become user claims, and
// claim history becomes platform claims. Later sources win. Runs once; the v1
// kv keys are left in place so rolling back to v1 is safe.
func BackfillDropState(ctx context.Context, q *gen.Queries, now time.Time) (int, error) {
	if v, err := q.GetSettingString(ctx, DropStateBackfilledKey); err == nil && string(v) == "1" {
		return 0, nil
	}
	rows := map[string]dropstate.Row{}
	put := func(r dropstate.Row) { rows[r.AccountID+"|"+r.DropID] = r }

	fromKV := func(prefix string, mk func(acct, drop, campaign, plat string) dropstate.Row) error {
		kvs, err := q.ListKVByPrefix(ctx, sql.NullString{String: prefix, Valid: true})
		if err != nil {
			return fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, kv := range kvs {
			drop, acct, ok := splitOverrideKey(kv.Key, prefix)
			if !ok {
				continue
			}
			bc, err := q.GetBenefitCampaign(ctx, drop)
			if err != nil {
				continue // benefit row gone, nothing to seed
			}
			put(mk(acct, drop, bc.CampaignID, bc.Platform))
		}
		return nil
	}
	if err := fromKV(SkipOverridePrefix, func(acct, drop, campaign, plat string) dropstate.Row {
		return dropstate.Row{AccountID: acct, DropID: drop, CampaignID: campaign, Platform: plat,
			Status: dropstate.Blocked, Reason: dropstate.NotEnrolled, Source: dropstate.FromPlatform,
			RetryAfter: now, UpdatedAt: now}
	}); err != nil {
		return 0, err
	}
	if err := fromKV(CollectOverridePrefix, func(acct, drop, campaign, plat string) dropstate.Row {
		return dropstate.Row{AccountID: acct, DropID: drop, CampaignID: campaign, Platform: plat,
			Status: dropstate.Claimed, Source: dropstate.FromUser, UpdatedAt: now}
	}); err != nil {
		return 0, err
	}
	claims, err := q.ListClaimsForBackfill(ctx)
	if err != nil {
		return 0, fmt.Errorf("list claims: %w", err)
	}
	for _, c := range claims {
		put(dropstate.Row{AccountID: c.AccountID, DropID: c.BenefitID, CampaignID: c.CampaignID, Platform: c.Platform,
			Status: dropstate.Claimed, Source: dropstate.FromPlatform, UpdatedAt: now})
	}

	st := NewDropStateStore(q)
	for _, r := range rows {
		if err := st.Upsert(ctx, r); err != nil {
			return 0, fmt.Errorf("upsert %s/%s: %w", r.AccountID, r.DropID, err)
		}
	}
	if err := q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: DropStateBackfilledKey, Value: []byte("1")}); err != nil {
		return 0, fmt.Errorf("mark backfilled: %w", err)
	}
	return len(rows), nil
}

// splitOverrideKey parses prefix + dropID + ":" + accountID.
func splitOverrideKey(key, prefix string) (drop, acct string, ok bool) {
	rest := strings.TrimPrefix(key, prefix)
	i := strings.LastIndex(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -v 2>&1 | tail -20`
Expected: PASS. If `gen.Kv.Key` is not a `string`, adapt the field access.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/store
git add internal/store
git commit -m "feat(pipeline-v2): backfill drop_state from claims and v1 overrides

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Platform capability types + Twitch adapter

**Files:**
- Create: `internal/platform/pipeline.go`
- Modify: `internal/platform/twitch/campaigns.go` (add `Self` to details struct)
- Modify: `internal/platform/twitch/claim.go` (split out `claimStatus`, behaviour unchanged)
- Create: `internal/platform/twitch/pipeline.go`
- Test: `internal/platform/twitch/pipeline_test.go`

**Interfaces:**
- Consumes: twitch `discovery`, `client.gql`, `OpDropCampaignDetails`, `(*discovery).inventory`, `(*channels).listEligible`, `(*watch).resolveUserID`, `claimer`.
- Produces (in `internal/platform`):

```go
type DropProgress struct {
	DropID, CampaignID string
	Minutes, Required  int
	Claimed            bool
	InstanceID         string
	Known              bool
	Unmineable         bool
}
type ClaimOutcome int // ClaimOK, ClaimAlready, ClaimNeedsLink, ClaimFailed; String()
type ClaimResult struct { Outcome ClaimOutcome; LinkURL, Detail string }
type DropProgressSource interface { DropProgress(ctx context.Context, s Session, camps []Campaign) ([]DropProgress, error) }
type DropClaimer interface { ClaimDrop(ctx context.Context, s Session, d DropProgress) ClaimResult }
type ChannelProber interface { ProbeChannels(ctx context.Context, s Session, c Campaign, logins []string) ([]Stream, error) }
```
- Produces (twitch): `(*Backend).DropProgress`, `(*Backend).ClaimDrop`, `(*Backend).ProbeChannels`, `classifyClaimStatus(string) platform.ClaimResult`, `isSyntheticID(string) bool`.

- [ ] **Step 1: Platform types** (`internal/platform/pipeline.go`)

```go
package platform

import "context"

// DropProgress is one drop's per-account state as the platform reports it.
// Pipeline v2's reconciler folds these into drop_state.
type DropProgress struct {
	DropID     string
	CampaignID string
	Minutes    int
	Required   int
	Claimed    bool
	InstanceID string
	// Known is true when the platform returned a definite record for this
	// drop. False means the platform said nothing about it (Kick lists a
	// reward only after it accrues), which is not evidence of anything.
	Known bool
	// Unmineable marks drops whose progress the platform cannot track,
	// such as Twitch scrape placeholders. The reconciler blocks them.
	Unmineable bool
}

type ClaimOutcome int

const (
	ClaimOK ClaimOutcome = iota
	ClaimAlready
	ClaimNeedsLink
	ClaimFailed
)

func (o ClaimOutcome) String() string {
	switch o {
	case ClaimOK:
		return "ok"
	case ClaimAlready:
		return "already_claimed"
	case ClaimNeedsLink:
		return "needs_link"
	default:
		return "failed"
	}
}

// ClaimResult is a typed claim outcome, replacing string-matched errors.
type ClaimResult struct {
	Outcome ClaimOutcome
	LinkURL string
	Detail  string
}

// DropProgressSource reports authoritative per-drop progress for the given
// campaigns. It returns an error rather than a partial result, so a failed
// sync never looks like "the platform forgot these drops".
type DropProgressSource interface {
	DropProgress(ctx context.Context, s Session, camps []Campaign) ([]DropProgress, error)
}

// DropClaimer claims one drop and classifies the outcome.
type DropClaimer interface {
	ClaimDrop(ctx context.Context, s Session, d DropProgress) ClaimResult
}

// ChannelProber checks specific channels for liveness and, when c.Game is
// set, that they stream the campaign's game. Used for priority streamers
// that platform directories may not list.
type ChannelProber interface {
	ProbeChannels(ctx context.Context, s Session, c Campaign, logins []string) ([]Stream, error)
}
```

- [ ] **Step 2: Add `Self` to the details struct** (`campaigns.go`, inside `TimeBasedDrops []struct { ... }`, after `PreconditionDrops`)

```go
				// self is the viewer's own progress on this drop. Null when
				// the account is not enrolled in the campaign yet.
				Self *struct {
					CurrentMinutesWatched int    `json:"currentMinutesWatched"`
					IsClaimed             bool   `json:"isClaimed"`
					DropInstanceID        string `json:"dropInstanceID"`
				} `json:"self"`
```

- [ ] **Step 3: Split `claimStatus` out of `claim`** (`claim.go`, replace the `claim` function)

```go
func (cl *claimer) claim(ctx context.Context, sess platform.Session, b platform.DropBenefit, userID int64) error {
	status, err := cl.claimStatus(ctx, sess, b, userID)
	if err != nil {
		return err
	}
	switch status {
	case "ELIGIBLE_FOR_ALL", "DROP_INSTANCE_ALREADY_CLAIMED", "":
		return nil
	default:
		return fmt.Errorf("claim status: %s", status)
	}
}

// claimStatus sends the claim mutation and returns Twitch's raw status.
func (cl *claimer) claimStatus(ctx context.Context, sess platform.Session, b platform.DropBenefit, userID int64) (string, error) {
	// Prefer the per-account instance id captured at progress time.
	// When it's missing, construct DevilXD's synthetic instance id
	// `userID#campaignID#dropID` (inventory.py generate_claim) — Twitch
	// accepts it and rejects the bare drop-template id with
	// INVALID_DROP_INSTANCE. Only fall back to the template id as a last
	// resort when we couldn't resolve the user id.
	id := b.InstanceID
	if id == "" && userID > 0 && b.CampaignID != "" && b.ID != "" {
		id = fmt.Sprintf("%d#%s#%s", userID, b.CampaignID, b.ID)
	}
	if id == "" {
		id = b.ID
	}
	var out claimResult
	if err := cl.c.gql(ctx, sess.AccessToken, OpClaimDrop,
		map[string]any{"input": map[string]any{"dropInstanceID": id}}, &out); err != nil {
		return "", fmt.Errorf("claim %s: %w", id, err)
	}
	return out.ClaimDropRewards.Status, nil
}
```

Run: `go test ./internal/platform/twitch/ -run Claim -v`
Expected: PASS (existing claim tests prove the refactor is behaviour-neutral).

- [ ] **Step 4: Failing adapter tests** (`pipeline_test.go`)

```go
package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// pipelineTestBackend serves DropCampaignDetails + Inventory from fixtures.
func pipelineTestBackend(t *testing.T, details map[string]string, inventory string, detailsStatus int) *Backend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.OperationName {
		case OpDropCampaignDetails.Name:
			if detailsStatus != 0 {
				w.WriteHeader(detailsStatus)
				return
			}
			id, _ := req.Variables["dropID"].(string)
			_, _ = w.Write([]byte(details[id]))
		case OpInventory.Name:
			_, _ = w.Write([]byte(inventory))
		default:
			t.Fatalf("unexpected op %q", req.OperationName)
		}
	}))
	t.Cleanup(srv.Close)
	return &Backend{disc: &discovery{c: newTestClient(srv.URL), userLogin: "testuser"}}
}

const emptyInventory = `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[]}}}}`

func TestDropProgress_ReadsSelfForCompletedCampaign(t *testing.T) {
	// Campaign left dropCampaignsInProgress (fully claimed), but details self
	// still reports isClaimed. This is the P1 fix.
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":{"currentMinutesWatched":60,"isClaimed":true,"dropInstanceID":"i1"}}
	]}}}}`}, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, platform.DropProgress{DropID: "d1", CampaignID: "c1", Minutes: 60, Required: 60, Claimed: true, InstanceID: "i1", Known: true}, got[0])
}

func TestDropProgress_NullSelfIsUnclaimed(t *testing.T) {
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":null}
	]}}}}`}, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Claimed)
	assert.Equal(t, 0, got[0].Minutes)
	assert.True(t, got[0].Known)
}

func TestDropProgress_DedupesMultiItemDrop(t *testing.T) {
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[{"benefit":{"id":"b1","name":"A"}},{"benefit":{"id":"b2","name":"B"}}],"self":null},
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":null}
	]}}}}`}, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestDropProgress_InventoryFillsInstanceAndMinutes(t *testing.T) {
	b := pipelineTestBackend(t, map[string]string{"c1": `{"data":{"user":{"dropCampaign":{"timeBasedDrops":[
		{"id":"d1","requiredMinutesWatched":60,"benefitEdges":[],"self":{"currentMinutesWatched":10,"isClaimed":false,"dropInstanceID":""}}
	]}}}}`}, `{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[
		{"id":"c1","timeBasedDrops":[{"id":"d1","self":{"currentMinutesWatched":25,"isClaimed":false,"dropInstanceID":"inst"}}]}
	]}}}}`, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 25, got[0].Minutes)
	assert.Equal(t, "inst", got[0].InstanceID)
}

func TestDropProgress_SyntheticCampaignIsUnmineable(t *testing.T) {
	b := pipelineTestBackend(t, nil, emptyInventory, 0)
	got, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "Game|Name", Platform: "twitch", Benefits: []platform.DropBenefit{{ID: "Game|Name_default"}}}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Unmineable)
	assert.True(t, got[0].Known)
}

func TestDropProgress_DetailsErrorFailsWhole(t *testing.T) {
	b := pipelineTestBackend(t, nil, emptyInventory, http.StatusInternalServerError)
	_, err := b.DropProgress(context.Background(), platform.Session{AccessToken: "t"},
		[]platform.Campaign{{ID: "c1", Platform: "twitch"}})
	require.Error(t, err)
}

func TestClassifyClaimStatus(t *testing.T) {
	assert.Equal(t, platform.ClaimOK, classifyClaimStatus("ELIGIBLE_FOR_ALL").Outcome)
	assert.Equal(t, platform.ClaimOK, classifyClaimStatus("").Outcome)
	assert.Equal(t, platform.ClaimAlready, classifyClaimStatus("DROP_INSTANCE_ALREADY_CLAIMED").Outcome)
	r := classifyClaimStatus("DROP_INSTANCE_ALREADY_EXPIRED")
	assert.Equal(t, platform.ClaimFailed, r.Outcome)
	assert.Contains(t, r.Detail, "DROP_INSTANCE_ALREADY_EXPIRED")
}

func TestBackend_SatisfiesPipelineInterfaces(t *testing.T) {
	var _ platform.DropProgressSource = (*Backend)(nil)
	var _ platform.DropClaimer = (*Backend)(nil)
	var _ platform.ChannelProber = (*Backend)(nil)
}
```

Run: `go test ./internal/platform/twitch/ -run 'DropProgress|ClassifyClaim|PipelineInterfaces' -v`
Expected: FAIL, `b.DropProgress undefined`.

- [ ] **Step 5: Implement** (`internal/platform/twitch/pipeline.go`)

```go
package twitch

import (
	"context"
	"fmt"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// isSyntheticID reports scrape-fallback placeholder ids ("Game|Name").
// Twitch can't report progress for them.
func isSyntheticID(id string) bool { return strings.ContainsAny(id, "| ") }

// dropProgress reads DropCampaignDetails uncached, because self changes as
// the viewer watches and claims. One entry per drop id.
func (d *discovery) dropProgress(ctx context.Context, sess platform.Session, campaignID string) ([]platform.DropProgress, error) {
	channelLogin := d.userLogin
	if channelLogin == "" {
		channelLogin = "twitch"
	}
	var det campaignDetailsData
	if err := d.c.gql(ctx, sess.AccessToken, OpDropCampaignDetails,
		map[string]any{"dropID": campaignID, "channelLogin": channelLogin}, &det); err != nil {
		return nil, fmt.Errorf("drop progress %s: %w", campaignID, err)
	}
	seen := map[string]bool{}
	var out []platform.DropProgress
	for _, td := range det.User.DropCampaign.TimeBasedDrops {
		if td.ID == "" || seen[td.ID] {
			continue
		}
		seen[td.ID] = true
		dp := platform.DropProgress{DropID: td.ID, CampaignID: campaignID, Required: td.RequiredMinutesWatched, Known: true}
		if td.Self != nil {
			dp.Minutes = td.Self.CurrentMinutesWatched
			dp.Claimed = td.Self.IsClaimed
			dp.InstanceID = td.Self.DropInstanceID
		}
		out = append(out, dp)
	}
	return out, nil
}

// DropProgress satisfies platform.DropProgressSource. Per-campaign details
// are the truth (they survive the campaign leaving the in-progress
// inventory); the in-progress inventory only fills fresher minutes and the
// instance id.
func (b *Backend) DropProgress(ctx context.Context, s platform.Session, camps []platform.Campaign) ([]platform.DropProgress, error) {
	inv, err := b.disc.inventory(ctx, s)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]platform.Progress, len(inv))
	for _, p := range inv {
		byID[p.BenefitID] = p
	}
	var out []platform.DropProgress
	for _, c := range camps {
		if isSyntheticID(c.ID) {
			for _, bf := range c.Benefits {
				out = append(out, platform.DropProgress{DropID: bf.ID, CampaignID: c.ID, Known: true, Unmineable: true})
			}
			continue
		}
		dps, err := b.disc.dropProgress(ctx, s, c.ID)
		if err != nil {
			return nil, err
		}
		for i := range dps {
			p, ok := byID[dps[i].DropID]
			if !ok {
				continue
			}
			if p.MinutesWatched > dps[i].Minutes {
				dps[i].Minutes = p.MinutesWatched
			}
			dps[i].Claimed = dps[i].Claimed || p.Claimed
			if dps[i].InstanceID == "" {
				dps[i].InstanceID = p.InstanceID
			}
		}
		out = append(out, dps...)
	}
	return out, nil
}

// classifyClaimStatus maps Twitch's claimDropRewards.status to an outcome.
func classifyClaimStatus(status string) platform.ClaimResult {
	switch status {
	case "ELIGIBLE_FOR_ALL", "":
		return platform.ClaimResult{Outcome: platform.ClaimOK}
	case "DROP_INSTANCE_ALREADY_CLAIMED":
		return platform.ClaimResult{Outcome: platform.ClaimAlready}
	default:
		return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: "claim status: " + status}
	}
}

// ClaimDrop satisfies platform.DropClaimer. Unlinked campaigns are blocked
// before claiming (reconciler reads self.isAccountConnected), so Twitch has
// no link outcome here.
func (b *Backend) ClaimDrop(ctx context.Context, s platform.Session, d platform.DropProgress) platform.ClaimResult {
	userID, _ := b.watch.resolveUserID(ctx, s)
	status, err := b.claim.claimStatus(ctx, s, platform.DropBenefit{ID: d.DropID, CampaignID: d.CampaignID, InstanceID: d.InstanceID}, userID)
	if err != nil {
		return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: err.Error()}
	}
	return classifyClaimStatus(status)
}

// ProbeChannels satisfies platform.ChannelProber: parallel live check of the
// given logins, filtered to c.Game when set.
func (b *Backend) ProbeChannels(ctx context.Context, s platform.Session, c platform.Campaign, logins []string) ([]platform.Stream, error) {
	return b.chans.listEligible(ctx, s, c, logins)
}

var (
	_ platform.DropProgressSource = (*Backend)(nil)
	_ platform.DropClaimer        = (*Backend)(nil)
	_ platform.ChannelProber      = (*Backend)(nil)
)
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/platform/... -v 2>&1 | tail -30`
Expected: PASS. If the 500 test does not error, check `client.gql` treats non-2xx as an error; if it doesn't, make the fixture return `{"errors":[{"message":"boom"}]}` instead, whichever `gql` rejects.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/platform
git add internal/platform
git commit -m "feat(pipeline-v2): platform progress/claim/probe capabilities, Twitch adapter

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Kick adapter

**Files:**
- Modify: `internal/platform/kick/api.go` (add `Required` to `progressReward`)
- Create: `internal/platform/kick/pipeline.go`
- Test: `internal/platform/kick/pipeline_test.go`

**Interfaces:**
- Consumes: `platform.DropProgress`, `ClaimResult`, capability interfaces (Task 5); kick `api.progressDetail`, `api.Claim`, `ClaimNeedsLinkError`, `probeLive`, `kickChannel`.
- Produces: `(*kick.Backend).DropProgress`, `ClaimDrop`, `ProbeChannels`.

- [ ] **Step 1: Add `Required` to `progressReward`**

In `api.go`, add field `Required int // required_units` to `progressReward`, and in `progressDetail`'s append add:

```go
				Required:   mnum(rm, "required_units", "required_minutes", "requiredMinutes", "minutes"),
```

- [ ] **Step 2: Failing tests** (`pipeline_test.go`)

```go
package kick

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

const progressURL = "https://web.kick.com/api/v1/drops/progress"

func TestKickDropProgress_RequiredFromPlatform(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{progressURL: {200, `{"data":[
		{"id":"c1","rewards":[{"id":"r1","claimed":false,"progress":0.5,"required_units":30}]}
	]}`}}}
	b := withFake(f)
	got, err := b.DropProgress(context.Background(), sess("acc1"), []platform.Campaign{{
		ID: "c1", Benefits: []platform.DropBenefit{{ID: "r1", CampaignID: "c1", RequiredMinutes: 120}},
	}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	// Platform says 30, adapter's invented 120 must not win (P6).
	assert.Equal(t, platform.DropProgress{DropID: "r1", CampaignID: "c1", Minutes: 15, Required: 30, Known: true}, got[0])
}

func TestKickDropProgress_AbsentRewardIsUnknown(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{progressURL: {200, `{"data":[]}`}}}
	b := withFake(f)
	got, err := b.DropProgress(context.Background(), sess("acc1"), []platform.Campaign{{
		ID: "c1", Benefits: []platform.DropBenefit{{ID: "r1", CampaignID: "c1", RequiredMinutes: 120}},
	}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Known, "absence proves nothing")
	assert.Equal(t, 120, got[0].Required)
}

func TestKickDropProgress_ErrorFailsWhole(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{progressURL: {403, `{}`}}}
	_, err := withFake(f).DropProgress(context.Background(), sess("acc1"), []platform.Campaign{{ID: "c1"}})
	require.Error(t, err)
}

func TestKickClaimDrop_Outcomes(t *testing.T) {
	claimURL := "https://web.kick.com/api/v1/drops/claim"
	ok := withFake(&fakeDoer{resp: map[string]fakeResp{claimURL: {200, `{}`}}})
	assert.Equal(t, platform.ClaimOK, ok.ClaimDrop(context.Background(), sess("a"), platform.DropProgress{DropID: "r1", CampaignID: "c1"}).Outcome)

	link := withFake(&fakeDoer{resp: map[string]fakeResp{claimURL: {400, `{"connect_url":"https://kick.com/connect/riot"}`}}})
	r := link.ClaimDrop(context.Background(), sess("a"), platform.DropProgress{DropID: "r1", CampaignID: "c1"})
	assert.Equal(t, platform.ClaimNeedsLink, r.Outcome)
	assert.Equal(t, "https://kick.com/connect/riot", r.LinkURL)

	fail := withFake(&fakeDoer{resp: map[string]fakeResp{claimURL: {500, `{}`}}})
	assert.Equal(t, platform.ClaimFailed, fail.ClaimDrop(context.Background(), sess("a"), platform.DropProgress{DropID: "r1", CampaignID: "c1"}).Outcome)
}

func TestKickProbeChannels(t *testing.T) {
	f := &fakeDoer{resp: map[string]fakeResp{
		"https://kick.com/api/v2/channels/fav/livestream":  {200, `{"data":{"id":7,"viewer_count":3,"categories":[{"name":"Rust","slug":"rust"}]}}`},
		"https://kick.com/api/v2/channels/off/livestream":  {200, `{"data":null}`},
		"https://kick.com/api/v2/channels/other/livestream": {200, `{"data":{"id":8,"viewer_count":9,"categories":[{"name":"Slots","slug":"slots"}]}}`},
	}}
	got, err := withFake(f).ProbeChannels(context.Background(), sess("a"), platform.Campaign{Game: "Rust"}, []string{"Fav", "off", "other"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "fav", got[0].Channel)
}

func TestKickBackend_SatisfiesPipelineInterfaces(t *testing.T) {
	var _ platform.DropProgressSource = (*Backend)(nil)
	var _ platform.DropClaimer = (*Backend)(nil)
	var _ platform.ChannelProber = (*Backend)(nil)
}
```

Run: `go test ./internal/platform/kick/ -run 'KickDropProgress|KickClaimDrop|KickProbe|PipelineInterfaces' -v`
Expected: FAIL, `b.DropProgress undefined`.

- [ ] **Step 3: Implement** (`internal/platform/kick/pipeline.go`)

```go
package kick

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// DropProgress satisfies platform.DropProgressSource. Required minutes come
// from /drops/progress required_units whenever Kick lists the reward. Kick
// lists a reward only after it accrues, so an unlisted reward is reported
// with Known=false instead of guessing.
func (b *Backend) DropProgress(ctx context.Context, s platform.Session, camps []platform.Campaign) ([]platform.DropProgress, error) {
	rewards, err := b.api.progressDetail(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("kick drop progress: %w", err)
	}
	byID := make(map[string]progressReward, len(rewards))
	for _, r := range rewards {
		byID[r.RewardID] = r
	}
	var out []platform.DropProgress
	for _, c := range camps {
		for _, bf := range c.Benefits {
			r, ok := byID[bf.ID]
			if !ok {
				out = append(out, platform.DropProgress{DropID: bf.ID, CampaignID: c.ID, Required: bf.RequiredMinutes})
				continue
			}
			req := r.Required
			if req <= 0 {
				req = bf.RequiredMinutes
			}
			minutes := 0
			if req > 0 && r.Fraction > 0 {
				minutes = int(math.Round(r.Fraction * float64(req)))
			}
			out = append(out, platform.DropProgress{
				DropID: bf.ID, CampaignID: c.ID, Minutes: minutes, Required: req,
				Claimed: r.Claimed, Known: true,
			})
		}
	}
	return out, nil
}

// ClaimDrop satisfies platform.DropClaimer. A claim error on an already
// auto-granted reward shows up as ClaimFailed; the post-claim reconcile
// then sees claimed=true and corrects the state.
func (b *Backend) ClaimDrop(ctx context.Context, s platform.Session, d platform.DropProgress) platform.ClaimResult {
	err := b.api.Claim(ctx, s, d.DropID, d.CampaignID)
	if err == nil {
		return platform.ClaimResult{Outcome: platform.ClaimOK}
	}
	var linkErr *ClaimNeedsLinkError
	if errors.As(err, &linkErr) {
		return platform.ClaimResult{Outcome: platform.ClaimNeedsLink, LinkURL: linkErr.ConnectURL, Detail: err.Error()}
	}
	return platform.ClaimResult{Outcome: platform.ClaimFailed, Detail: err.Error()}
}

// ProbeChannels satisfies platform.ChannelProber on top of probeLive.
func (b *Backend) ProbeChannels(ctx context.Context, s platform.Session, c platform.Campaign, logins []string) ([]platform.Stream, error) {
	pool := make([]kickChannel, 0, len(logins))
	for _, l := range logins {
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			pool = append(pool, kickChannel{Slug: l})
		}
	}
	return b.probeLive(ctx, s, c, pool), nil
}

var (
	_ platform.DropProgressSource = (*Backend)(nil)
	_ platform.DropClaimer        = (*Backend)(nil)
	_ platform.ChannelProber      = (*Backend)(nil)
)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/platform/kick/ -v 2>&1 | tail -30`
Expected: PASS (including existing sweep tests, since `progressReward` only gained a field).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/platform
git add internal/platform/kick
git commit -m "feat(pipeline-v2): Kick adapter with platform-sourced required minutes

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Reconciler

**Files:**
- Create: `internal/pipeline/reconcile/reconcile.go`
- Test: `internal/pipeline/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: `dropstate` (Task 2), `platform.Backend.ListActiveCampaigns`, `platform.DropProgressSource` (Task 5).
- Produces:

```go
type CampaignPersister interface { PersistCampaigns(ctx context.Context, camps []platform.Campaign) error }
type Config struct {
	AccountID, Platform string
	Backend      platform.Backend
	Progress     platform.DropProgressSource
	Session      platform.Session
	AllowGame    func(game string) bool
	AllowChannel func(channels []string) bool
	ForceLinked  func(campaignID string) bool
	Persister    CampaignPersister
}
type Result struct {
	Campaigns []platform.Campaign               // active, in scope
	Rows      []dropstate.Row                   // every row touched this sync
	Progress  map[string]platform.DropProgress  // by drop id
}
func Run(ctx context.Context, cfg Config, prev map[string]dropstate.Row, now time.Time) (Result, error)
func InScope(camps []platform.Campaign, allowGame func(string) bool, allowChannel func([]string) bool) []platform.Campaign
```

- [ ] **Step 1: Failing tests**

```go
package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeBackend struct {
	platform.Backend // nil: unimplemented methods panic
	camps            []platform.Campaign
	progress         []platform.DropProgress
	progErr          error
}

func (f *fakeBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	return f.camps, nil
}

func (f *fakeBackend) DropProgress(context.Context, platform.Session, []platform.Campaign) ([]platform.DropProgress, error) {
	return f.progress, f.progErr
}

type fakePersister struct{ got []platform.Campaign }

func (p *fakePersister) PersistCampaigns(_ context.Context, c []platform.Campaign) error {
	p.got = append(p.got, c...)
	return nil
}

var now = time.Unix(1_700_000_000, 0)

func camp(id, game string, drops ...string) platform.Campaign {
	c := platform.Campaign{ID: id, Platform: "twitch", Game: game, Status: "active", AccountLinked: true, AccountLinkChecked: true}
	for _, d := range drops {
		c.Benefits = append(c.Benefits, platform.DropBenefit{ID: d, CampaignID: id, RequiredMinutes: 60})
	}
	return c
}

func cfg(f *fakeBackend, p *fakePersister) Config {
	return Config{AccountID: "a", Platform: "twitch", Backend: f, Progress: f, Persister: p,
		AllowGame: func(g string) bool { return g == "G" }}
}

func byDrop(rows []dropstate.Row) map[string]dropstate.Row {
	m := map[string]dropstate.Row{}
	for _, r := range rows {
		m[r.DropID] = r
	}
	return m
}

func TestRun_ClaimedAfterLeavingInventory(t *testing.T) {
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "G", "d1")},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 60, Required: 60, Claimed: true, Known: true}}}
	prev := map[string]dropstate.Row{"d1": {AccountID: "a", DropID: "d1", CampaignID: "c1", Platform: "twitch", Status: dropstate.Accruing, Minutes: 30, Required: 60, Source: dropstate.FromPlatform}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), prev, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.Claimed, byDrop(res.Rows)["d1"].Status)
}

func TestRun_NewDropSeedsIdentity(t *testing.T) {
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "G", "d1")},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 5, Required: 60, Known: true}}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), nil, now)
	require.NoError(t, err)
	r := byDrop(res.Rows)["d1"]
	assert.Equal(t, "a", r.AccountID)
	assert.Equal(t, "c1", r.CampaignID)
	assert.Equal(t, "twitch", r.Platform)
	assert.Equal(t, dropstate.Accruing, r.Status)
}

func TestRun_ProgressErrorChangesNothing(t *testing.T) {
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "G", "d1")}, progErr: errors.New("details 500")}
	prev := map[string]dropstate.Row{"d1": {DropID: "d1", Status: dropstate.Accruing, Minutes: 30, Required: 60}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), prev, now)
	require.Error(t, err)
	assert.Empty(t, res.Rows)
}

func TestRun_MultiItemDropOneRow(t *testing.T) {
	c := camp("c1", "G", "d1", "d1")
	f := &fakeBackend{camps: []platform.Campaign{c},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Required: 60, Known: true}}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), nil, now)
	require.NoError(t, err)
	assert.Len(t, res.Rows, 1)
}

func TestRun_UnlinkedCampaignBlocked_UnlessForced(t *testing.T) {
	c := camp("c1", "G", "d1")
	c.AccountLinked = false
	f := &fakeBackend{camps: []platform.Campaign{c},
		progress: []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Required: 60, Known: true}}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), nil, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.NeedsLink, byDrop(res.Rows)["d1"].Reason)

	cf := cfg(f, &fakePersister{})
	cf.ForceLinked = func(id string) bool { return id == "c1" }
	res, err = Run(context.Background(), cf, nil, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.Eligible, byDrop(res.Rows)["d1"].Status)
}

func TestRun_ExpiredCampaign(t *testing.T) {
	c := camp("c1", "G", "d1")
	c.Status = "expired"
	f := &fakeBackend{camps: []platform.Campaign{c}}
	prev := map[string]dropstate.Row{"d1": {DropID: "d1", Status: dropstate.Accruing, Minutes: 30, Required: 60}}
	res, err := Run(context.Background(), cfg(f, &fakePersister{}), prev, now)
	require.NoError(t, err)
	assert.Equal(t, dropstate.Expired, byDrop(res.Rows)["d1"].Reason)
	assert.Empty(t, res.Campaigns, "expired campaigns are not mining candidates")
}

func TestRun_ScopeAndPersist(t *testing.T) {
	nullGame := camp("c2", "", "d2")
	nullGame.AllowedChannels = []string{"fav"}
	reward := camp("c3", "G", "d3")
	reward.Kind = "reward"
	f := &fakeBackend{camps: []platform.Campaign{camp("c1", "Other", "d1"), nullGame, reward},
		progress: []platform.DropProgress{{DropID: "d2", CampaignID: "c2", Required: 60, Known: true}}}
	p := &fakePersister{}
	cf := cfg(f, p)
	cf.AllowChannel = func(chs []string) bool { return len(chs) > 0 && chs[0] == "fav" }
	res, err := Run(context.Background(), cf, nil, now)
	require.NoError(t, err)
	got := byDrop(res.Rows)
	assert.NotContains(t, got, "d1", "non-whitelisted game")
	assert.Contains(t, got, "d2", "null-game campaign allowed by channel")
	assert.NotContains(t, got, "d3", "reward kind has no watch time")
	require.Len(t, p.got, 1)
	assert.Equal(t, "c2", p.got[0].ID)
}
```

Run: `go test ./internal/pipeline/reconcile/ -v`
Expected: FAIL, `undefined: Run`.

- [ ] **Step 2: Implement**

```go
// Package reconcile folds platform per-campaign progress into drop_state.
// It never decides what to watch.
package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type CampaignPersister interface {
	PersistCampaigns(ctx context.Context, camps []platform.Campaign) error
}

type Config struct {
	AccountID    string
	Platform     string
	Backend      platform.Backend
	Progress     platform.DropProgressSource
	Session      platform.Session
	AllowGame    func(game string) bool
	AllowChannel func(channels []string) bool
	ForceLinked  func(campaignID string) bool
	Persister    CampaignPersister
}

type Result struct {
	Campaigns []platform.Campaign
	Rows      []dropstate.Row
	Progress  map[string]platform.DropProgress
}

// InScope keeps campaigns the account mines: whitelisted game, or a
// campaign whose channels match the account's priority streamers
// (null-game drops). One-click reward campaigns have no watch time.
func InScope(camps []platform.Campaign, allowGame func(string) bool, allowChannel func([]string) bool) []platform.Campaign {
	var out []platform.Campaign
	for _, c := range camps {
		if c.Kind == "reward" {
			continue
		}
		ok := allowGame == nil || allowGame(c.Game)
		if !ok && allowChannel != nil && allowChannel(c.AllowedChannels) {
			ok = true
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

func isActive(c platform.Campaign, now time.Time) bool {
	if c.Status != "" && c.Status != "active" {
		return false
	}
	return c.EndsAt.IsZero() || now.Before(c.EndsAt)
}

// Run lists campaigns, persists the in-scope ones, reads their progress and
// returns every drop row it touched. On any platform error it returns the
// error and no rows, so a failed sync never changes state.
func Run(ctx context.Context, cfg Config, prev map[string]dropstate.Row, now time.Time) (Result, error) {
	camps, err := cfg.Backend.ListActiveCampaigns(ctx, cfg.Session)
	if err != nil {
		return Result{}, fmt.Errorf("list campaigns: %w", err)
	}
	scope := InScope(camps, cfg.AllowGame, cfg.AllowChannel)
	if cfg.Persister != nil && len(scope) > 0 {
		if err := cfg.Persister.PersistCampaigns(ctx, scope); err != nil {
			return Result{}, fmt.Errorf("persist campaigns: %w", err)
		}
	}
	var active []platform.Campaign
	for _, c := range scope {
		if isActive(c, now) {
			active = append(active, c)
		}
	}
	byDrop := map[string]platform.DropProgress{}
	if len(active) > 0 {
		obs, err := cfg.Progress.DropProgress(ctx, cfg.Session, active)
		if err != nil {
			return Result{}, fmt.Errorf("drop progress: %w", err)
		}
		for _, o := range obs {
			byDrop[o.DropID] = o
		}
	}

	res := Result{Campaigns: active, Progress: byDrop}
	seen := map[string]bool{}
	for _, c := range scope {
		if c.Status == "upcoming" {
			continue
		}
		expired := !isActive(c, now)
		linked := c.AccountLinked || !c.AccountLinkChecked || (cfg.ForceLinked != nil && cfg.ForceLinked(c.ID))
		for _, b := range c.Benefits {
			if b.ID == "" || seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			p, ok := prev[b.ID]
			if !ok || p.IsZero() {
				p = dropstate.Row{AccountID: cfg.AccountID, DropID: b.ID, CampaignID: c.ID, Platform: cfg.Platform, Source: dropstate.FromPlatform}
			}
			var next dropstate.Row
			if expired {
				if p.IsZero() {
					continue // never tracked, nothing to expire
				}
				next = dropstate.Expire(p, now)
			} else {
				o, known := byDrop[b.ID]
				req := b.RequiredMinutes
				if known {
					req = o.Required
				}
				next = dropstate.Apply(p, dropstate.Observation{
					Known: known && o.Known, Unmineable: o.Unmineable, Claimed: o.Claimed,
					Minutes: o.Minutes, Required: req,
				}, now)
				if !linked {
					next = dropstate.BlockLink(next, now)
				}
			}
			res.Rows = append(res.Rows, next)
		}
	}
	return res, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/pipeline/... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/pipeline
git add internal/pipeline/reconcile
git commit -m "feat(pipeline-v2): reconciler folds platform progress into drop state

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Planner (incl. streamer priority, #49)

**Files:**
- Create: `internal/pipeline/planner/planner.go`
- Test: `internal/pipeline/planner/planner_test.go`

**Interfaces:**
- Consumes: `dropstate.Row`, `dropstate.Mineable`, `dropstate.Claimed`; `platform.Campaign`, `platform.Stream`.
- Produces:

```go
type Kind int // Idle, Mine, ForceWatch; String() "idle" | "mine" | "force_watch"
type Settings struct {
	GameRank         func(game string) int
	PriorityMode     string // "ordered" | "ending_soonest"
	StreamerPriority []string
	ForceWatch       []platform.Stream // live force-watch channels, rank order
}
type Input struct {
	Now       time.Time
	Rows      map[string]dropstate.Row
	Campaigns []platform.Campaign
	Settings  Settings
	Live      map[string][]platform.Stream // campaign id -> live eligible channels
	Cooldowns map[string]time.Time         // lowercased channel -> until
	Current   *Decision
	LastSwap  time.Time
}
type Decision struct {
	Kind       Kind
	CampaignID string
	Channel    platform.Stream
	Serves     []string
	Reason     string
}
func (d Decision) Same(o Decision) bool
func Plan(in Input) Decision
func Candidates(in Input) []platform.Campaign
const SwapHold = 10 * time.Minute
const ReasonNothingToMine = "nothing_to_mine"; const ReasonNoLiveChannels = "no_live_channels"
```

- [ ] **Step 1: Failing tests**

```go
package planner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

var now = time.Unix(1_700_000_000, 0)

func camp(id, game string, ends time.Time, drops ...platform.DropBenefit) platform.Campaign {
	return platform.Campaign{ID: id, Game: game, Status: "active", EndsAt: ends, Benefits: drops}
}

func drop(id, campaign string, req int, pre ...string) platform.DropBenefit {
	return platform.DropBenefit{ID: id, CampaignID: campaign, RequiredMinutes: req, Preconditions: pre}
}

func rows(rs ...dropstate.Row) map[string]dropstate.Row {
	m := map[string]dropstate.Row{}
	for _, r := range rs {
		m[r.DropID] = r
	}
	return m
}

func st(id string, s dropstate.Status) dropstate.Row { return dropstate.Row{DropID: id, Status: s} }

func stream(ch string, viewers int) platform.Stream {
	return platform.Stream{Channel: ch, ViewerCount: viewers}
}

func TestPlan_PicksHighestViewerByDefault(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("small", 5), stream("big", 500)}},
	}
	d := Plan(in)
	assert.Equal(t, Mine, d.Kind)
	assert.Equal(t, "big", d.Channel.Channel)
	assert.Equal(t, []string{"d1"}, d.Serves)
}

func TestPlan_PriorityStreamerFirst(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("big", 500), stream("Fav2", 1), stream("fav1", 2)}},
		Settings:  Settings{StreamerPriority: []string{"fav1", "fav2"}},
	}
	assert.Equal(t, "fav1", Plan(in).Channel.Channel)
	in.Live["c1"] = []platform.Stream{stream("big", 500), stream("Fav2", 1)}
	assert.Equal(t, "Fav2", Plan(in).Channel.Channel, "case-insensitive, next priority")
	in.Live["c1"] = []platform.Stream{stream("big", 500)}
	assert.Equal(t, "big", Plan(in).Channel.Channel, "fallback to category")
}

func TestPlan_GameRankThenEndingSoonest(t *testing.T) {
	soon, late := now.Add(time.Hour), now.Add(48*time.Hour)
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("late", "A", late, drop("d1", "late", 60)), camp("soon", "B", soon, drop("d2", "soon", 60))},
		Rows:      rows(st("d1", dropstate.Eligible), st("d2", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"late": {stream("x", 1)}, "soon": {stream("y", 1)}},
		Settings:  Settings{GameRank: func(g string) int { return map[string]int{"A": 0, "B": 1}[g] }},
	}
	assert.Equal(t, "late", Plan(in).CampaignID, "ordered: game rank wins")
	in.Settings.PriorityMode = "ending_soonest"
	assert.Equal(t, "soon", Plan(in).CampaignID)
}

func TestPlan_SkipsNonMineableAndLockedPreconditions(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{},
			drop("claimed", "c1", 30), drop("locked", "c1", 60, "gate"), drop("gate", "c1", 10))},
		Rows: rows(st("claimed", dropstate.Claimed), st("locked", dropstate.Eligible), st("gate", dropstate.Blocked)),
		Live: map[string][]platform.Stream{"c1": {stream("x", 1)}},
	}
	assert.Equal(t, Idle, Plan(in).Kind, "gate is blocked, locked waits on it")

	in.Rows["gate"] = dropstate.Row{DropID: "gate", Status: dropstate.Claimed, Source: dropstate.FromUser}
	d := Plan(in)
	assert.Equal(t, Mine, d.Kind, "user-marked precondition unlocks (P9)")
	assert.Equal(t, []string{"locked"}, d.Serves)
}

func TestPlan_ServesDropsAcrossCampaignsOnSameChannel(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{
			camp("c1", "Rust", time.Time{}, drop("r1", "c1", 60), drop("r2", "c1", 30)),
			camp("c2", "Rust", time.Time{}, drop("r3", "c2", 120)),
		},
		Rows: rows(st("r1", dropstate.Eligible), st("r2", dropstate.Accruing), st("r3", dropstate.Eligible)),
		Live: map[string][]platform.Stream{"c1": {stream("oilrats", 9)}, "c2": {stream("OILRATS", 9)}},
	}
	d := Plan(in)
	assert.Equal(t, []string{"r2", "r1", "r3"}, d.Serves, "lowest tier first, then other campaigns")
}

func TestPlan_CooldownSkipsChannel(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("big", 500), stream("small", 5)}},
		Cooldowns: map[string]time.Time{"big": now.Add(time.Minute)},
	}
	assert.Equal(t, "small", Plan(in).Channel.Channel)
}

func TestPlan_HysteresisHoldsThenBypassesDeadChannel(t *testing.T) {
	cur := Decision{Kind: Mine, CampaignID: "c1", Channel: stream("big", 500), Serves: []string{"d1"}}
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
		Live:      map[string][]platform.Stream{"c1": {stream("big", 500), stream("fav", 1)}},
		Settings:  Settings{StreamerPriority: []string{"fav"}},
		Current:   &cur, LastSwap: now.Add(-time.Minute),
	}
	assert.Equal(t, "big", Plan(in).Channel.Channel, "inside hold: keep current")
	in.LastSwap = now.Add(-SwapHold - time.Second)
	assert.Equal(t, "fav", Plan(in).Channel.Channel, "hold over: priority streamer wins")
	in.LastSwap = now.Add(-time.Minute)
	in.Live["c1"] = []platform.Stream{stream("fav", 1)}
	assert.Equal(t, "fav", Plan(in).Channel.Channel, "current went offline: bypass hold")
}

func TestPlan_ForceWatchOnlyWhenNothingToMine(t *testing.T) {
	in := Input{Now: now, Settings: Settings{ForceWatch: []platform.Stream{stream("pts", 1)}}}
	d := Plan(in)
	assert.Equal(t, ForceWatch, d.Kind)
	assert.Equal(t, "pts", d.Channel.Channel)

	in.Settings.ForceWatch = nil
	d = Plan(in)
	assert.Equal(t, Idle, d.Kind)
	assert.Equal(t, ReasonNothingToMine, d.Reason)
}

func TestPlan_NoLiveChannelsIdleReason(t *testing.T) {
	in := Input{Now: now,
		Campaigns: []platform.Campaign{camp("c1", "G", time.Time{}, drop("d1", "c1", 60))},
		Rows:      rows(st("d1", dropstate.Eligible)),
	}
	assert.Equal(t, ReasonNoLiveChannels, Plan(in).Reason)
}

func TestPlan_OutsideWindowIgnored(t *testing.T) {
	c := camp("c1", "G", now.Add(-time.Minute), drop("d1", "c1", 60))
	in := Input{Now: now, Campaigns: []platform.Campaign{c}, Rows: rows(st("d1", dropstate.Eligible)),
		Live: map[string][]platform.Stream{"c1": {stream("x", 1)}}}
	assert.Equal(t, Idle, Plan(in).Kind)
}

func TestDecision_Same(t *testing.T) {
	a := Decision{Kind: Mine, Channel: stream("Big", 1)}
	assert.True(t, a.Same(Decision{Kind: Mine, Channel: stream("big", 9)}))
	assert.False(t, a.Same(Decision{Kind: ForceWatch, Channel: stream("big", 9)}))
	assert.True(t, Decision{Kind: Idle}.Same(Decision{Kind: Idle, Reason: "x"}))
}
```

Run: `go test ./internal/pipeline/planner/ -v`
Expected: FAIL, `undefined: Input`.

- [ ] **Step 2: Implement**

```go
// Package planner turns drop state, campaigns and live channels into one
// decision: which channel to watch and which drops that serves. Pure: no
// I/O, no clock reads, no goroutines.
package planner

import (
	"sort"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type Kind int

const (
	Idle Kind = iota
	Mine
	ForceWatch
)

func (k Kind) String() string {
	switch k {
	case Mine:
		return "mine"
	case ForceWatch:
		return "force_watch"
	default:
		return "idle"
	}
}

// SwapHold is the minimum time on a channel before the planner switches to
// a better one, unless the current channel is dead or cooling down.
const SwapHold = 10 * time.Minute

const (
	ReasonNothingToMine  = "nothing_to_mine"
	ReasonNoLiveChannels = "no_live_channels"
)

type Settings struct {
	GameRank         func(game string) int
	PriorityMode     string
	StreamerPriority []string
	ForceWatch       []platform.Stream
}

type Input struct {
	Now       time.Time
	Rows      map[string]dropstate.Row
	Campaigns []platform.Campaign
	Settings  Settings
	Live      map[string][]platform.Stream
	Cooldowns map[string]time.Time
	Current   *Decision
	LastSwap  time.Time
}

type Decision struct {
	Kind       Kind
	CampaignID string
	Channel    platform.Stream
	Serves     []string
	Reason     string
}

// Same reports whether two decisions watch the same thing.
func (d Decision) Same(o Decision) bool {
	return d.Kind == o.Kind && strings.EqualFold(d.Channel.Channel, o.Channel.Channel)
}

func key(ch string) string { return strings.ToLower(ch) }

func cooling(in Input, ch string) bool {
	until, ok := in.Cooldowns[key(ch)]
	return ok && in.Now.Before(until)
}

func inWindow(c platform.Campaign, now time.Time) bool {
	if c.Status != "" && c.Status != "active" {
		return false
	}
	if !c.StartsAt.IsZero() && now.Before(c.StartsAt) {
		return false
	}
	return c.EndsAt.IsZero() || now.Before(c.EndsAt)
}

// mineable returns the campaign's mineable drop ids, lowest tier first.
func mineable(in Input, c platform.Campaign) []string {
	type d struct {
		id  string
		req int
	}
	seen := map[string]bool{}
	var ds []d
	for _, b := range c.Benefits {
		if seen[b.ID] || !dropstate.Mineable(in.Rows[b.ID]) {
			continue
		}
		ok := true
		for _, p := range b.Preconditions {
			if in.Rows[p].Status != dropstate.Claimed {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		seen[b.ID] = true
		ds = append(ds, d{b.ID, b.RequiredMinutes})
	}
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].req != ds[j].req {
			return ds[i].req < ds[j].req
		}
		return ds[i].id < ds[j].id
	})
	out := make([]string, len(ds))
	for i, x := range ds {
		out[i] = x.id
	}
	return out
}

func ends(c platform.Campaign) time.Time {
	if c.EndsAt.IsZero() {
		return time.Unix(1<<62, 0)
	}
	return c.EndsAt
}

// Candidates returns in-window campaigns with at least one mineable drop,
// best first: game rank then ending soonest (ending soonest first when
// PriorityMode is "ending_soonest").
func Candidates(in Input) []platform.Campaign {
	var out []platform.Campaign
	for _, c := range in.Campaigns {
		if inWindow(c, in.Now) && len(mineable(in, c)) > 0 {
			out = append(out, c)
		}
	}
	rank := func(g string) int {
		if in.Settings.GameRank == nil {
			return 0
		}
		return in.Settings.GameRank(g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if in.Settings.PriorityMode == "ending_soonest" && !ends(a).Equal(ends(b)) {
			return ends(a).Before(ends(b))
		}
		if ra, rb := rank(a.Game), rank(b.Game); ra != rb {
			return ra < rb
		}
		if !ends(a).Equal(ends(b)) {
			return ends(a).Before(ends(b))
		}
		return a.ID < b.ID
	})
	return out
}

func hasChannel(streams []platform.Stream, ch string) bool {
	for _, s := range streams {
		if strings.EqualFold(s.Channel, ch) {
			return true
		}
	}
	return false
}

func servedBy(in Input, cands []platform.Campaign, ch string) []string {
	var out []string
	for _, c := range cands {
		if hasChannel(in.Live[c.ID], ch) {
			out = append(out, mineable(in, c)...)
		}
	}
	return out
}

func pickChannel(in Input, c platform.Campaign) (platform.Stream, bool) {
	prio := map[string]int{}
	for i, l := range in.Settings.StreamerPriority {
		if _, dup := prio[key(l)]; !dup {
			prio[key(l)] = i
		}
	}
	live := append([]platform.Stream(nil), in.Live[c.ID]...)
	sort.SliceStable(live, func(i, j int) bool {
		pi, iok := prio[key(live[i].Channel)]
		pj, jok := prio[key(live[j].Channel)]
		if iok != jok {
			return iok
		}
		if iok && pi != pj {
			return pi < pj
		}
		return live[i].ViewerCount > live[j].ViewerCount
	})
	for _, s := range live {
		if !cooling(in, s.Channel) {
			return s, true
		}
	}
	return platform.Stream{}, false
}

func forceOrIdle(in Input, reason string) Decision {
	for _, s := range in.Settings.ForceWatch {
		if !cooling(in, s.Channel) {
			return Decision{Kind: ForceWatch, Channel: s, Reason: "force-watch " + s.Channel}
		}
	}
	return Decision{Kind: Idle, Reason: reason}
}

// Plan decides what the account should watch now.
func Plan(in Input) Decision {
	cands := Candidates(in)
	if len(cands) == 0 {
		return forceOrIdle(in, ReasonNothingToMine)
	}
	if cur := in.Current; cur != nil && cur.Kind == Mine && in.Now.Sub(in.LastSwap) < SwapHold && !cooling(in, cur.Channel.Channel) {
		if serves := servedBy(in, cands, cur.Channel.Channel); len(serves) > 0 {
			d := *cur
			d.Serves, d.Reason = serves, "holding current channel"
			return d
		}
	}
	for _, c := range cands {
		ch, ok := pickChannel(in, c)
		if !ok {
			continue
		}
		reason := "top viewers"
		for _, l := range in.Settings.StreamerPriority {
			if strings.EqualFold(l, ch.Channel) {
				reason = "priority streamer"
				break
			}
		}
		return Decision{Kind: Mine, CampaignID: c.ID, Channel: ch, Serves: servedBy(in, cands, ch.Channel), Reason: reason}
	}
	return forceOrIdle(in, ReasonNoLiveChannels)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/pipeline/planner/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/pipeline
git add internal/pipeline/planner
git commit -m "feat(pipeline-v2): pure planner with streamer priority and swap hysteresis

Refs #49

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Claimer

**Files:**
- Create: `internal/pipeline/claimer/claimer.go`
- Test: `internal/pipeline/claimer/claimer_test.go`

**Interfaces:**
- Consumes: `platform.DropClaimer`, `platform.ClaimResult` (Task 5); `dropstate.ClaimOK/ClaimNeedsLink/ClaimFailedAttempt` (Task 2).
- Produces: `func Attempt(ctx context.Context, c platform.DropClaimer, s platform.Session, row dropstate.Row, dp platform.DropProgress, now time.Time) (dropstate.Row, platform.ClaimResult)`

- [ ] **Step 1: Failing test**

```go
package claimer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeClaimer struct {
	res platform.ClaimResult
	got platform.DropProgress
}

func (f *fakeClaimer) ClaimDrop(_ context.Context, _ platform.Session, d platform.DropProgress) platform.ClaimResult {
	f.got = d
	return f.res
}

func TestAttempt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	row := dropstate.Row{DropID: "d1", CampaignID: "c1", Status: dropstate.Claimable, Minutes: 60, Required: 60}
	cases := []struct {
		out    platform.ClaimOutcome
		status dropstate.Status
		reason dropstate.Reason
	}{
		{platform.ClaimOK, dropstate.Claimed, dropstate.NoReason},
		{platform.ClaimAlready, dropstate.Claimed, dropstate.NoReason},
		{platform.ClaimNeedsLink, dropstate.Blocked, dropstate.NeedsLink},
		{platform.ClaimFailed, dropstate.Claimable, dropstate.NoReason},
	}
	for _, tc := range cases {
		t.Run(tc.out.String(), func(t *testing.T) {
			fc := &fakeClaimer{res: platform.ClaimResult{Outcome: tc.out}}
			got, res := Attempt(context.Background(), fc, platform.Session{}, row, platform.DropProgress{DropID: "d1", InstanceID: "i"}, now)
			assert.Equal(t, tc.status, got.Status)
			assert.Equal(t, tc.reason, got.Reason)
			assert.Equal(t, tc.out, res.Outcome)
			assert.Equal(t, "i", fc.got.InstanceID, "instance id forwarded")
		})
	}
	fc := &fakeClaimer{res: platform.ClaimResult{Outcome: platform.ClaimFailed}}
	got, _ := Attempt(context.Background(), fc, platform.Session{}, row, platform.DropProgress{DropID: "d1"}, now)
	assert.Equal(t, 1, got.FailCount)
	assert.Equal(t, now.Add(time.Minute), got.RetryAfter)
}
```

Run: `go test ./internal/pipeline/claimer/ -v`
Expected: FAIL, `undefined: Attempt`.

- [ ] **Step 2: Implement**

```go
// Package claimer turns a claimable drop into claimed or blocked.
package claimer

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// Attempt claims one drop and returns its next state. The caller must
// reconcile afterwards: a lost response shows up as ClaimFailed here and is
// corrected when the platform reports isClaimed.
func Attempt(ctx context.Context, c platform.DropClaimer, s platform.Session, row dropstate.Row, dp platform.DropProgress, now time.Time) (dropstate.Row, platform.ClaimResult) {
	res := c.ClaimDrop(ctx, s, dp)
	switch res.Outcome {
	case platform.ClaimOK, platform.ClaimAlready:
		return dropstate.ClaimOK(row, now), res
	case platform.ClaimNeedsLink:
		return dropstate.ClaimNeedsLink(row, now), res
	default:
		return dropstate.ClaimFailedAttempt(row, now), res
	}
}
```

- [ ] **Step 3: Run tests and commit**

Run: `go test ./internal/pipeline/claimer/ -v`
Expected: PASS.

```bash
gofmt -w internal/pipeline
git add internal/pipeline/claimer
git commit -m "feat(pipeline-v2): claimer maps typed claim outcomes to drop state

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Session

**Files:**
- Create: `internal/pipeline/session/session.go`
- Test: `internal/pipeline/session/session_test.go`

**Interfaces:**
- Consumes: `platform.Backend` (`StartWatch`, `Heartbeat`, `StopWatch`, `InventoryProgress`).
- Produces:

```go
type Kind int // Progress, StreamDown, Stalled
type Event struct { Kind Kind; Channel string; Progress []platform.Progress; Err error }
type Config struct {
	Backend    platform.Backend
	Session    platform.Session
	Stream     platform.Stream
	Serves     []string
	Ticks      <-chan time.Time // 60s ticker in production
	StallPolls int              // consecutive polls with no gain before Stalled
}
func Run(ctx context.Context, cfg Config, out chan<- Event)
```

- [ ] **Step 1: Failing tests**

```go
package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type fakeBackend struct {
	platform.Backend
	mu       sync.Mutex
	startErr error
	beatErr  error
	minutes  []int // minutes for d1 per poll; last value repeats
	polls    int
	beats    int
	stopped  bool
}

func (f *fakeBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	return platform.WatchHandle{Channel: s.Channel}, f.startErr
}
func (f *fakeBackend) Heartbeat(context.Context, platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beats++
	return f.beatErr
}
func (f *fakeBackend) StopWatch(context.Context, platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}
func (f *fakeBackend) InventoryProgress(context.Context, platform.Session) ([]platform.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.polls
	if i >= len(f.minutes) {
		i = len(f.minutes) - 1
	}
	f.polls++
	return []platform.Progress{{BenefitID: "d1", MinutesWatched: f.minutes[i]}}, nil
}

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

func TestRun_ProgressEachTickAndStopOnCancel(t *testing.T) {
	f := &fakeBackend{minutes: []int{1, 2}}
	ticks := make(chan time.Time)
	out := make(chan Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Serves: []string{"d1"}, Ticks: ticks, StallPolls: 5}, out); close(done) }()

	e := recv(t, out)
	assert.Equal(t, Progress, e.Kind, "first beat is immediate")
	assert.Equal(t, 1, e.Progress[0].MinutesWatched)
	ticks <- time.Now()
	assert.Equal(t, 2, recv(t, out).Progress[0].MinutesWatched)
	cancel()
	<-done
	assert.True(t, f.stopped)
}

func TestRun_HeartbeatErrorIsStreamDown(t *testing.T) {
	f := &fakeBackend{minutes: []int{1}, beatErr: errors.New("offline")}
	out := make(chan Event, 8)
	Run(context.Background(), Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Ticks: make(chan time.Time)}, out)
	e := recv(t, out)
	assert.Equal(t, StreamDown, e.Kind)
	assert.Equal(t, "ch", e.Channel)
	assert.True(t, f.stopped)
}

func TestRun_StartErrorIsStreamDown(t *testing.T) {
	f := &fakeBackend{startErr: errors.New("sidecar")}
	out := make(chan Event, 8)
	Run(context.Background(), Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Ticks: make(chan time.Time)}, out)
	assert.Equal(t, StreamDown, recv(t, out).Kind)
}

func TestRun_StalledOnceAfterNoGain(t *testing.T) {
	f := &fakeBackend{minutes: []int{5}}
	ticks := make(chan time.Time)
	out := make(chan Event, 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Config{Backend: f, Stream: platform.Stream{Channel: "ch"}, Serves: []string{"d1"}, Ticks: ticks, StallPolls: 2}, out)
	require.Equal(t, Progress, recv(t, out).Kind) // poll 1 sets baseline
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind) // no gain 1
	ticks <- time.Now()
	require.Equal(t, Progress, recv(t, out).Kind) // no gain 2
	assert.Equal(t, Stalled, recv(t, out).Kind)
	ticks <- time.Now()
	assert.Equal(t, Progress, recv(t, out).Kind)
	select {
	case e := <-out:
		t.Fatalf("unexpected second event %v", e.Kind)
	case <-time.After(50 * time.Millisecond):
	}
}
```

Run: `go test ./internal/pipeline/session/ -v`
Expected: FAIL, `undefined: Run`.

- [ ] **Step 2: Implement**

```go
// Package session runs one accrual session on one channel and reports what
// it sees. It makes no decisions.
package session

import (
	"context"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

type Kind int

const (
	Progress Kind = iota
	StreamDown
	Stalled
)

type Event struct {
	Kind     Kind
	Channel  string
	Progress []platform.Progress
	Err      error
}

type Config struct {
	Backend platform.Backend
	Session platform.Session
	Stream  platform.Stream
	Serves  []string
	// Ticks drives beats. Production passes a 60s ticker: Twitch credits one
	// minute per beacon, so a slower cadence under-credits.
	Ticks      <-chan time.Time
	StallPolls int
}

// Run starts the watch, beats immediately and on every tick, and stops the
// watch when ctx ends or the stream goes down.
func Run(ctx context.Context, cfg Config, out chan<- Event) {
	ch := cfg.Stream.Channel
	send := func(e Event) {
		e.Channel = ch
		select {
		case out <- e:
		case <-ctx.Done():
		}
	}
	h, err := cfg.Backend.StartWatch(ctx, cfg.Session, cfg.Stream)
	if err != nil {
		send(Event{Kind: StreamDown, Err: err})
		return
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cfg.Backend.StopWatch(stopCtx, h)
	}()

	serves := make(map[string]bool, len(cfg.Serves))
	for _, id := range cfg.Serves {
		serves[id] = true
	}
	best, still, stalled := -1, 0, false
	beat := func() bool {
		if err := cfg.Backend.Heartbeat(ctx, h); err != nil {
			if ctx.Err() == nil {
				send(Event{Kind: StreamDown, Err: err})
			}
			return false
		}
		prog, err := cfg.Backend.InventoryProgress(ctx, cfg.Session)
		if err != nil {
			return true // transient; the next tick retries
		}
		send(Event{Kind: Progress, Progress: prog})
		total := 0
		for _, p := range prog {
			if serves[p.BenefitID] {
				total += p.MinutesWatched
			}
		}
		if total > best {
			best, still = total, 0
		} else {
			still++
		}
		if !stalled && cfg.StallPolls > 0 && still >= cfg.StallPolls {
			stalled = true
			send(Event{Kind: Stalled})
		}
		return true
	}
	if !beat() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-cfg.Ticks:
			if !beat() {
				return
			}
		}
	}
}
```

- [ ] **Step 3: Run tests and commit**

Run: `go test ./internal/pipeline/session/ -race -v`
Expected: PASS, no races.

```bash
gofmt -w internal/pipeline
git add internal/pipeline/session
git commit -m "feat(pipeline-v2): accrual session reports progress, stream-down, stall

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Account loop

**Files:**
- Create: `internal/pipeline/loop/loop.go`
- Test: `internal/pipeline/loop/loop_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2, 5, 7 to 10; `watcher.Snapshot` (existing, for dashboard compatibility); `platform.PubSubAware`, `platform.PubSubHooks`, `platform.ChannelSubscriber` (existing).
- Produces:

```go
type Notifier interface { Notify(ctx context.Context, event string, fields map[string]any) error }
type Store interface {
	List(ctx context.Context, accountID string) ([]dropstate.Row, error)
	Upsert(ctx context.Context, r dropstate.Row) error
}
type ClaimHistory interface {
	RecordClaimIfNew(ctx context.Context, accountID string, b platform.DropBenefit) (bool, error)
	ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
}
type Config struct { ... see code ... }
func New(cfg Config) (*Loop, error)
func (l *Loop) Run(ctx context.Context) error
func (l *Loop) Snapshot() watcher.Snapshot
func (l *Loop) LastDiscovery() ([]platform.Campaign, time.Time)
func (l *Loop) AllowGame() func(game string) bool
func (l *Loop) Nudge()
```

`*store.DropStateStore` satisfies `Store`; `*store.ClaimRecorder` satisfies `ClaimHistory`; `*store.CampaignPersister` satisfies `reconcile.CampaignPersister`.

- [ ] **Step 1: Failing tests**

```go
package loop

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeBackend: one game "G", campaign c1 with drop d1 (60 min). Tests
// mutate the fields under mu to script the platform.
type fakeBackend struct {
	platform.Backend
	mu         sync.Mutex
	camps      []platform.Campaign
	progress   []platform.DropProgress
	inventory  []platform.Progress
	live       []platform.Stream
	claimRes   platform.ClaimResult
	claims     int
	watching   []string
	stops      int
	beatFailOn string
}

func (f *fakeBackend) ListActiveCampaigns(context.Context, platform.Session) ([]platform.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.camps, nil
}
func (f *fakeBackend) DropProgress(context.Context, platform.Session, []platform.Campaign) ([]platform.DropProgress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.DropProgress(nil), f.progress...), nil
}
func (f *fakeBackend) ListEligibleChannels(context.Context, platform.Session, platform.Campaign) ([]platform.Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.Stream(nil), f.live...), nil
}
func (f *fakeBackend) InventoryProgress(context.Context, platform.Session) ([]platform.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.Progress(nil), f.inventory...), nil
}
func (f *fakeBackend) StartWatch(_ context.Context, _ platform.Session, s platform.Stream) (platform.WatchHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watching = append(f.watching, s.Channel)
	return platform.WatchHandle{Channel: s.Channel}, nil
}
func (f *fakeBackend) Heartbeat(_ context.Context, h platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beatFailOn != "" && strings.EqualFold(h.Channel, f.beatFailOn) {
		return context.DeadlineExceeded
	}
	return nil
}
func (f *fakeBackend) StopWatch(context.Context, platform.WatchHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}
func (f *fakeBackend) ClaimDrop(context.Context, platform.Session, platform.DropProgress) platform.ClaimResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	return f.claimRes
}

type memStore struct {
	mu   sync.Mutex
	rows map[string]dropstate.Row
}

func (m *memStore) List(context.Context, string) ([]dropstate.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []dropstate.Row
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}
func (m *memStore) Upsert(_ context.Context, r dropstate.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.DropID] = r
	return nil
}
func (m *memStore) get(id string) dropstate.Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[id]
}

type memHistory struct {
	mu       sync.Mutex
	recorded []string
	claimed  map[string]bool
}

func (h *memHistory) RecordClaimIfNew(_ context.Context, _ string, b platform.DropBenefit) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recorded = append(h.recorded, b.ID)
	return true, nil
}
func (h *memHistory) ClaimedBenefitIDs(context.Context, string) (map[string]bool, error) {
	return h.claimed, nil
}

func setup(t *testing.T) (*fakeBackend, *memStore, *memHistory, Config) {
	f := &fakeBackend{
		camps: []platform.Campaign{{ID: "c1", Platform: "twitch", Game: "G", Name: "Camp", Status: "active",
			AccountLinked: true, AccountLinkChecked: true,
			Benefits: []platform.DropBenefit{{ID: "d1", CampaignID: "c1", Name: "Drop", RequiredMinutes: 60}}}},
		progress:  []platform.DropProgress{{DropID: "d1", CampaignID: "c1", Minutes: 10, Required: 60, Known: true}},
		inventory: []platform.Progress{{BenefitID: "d1", MinutesWatched: 10}},
		live:      []platform.Stream{{Channel: "ch1", ViewerCount: 10}, {Channel: "ch2", ViewerCount: 5}},
		claimRes:  platform.ClaimResult{Outcome: platform.ClaimOK},
	}
	st := &memStore{rows: map[string]dropstate.Row{}}
	h := &memHistory{}
	cfg := Config{
		AccountID: "acc", Platform: "twitch", Backend: f, Store: st, History: h,
		AllowGame:      func(g string) bool { return g == "G" },
		ReconcileEvery: time.Hour, LiveEvery: time.Hour,
		BeatEvery: 10 * time.Millisecond, StallPolls: 1000, // tests opt into stalls
	}
	return f, st, h, cfg
}

func run(t *testing.T, cfg Config) (*Loop, context.CancelFunc) {
	l, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = l.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return l, cancel
}

func TestLoop_ClaimedOnWebsiteMidWatch(t *testing.T) {
	f, st, h, cfg := setup(t)
	l, _ := run(t, cfg)
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch1" }, 2*time.Second, 5*time.Millisecond)

	f.mu.Lock()
	f.inventory = []platform.Progress{{BenefitID: "d1", MinutesWatched: 20, Claimed: true}}
	f.mu.Unlock()

	require.Eventually(t, func() bool { return st.get("d1").Status == dropstate.Claimed }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return l.Snapshot().State == "idle" }, 2*time.Second, 5*time.Millisecond)
	h.mu.Lock()
	assert.Contains(t, h.recorded, "d1")
	h.mu.Unlock()
	f.mu.Lock()
	assert.Equal(t, 0, f.claims, "no claim call for an already-claimed drop")
	assert.GreaterOrEqual(t, f.stops, 1)
	f.mu.Unlock()
}

func TestLoop_ClaimsWhenClaimable(t *testing.T) {
	f, st, _, cfg := setup(t)
	f.progress[0].Minutes = 60
	f.inventory[0].MinutesWatched = 60
	run(t, cfg)
	require.Eventually(t, func() bool { return st.get("d1").Status == dropstate.Claimed }, 2*time.Second, 5*time.Millisecond)
	f.mu.Lock()
	assert.Equal(t, 1, f.claims)
	f.mu.Unlock()
}

func TestLoop_NeedsLinkBlocksWithoutLooping(t *testing.T) {
	f, st, _, cfg := setup(t)
	f.progress[0].Minutes = 60
	f.inventory[0].MinutesWatched = 60
	f.claimRes = platform.ClaimResult{Outcome: platform.ClaimNeedsLink}
	run(t, cfg)
	require.Eventually(t, func() bool { return st.get("d1").Reason == dropstate.NeedsLink }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	assert.Equal(t, 1, f.claims, "needs_link is not retried")
	f.mu.Unlock()
}

func TestLoop_StallCoolsChannelAndSwaps(t *testing.T) {
	_, _, _, cfg := setup(t)
	cfg.StallPolls = 3
	l, _ := run(t, cfg)
	// Minutes never move, so after StallPolls the loop cools ch1 and moves on.
	require.Eventually(t, func() bool { return l.Snapshot().Channel == "ch2" }, 3*time.Second, 5*time.Millisecond)
}

func TestLoop_StaleStreamDownIgnored(t *testing.T) {
	f, _, _, cfg := setup(t)
	l, err := New(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Drive the loop manually: load, reconcile, refresh, plan.
	require.NoError(t, l.load(ctx))
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.replan(ctx)
	require.Equal(t, "ch1", l.current.Channel.Channel)
	l.onSession(ctx, sessionEvent("ch-old-down"))
	assert.Empty(t, l.cooldowns, "event from another channel must not cool anything")
	l.stopSession()
	_ = f
}

func TestLoop_BridgesManualMarksOnLoad(t *testing.T) {
	_, st, h, cfg := setup(t)
	st.rows["d1"] = dropstate.Row{AccountID: "acc", DropID: "d1", CampaignID: "c1", Platform: "twitch", Status: dropstate.Accruing, Minutes: 10, Required: 60, Source: dropstate.FromPlatform}
	h.claimed = map[string]bool{"d1": true}
	l, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, l.load(context.Background()))
	assert.Equal(t, dropstate.Claimed, st.get("d1").Status)
	assert.Equal(t, dropstate.FromUser, st.get("d1").Source)
}

func TestNew_RejectsBackendWithoutCapabilities(t *testing.T) {
	type bare struct{ platform.Backend }
	_, err := New(Config{AccountID: "a", Backend: bare{}})
	require.Error(t, err)
}
```

Add this helper at the bottom of the test file (it builds a StreamDown for a given channel):

```go
func sessionEvent(ch string) session.Event { return session.Event{Kind: session.StreamDown, Channel: ch} }
```

and add `"github.com/aalejandrofer/grubdrops/internal/pipeline/session"` to the test imports.

Run: `go test ./internal/pipeline/loop/ -v`
Expected: FAIL, `undefined: New`.

- [ ] **Step 2: Implement**

```go
// Package loop is the per-account pipeline v2 runner. One goroutine owns
// every drop state transition: it reconciles, plans, runs one session at a
// time, and claims.
package loop

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/pipeline/claimer"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/dropstate"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/planner"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/reconcile"
	"github.com/aalejandrofer/grubdrops/internal/pipeline/session"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type Notifier interface {
	Notify(ctx context.Context, event string, fields map[string]any) error
}

type Store interface {
	List(ctx context.Context, accountID string) ([]dropstate.Row, error)
	Upsert(ctx context.Context, r dropstate.Row) error
}

type ClaimHistory interface {
	RecordClaimIfNew(ctx context.Context, accountID string, b platform.DropBenefit) (bool, error)
	ClaimedBenefitIDs(ctx context.Context, accountID string) (map[string]bool, error)
}

type Config struct {
	AccountID    string
	AccountLabel string
	Platform     string
	Backend      platform.Backend
	Session      platform.Session
	Store        Store
	History      ClaimHistory
	Persister    reconcile.CampaignPersister
	Notifier     Notifier

	AllowGame             func(game string) bool
	AllowChannel          func(channels []string) bool
	GameRank              func(game string) int
	PriorityMode          string
	ForceLinked           func(campaignID string) bool
	StreamerPriority      []string
	ForceWatch            []string
	ProgressNotifyStepPct int

	Now            func() time.Time
	ReconcileEvery time.Duration // default 15m
	LiveEvery      time.Duration // default 5m
	BeatEvery      time.Duration // default 60s; production never overrides (Twitch credit gate)
	StallPolls     int           // default 5
	StallCooldown  time.Duration // default 30m
	DownCooldown   time.Duration // default 2m
}

// maxLiveCampaigns bounds channel lookups per refresh.
const maxLiveCampaigns = 8

type pubsubEvent struct {
	kind     string // "progress" | "claimable" | "down"
	drop     string
	instance string
	minutes  int
	required int
	channel  string
}

type Loop struct {
	cfg     Config
	prog    platform.DropProgressSource
	claimer platform.DropClaimer
	prober  platform.ChannelProber
	subs    platform.ChannelSubscriber

	nudge      chan struct{}
	pubsub     chan pubsubEvent
	sessEvents chan session.Event

	mu         sync.Mutex
	snap       watcher.Snapshot
	discovery  []platform.Campaign
	discoverAt time.Time

	// Owned by the Run goroutine.
	rows       map[string]dropstate.Row
	camps      []platform.Campaign
	progress   map[string]platform.DropProgress
	live       map[string][]platform.Stream
	forceLive  []platform.Stream
	cooldowns  map[string]time.Time
	current    *planner.Decision
	lastSwap   time.Time
	sessCancel context.CancelFunc
	subscribed string
	milestones map[string]int
}

// New assembles the loop, checking the backend's pipeline capabilities once.
func New(cfg Config) (*Loop, error) {
	prog, ok := cfg.Backend.(platform.DropProgressSource)
	if !ok {
		return nil, fmt.Errorf("backend %T has no DropProgress: pipeline v2 unsupported", cfg.Backend)
	}
	cl, ok := cfg.Backend.(platform.DropClaimer)
	if !ok {
		return nil, fmt.Errorf("backend %T has no ClaimDrop: pipeline v2 unsupported", cfg.Backend)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ReconcileEvery <= 0 {
		cfg.ReconcileEvery = 15 * time.Minute
	}
	if cfg.LiveEvery <= 0 {
		cfg.LiveEvery = 5 * time.Minute
	}
	if cfg.BeatEvery <= 0 {
		cfg.BeatEvery = 60 * time.Second
	}
	if cfg.StallPolls <= 0 {
		cfg.StallPolls = 5
	}
	if cfg.StallCooldown <= 0 {
		cfg.StallCooldown = 30 * time.Minute
	}
	if cfg.DownCooldown <= 0 {
		cfg.DownCooldown = 2 * time.Minute
	}
	cfg.Session.AccountID = cfg.AccountID
	if cfg.AllowGame != nil {
		cfg.Session.GameFilter = cfg.AllowGame
	}
	l := &Loop{
		cfg: cfg, prog: prog, claimer: cl,
		nudge:      make(chan struct{}, 1),
		pubsub:     make(chan pubsubEvent, 64),
		sessEvents: make(chan session.Event, 64),
		rows:       map[string]dropstate.Row{},
		progress:   map[string]platform.DropProgress{},
		live:       map[string][]platform.Stream{},
		cooldowns:  map[string]time.Time{},
		milestones: map[string]int{},
	}
	l.prober, _ = cfg.Backend.(platform.ChannelProber)
	l.subs, _ = cfg.Backend.(platform.ChannelSubscriber)
	l.snap = watcher.Snapshot{AccountID: cfg.AccountID, State: "pick_campaign"}
	if ps, ok := cfg.Backend.(platform.PubSubAware); ok {
		ps.SetAccountPubSubHooks(cfg.AccountID, l.hooks())
	}
	return l, nil
}

func (l *Loop) hooks() platform.PubSubHooks {
	push := func(e pubsubEvent) {
		select {
		case l.pubsub <- e:
		default: // never block the PubSub reader
		}
	}
	return platform.PubSubHooks{
		OnDropProgress: func(dropID string, cur, req int64) {
			push(pubsubEvent{kind: "progress", drop: dropID, minutes: int(cur), required: int(req)})
		},
		OnDropClaim: func(dropID, instanceID string) {
			push(pubsubEvent{kind: "claimable", drop: dropID, instance: instanceID})
		},
		OnStreamDown: func(channelID string) {
			push(pubsubEvent{kind: "down", channel: channelID})
		},
	}
}

// Nudge asks the loop to reconcile and refresh channels now.
func (l *Loop) Nudge() {
	select {
	case l.nudge <- struct{}{}:
	default:
	}
}

func (l *Loop) Snapshot() watcher.Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snap
}

func (l *Loop) LastDiscovery() ([]platform.Campaign, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]platform.Campaign(nil), l.discovery...), l.discoverAt
}

func (l *Loop) AllowGame() func(game string) bool { return l.cfg.AllowGame }

func (l *Loop) Run(ctx context.Context) error {
	if err := l.load(ctx); err != nil {
		return err
	}
	l.reconcile(ctx)
	l.refreshLive(ctx)
	l.claimReady(ctx)
	l.replan(ctx)
	recT := time.NewTicker(l.cfg.ReconcileEvery)
	liveT := time.NewTicker(l.cfg.LiveEvery)
	defer recT.Stop()
	defer liveT.Stop()
	defer l.stopSession()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-recT.C:
			l.reconcile(ctx)
		case <-liveT.C:
			l.refreshLive(ctx)
		case <-l.nudge:
			l.reconcile(ctx)
			l.refreshLive(ctx)
		case ev := <-l.sessEvents:
			l.onSession(ctx, ev)
		case pe := <-l.pubsub:
			l.onPubSub(ctx, pe)
		}
		l.claimReady(ctx)
		l.replan(ctx)
	}
}

// load reads stored state, then bridges claim rows written by the v1-era
// mark-collected UI (that handler reloads the scheduler, so this runs).
func (l *Loop) load(ctx context.Context) error {
	rows, err := l.cfg.Store.List(ctx, l.cfg.AccountID)
	if err != nil {
		return fmt.Errorf("load drop state: %w", err)
	}
	for _, r := range rows {
		l.rows[r.DropID] = r
	}
	if l.cfg.History == nil {
		return nil
	}
	ids, err := l.cfg.History.ClaimedBenefitIDs(ctx, l.cfg.AccountID)
	if err != nil {
		slog.Warn("pipeline: read claim history failed", "kind", "error", "account", l.cfg.AccountID, "err", err)
		return nil
	}
	now := l.cfg.Now()
	for id := range ids {
		if r, ok := l.rows[id]; ok && r.Status != dropstate.Claimed {
			next := dropstate.MarkCollected(r, now)
			l.rows[id] = next
			if err := l.cfg.Store.Upsert(ctx, next); err != nil {
				slog.Warn("pipeline: persist bridged mark failed", "kind", "error", "account", l.cfg.AccountID, "drop", id, "err", err)
			}
		}
	}
	return nil
}

func (l *Loop) reconcile(ctx context.Context) {
	res, err := reconcile.Run(ctx, reconcile.Config{
		AccountID: l.cfg.AccountID, Platform: l.cfg.Platform,
		Backend: l.cfg.Backend, Progress: l.prog, Session: l.cfg.Session,
		AllowGame: l.cfg.AllowGame, AllowChannel: l.cfg.AllowChannel,
		ForceLinked: l.cfg.ForceLinked, Persister: l.cfg.Persister,
	}, l.rows, l.cfg.Now())
	if err != nil {
		slog.Warn("pipeline reconcile failed; keeping last state", "kind", "error", "account", l.cfg.AccountID, "err", err)
		return
	}
	l.camps = res.Campaigns
	for id, dp := range res.Progress {
		l.progress[id] = dp
	}
	l.mu.Lock()
	l.discovery, l.discoverAt = res.Campaigns, l.cfg.Now()
	l.mu.Unlock()
	for _, r := range res.Rows {
		l.commit(ctx, r)
	}
}

func (l *Loop) benefit(dropID string) (platform.DropBenefit, platform.Campaign) {
	for _, c := range l.camps {
		for _, b := range c.Benefits {
			if b.ID == dropID {
				return b, c
			}
		}
	}
	return platform.DropBenefit{ID: dropID}, platform.Campaign{}
}

// commit stores a row and handles the transition into claimed.
func (l *Loop) commit(ctx context.Context, next dropstate.Row) {
	prev := l.rows[next.DropID]
	l.rows[next.DropID] = next
	if err := l.cfg.Store.Upsert(ctx, next); err != nil {
		slog.Warn("pipeline: persist drop state failed", "kind", "error", "account", l.cfg.AccountID, "drop", next.DropID, "err", err)
	}
	if next.Status != dropstate.Claimed || prev.Status == dropstate.Claimed {
		return
	}
	b, c := l.benefit(next.DropID)
	if l.cfg.History != nil {
		if _, err := l.cfg.History.RecordClaimIfNew(ctx, l.cfg.AccountID, b); err != nil {
			slog.Warn("pipeline: record claim history failed", "kind", "error", "account", l.cfg.AccountID, "drop", next.DropID, "err", err)
		}
	}
	slog.Info("pipeline drop claimed", "kind", "claim", "account", l.cfg.AccountID, "drop", next.DropID, "source", string(next.Source))
	// Only notify live transitions, not historical claims seen on first sync.
	if !prev.IsZero() && next.Source == dropstate.FromPlatform {
		l.notify(ctx, "claim", c, b, nil)
	}
}

func (l *Loop) notify(ctx context.Context, event string, c platform.Campaign, b platform.DropBenefit, extra map[string]any) {
	if l.cfg.Notifier == nil {
		return
	}
	f := map[string]any{"account": l.cfg.AccountID}
	if l.cfg.AccountLabel != "" {
		f["account_label"] = l.cfg.AccountLabel
	}
	if l.cfg.Platform != "" {
		f["platform"] = l.cfg.Platform
	}
	if c.Game != "" {
		f["game"] = c.Game
	}
	if c.Name != "" {
		f["campaign"] = c.Name
	}
	if b.Name != "" {
		f["drop"] = b.Name
	}
	for k, v := range extra {
		f[k] = v
	}
	_ = l.cfg.Notifier.Notify(ctx, event, f)
}

func (l *Loop) plannerInput() planner.Input {
	return planner.Input{
		Now: l.cfg.Now(), Rows: l.rows, Campaigns: l.camps,
		Settings: planner.Settings{
			GameRank: l.cfg.GameRank, PriorityMode: l.cfg.PriorityMode,
			StreamerPriority: l.cfg.StreamerPriority, ForceWatch: l.forceLive,
		},
		Live: l.live, Cooldowns: l.cooldowns, Current: l.current, LastSwap: l.lastSwap,
	}
}

// allowedPriority returns the priority logins a campaign accepts.
func allowedPriority(c platform.Campaign, prio []string) []string {
	if len(c.AllowedChannels) == 0 {
		return prio
	}
	allowed := map[string]bool{}
	for _, a := range c.AllowedChannels {
		allowed[strings.ToLower(a)] = true
	}
	var out []string
	for _, p := range prio {
		if allowed[strings.ToLower(p)] {
			out = append(out, p)
		}
	}
	return out
}

func mergeStreams(first, rest []platform.Stream) []platform.Stream {
	seen := map[string]bool{}
	var out []platform.Stream
	for _, s := range append(append([]platform.Stream(nil), first...), rest...) {
		k := strings.ToLower(s.Channel)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

func (l *Loop) refreshLive(ctx context.Context) {
	live := map[string][]platform.Stream{}
	for i, c := range planner.Candidates(l.plannerInput()) {
		if i >= maxLiveCampaigns {
			break
		}
		streams, err := l.cfg.Backend.ListEligibleChannels(ctx, l.cfg.Session, c)
		if err != nil {
			slog.Debug("pipeline: list channels failed", "account", l.cfg.AccountID, "campaign", c.ID, "err", err)
		}
		if l.prober != nil {
			if logins := allowedPriority(c, l.cfg.StreamerPriority); len(logins) > 0 {
				if ps, err := l.prober.ProbeChannels(ctx, l.cfg.Session, c, logins); err == nil {
					streams = mergeStreams(ps, streams)
				}
			}
		}
		live[c.ID] = streams
	}
	l.live = live
	l.forceLive = nil
	if l.prober != nil && len(l.cfg.ForceWatch) > 0 {
		if ps, err := l.prober.ProbeChannels(ctx, l.cfg.Session, platform.Campaign{}, l.cfg.ForceWatch); err == nil {
			l.forceLive = ps
		}
	}
}

func (l *Loop) isCurrent(channel string) bool {
	return l.current != nil && l.current.Kind != planner.Idle && strings.EqualFold(l.current.Channel.Channel, channel)
}

func (l *Loop) onSession(ctx context.Context, ev session.Event) {
	now := l.cfg.Now()
	switch ev.Kind {
	case session.Progress:
		l.applyProgress(ctx, ev.Progress)
	case session.StreamDown:
		if l.isCurrent(ev.Channel) {
			l.cooldowns[strings.ToLower(ev.Channel)] = now.Add(l.cfg.DownCooldown)
		}
	case session.Stalled:
		if l.isCurrent(ev.Channel) {
			l.cooldowns[strings.ToLower(ev.Channel)] = now.Add(l.cfg.StallCooldown)
			l.reconcile(ctx)
		}
	}
}

func (l *Loop) applyProgress(ctx context.Context, progs []platform.Progress) {
	now := l.cfg.Now()
	for _, p := range progs {
		prev, ok := l.rows[p.BenefitID]
		if !ok {
			continue // reconcile creates the row first
		}
		if p.InstanceID != "" {
			dp := l.progress[p.BenefitID]
			dp.DropID, dp.CampaignID, dp.InstanceID = p.BenefitID, prev.CampaignID, p.InstanceID
			l.progress[p.BenefitID] = dp
		}
		next := dropstate.Apply(prev, dropstate.Observation{
			Known: true, Claimed: p.Claimed, Minutes: p.MinutesWatched, Required: prev.Required,
		}, now)
		l.maybeNotifyProgress(ctx, next)
		l.commit(ctx, next)
	}
}

func (l *Loop) onPubSub(ctx context.Context, e pubsubEvent) {
	switch e.kind {
	case "progress":
		if r, ok := l.rows[e.drop]; ok && r.Required == 0 && e.required > 0 {
			r.Required = e.required
			l.rows[e.drop] = r
		}
		l.applyProgress(ctx, []platform.Progress{{BenefitID: e.drop, MinutesWatched: e.minutes}})
	case "claimable":
		r, ok := l.rows[e.drop]
		if !ok || r.Status == dropstate.Claimed {
			return
		}
		dp := l.progress[e.drop]
		dp.DropID, dp.CampaignID, dp.InstanceID = e.drop, r.CampaignID, e.instance
		l.progress[e.drop] = dp
		minutes := r.Minutes
		if minutes < r.Required {
			minutes = r.Required
		}
		l.commit(ctx, dropstate.Apply(r, dropstate.Observation{Known: true, Minutes: minutes, Required: r.Required}, l.cfg.Now()))
	case "down":
		if l.current != nil && l.current.Channel.ChannelID != "" && l.current.Channel.ChannelID == e.channel {
			l.cooldowns[strings.ToLower(l.current.Channel.Channel)] = l.cfg.Now().Add(l.cfg.DownCooldown)
		}
	}
}

func (l *Loop) maybeNotifyProgress(ctx context.Context, r dropstate.Row) {
	step := l.cfg.ProgressNotifyStepPct
	if step <= 0 || r.Required <= 0 || l.cfg.Notifier == nil {
		return
	}
	pct := r.Minutes * 100 / r.Required
	if pct > 100 {
		pct = 100
	}
	m := pct / step * step
	last, seen := l.milestones[r.DropID]
	l.milestones[r.DropID] = m
	if !seen || m <= last {
		return // first sight records a baseline; no restart storms
	}
	b, c := l.benefit(r.DropID)
	l.notify(ctx, "progress", c, b, map[string]any{"cur_min": r.Minutes, "req_min": r.Required})
}

func (l *Loop) claimReady(ctx context.Context) {
	now := l.cfg.Now()
	attempted := false
	for id, r := range l.rows {
		if !dropstate.ReadyToClaim(r, now) {
			continue
		}
		dp, ok := l.progress[id]
		if !ok || dp.DropID == "" {
			dp = platform.DropProgress{DropID: id, CampaignID: r.CampaignID}
		}
		next, res := claimer.Attempt(ctx, l.claimer, l.cfg.Session, r, dp, now)
		slog.Info("pipeline claim attempt", "kind", "claim", "account", l.cfg.AccountID, "drop", id,
			"outcome", res.Outcome.String(), "detail", res.Detail, "link", res.LinkURL)
		l.commit(ctx, next)
		attempted = true
	}
	if attempted {
		l.reconcile(ctx) // corrects lost responses and auto-granted rewards
	}
}

func (l *Loop) replan(ctx context.Context) {
	d := planner.Plan(l.plannerInput())
	if l.current != nil && l.current.Same(d) {
		l.current.Serves, l.current.Reason = d.Serves, d.Reason
		l.updateSnapshot()
		return
	}
	l.stopSession()
	l.current, l.lastSwap = &d, l.cfg.Now()
	slog.Info("pipeline decision", "kind", "state", "account", l.cfg.AccountID,
		"decision", d.Kind.String(), "channel", d.Channel.Channel, "serves", len(d.Serves), "reason", d.Reason)
	if l.cfg.Notifier != nil {
		_ = l.cfg.Notifier.Notify(ctx, "state", map[string]any{"account": l.cfg.AccountID, "state": stateString(&d)})
	}
	if d.Kind != planner.Idle {
		l.startSession(ctx, d)
	}
	l.updateSnapshot()
}

func (l *Loop) startSession(ctx context.Context, d planner.Decision) {
	sctx, cancel := context.WithCancel(ctx)
	l.sessCancel = cancel
	if l.subs != nil && d.Channel.ChannelID != "" {
		l.subs.SubscribeChannel(l.cfg.AccountID, d.Channel.ChannelID)
		l.subscribed = d.Channel.ChannelID
	}
	ticker := time.NewTicker(l.cfg.BeatEvery)
	cfg := session.Config{
		Backend: l.cfg.Backend, Session: l.cfg.Session, Stream: d.Channel,
		Serves: d.Serves, Ticks: ticker.C, StallPolls: l.cfg.StallPolls,
	}
	if d.Kind == planner.ForceWatch {
		cfg.StallPolls = 0 // channel-points farming has no drop progress to stall on
	}
	go func() {
		defer ticker.Stop()
		session.Run(sctx, cfg, l.sessEvents)
	}()
}

func (l *Loop) stopSession() {
	if l.sessCancel != nil {
		l.sessCancel()
		l.sessCancel = nil
	}
	if l.subs != nil && l.subscribed != "" {
		l.subs.UnsubscribeChannel(l.cfg.AccountID, l.subscribed)
		l.subscribed = ""
	}
}

// stateString maps a decision onto the v1 dashboard state vocabulary.
func stateString(d *planner.Decision) string {
	if d == nil {
		return "pick_campaign"
	}
	switch d.Kind {
	case planner.Mine:
		return "watching"
	case planner.ForceWatch:
		return "force_watch"
	default:
		return "idle"
	}
}

func (l *Loop) updateSnapshot() {
	s := watcher.Snapshot{AccountID: l.cfg.AccountID, State: stateString(l.current)}
	if d := l.current; d != nil && d.Kind != planner.Idle {
		s.Channel, s.ViewerCount, s.StartedAt = d.Channel.Channel, d.Channel.ViewerCount, l.lastSwap
		if len(d.Serves) > 0 {
			b, c := l.benefit(d.Serves[0])
			r := l.rows[d.Serves[0]]
			s.CampaignID, s.CampaignName, s.CampaignGame = c.ID, c.Name, c.Game
			s.BenefitID, s.BenefitName, s.BenefitImage = b.ID, b.Name, b.ImageURL
			s.MinutesWatched, s.RequiredMinutes = r.Minutes, r.Required
		}
	}
	l.mu.Lock()
	l.snap = s
	l.mu.Unlock()
}
```

- [ ] **Step 3: Run tests with race detector**

Run: `go test ./internal/pipeline/loop/ -race -v`
Expected: PASS, no races. If `TestLoop_StallCoolsChannelAndSwaps` flakes, raise its timeout, not the stall logic.

- [ ] **Step 4: Revert-proof checks**

1. In `onSession`, remove the `l.isCurrent(ev.Channel)` guard for StreamDown: `TestLoop_StaleStreamDownIgnored` must FAIL. Restore.
2. In `claimReady`, remove `l.reconcile(ctx)`: run whole package; confirm tests still pass (reconcile is belt-and-braces there) and note it in the task report.
3. In `commit`, delete the `RecordClaimIfNew` call: `TestLoop_ClaimedOnWebsiteMidWatch` must FAIL. Restore.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/pipeline
git add internal/pipeline/loop
git commit -m "feat(pipeline-v2): per-account loop owning all drop state transitions

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Scheduler interfaces + `GRUB_PIPELINE` wiring

**Files:**
- Modify: `internal/scheduler/state.go:30-62` (use interfaces, not `*watcher.Watcher`)
- Modify: `internal/scheduler/discovery.go:25-45` (same)
- Test: `internal/scheduler/v2_runner_test.go`
- Modify: `internal/store/campaign_persister.go` (add `PipelineOverridePrefix`)
- Modify: `cmd/miner/main.go` (backfill at startup, `dropStore`, v2 branch in `build`, helpers)
- Test: `cmd/miner/main_test.go` (helpers)

**Interfaces:**
- Consumes: `loop.New`, `loop.Config` (Task 11), `store.DropStateStore`, `store.BackfillDropState` (Tasks 3 and 4).
- Produces: `pipelineModeFor(ctx, q, accountID) string`, `matchAnyChannel(logins []string) func([]string) bool` in `cmd/miner`; `store.PipelineOverridePrefix = "pipeline_override:"`.

- [ ] **Step 1: Failing scheduler test**

First read `internal/scheduler/*_test.go` for how entries are added and snapshots read (existing tests use `NewEntry` + `AddEntry`). Then:

```go
package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/watcher"
)

type v2Runner struct{}

func (v2Runner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (v2Runner) Snapshot() watcher.Snapshot {
	return watcher.Snapshot{AccountID: "acc", State: "watching", Channel: "ch1"}
}
func (v2Runner) LastDiscovery() ([]platform.Campaign, time.Time) {
	return []platform.Campaign{{ID: "c1"}}, time.Unix(1, 0)
}
func (v2Runner) AllowGame() func(string) bool { return func(string) bool { return true } }

func TestScheduler_NonWatcherRunnerWithSnapshot(t *testing.T) {
	s := New(Options{})
	s.AddEntry(NewEntry("acc", v2Runner{}))
	snaps := s.WatcherSnapshots()
	assert.Equal(t, "ch1", snaps[0].Channel)
	assert.Equal(t, "watching", s.Snapshot()[0].State, "must not fall back to needs_auth")
	disc := s.WatcherDiscoveries()
	assert.Len(t, disc[0].Campaigns, 1)
}
```

Run: `go test ./internal/scheduler/ -run NonWatcherRunner -v`
Expected: FAIL (`needs_auth` / empty channel).

- [ ] **Step 2: Implement scheduler interfaces**

In `state.go` add:

```go
// snapshotter is any runner that can describe its current work. Both the v1
// *watcher.Watcher and the pipeline v2 loop implement it.
type snapshotter interface {
	Snapshot() watcher.Snapshot
}
```

Replace the body of `Snapshot()`'s loop with:

```go
	for _, e := range s.entries {
		if sn, ok := e.runner.(snapshotter); ok {
			out = append(out, AccountState{AccountID: e.id, State: sn.Snapshot().State})
			continue
		}
		out = append(out, AccountState{AccountID: e.id, State: idleState(e.runner)})
	}
```

and `WatcherSnapshots()`'s loop with:

```go
	for _, e := range s.entries {
		if sn, ok := e.runner.(snapshotter); ok {
			out = append(out, sn.Snapshot())
			continue
		}
		out = append(out, watcher.Snapshot{AccountID: e.id, State: idleState(e.runner)})
	}
```

In `discovery.go` add (import `time` and `platform` as needed):

```go
// discoveryReporter is any runner that caches its last campaign discovery.
type discoveryReporter interface {
	LastDiscovery() ([]platform.Campaign, time.Time)
	AllowGame() func(game string) bool
}
```

and in `WatcherDiscoveries` replace `w, ok := e.runner.(*watcher.Watcher)` with `w, ok := e.runner.(discoveryReporter)`. Remove the `watcher` import from `discovery.go` if now unused.

Run: `go test ./internal/scheduler/ -v`
Expected: PASS (new + existing).

- [ ] **Step 3: Failing main helper tests** (append to `cmd/miner/main_test.go`)

```go
func TestPipelineModeFor(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/t.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	t.Setenv("GRUB_PIPELINE", "")
	assert.Equal(t, "v1", pipelineModeFor(ctx, q, "acc"))
	t.Setenv("GRUB_PIPELINE", "v2")
	assert.Equal(t, "v2", pipelineModeFor(ctx, q, "acc"))
	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: store.PipelineOverridePrefix + "acc", Value: []byte("v1")}))
	assert.Equal(t, "v1", pipelineModeFor(ctx, q, "acc"), "per-account override beats env")
	require.NoError(t, q.UpsertSettingString(ctx, gen.UpsertSettingStringParams{Key: store.PipelineOverridePrefix + "acc", Value: []byte("junk")}))
	assert.Equal(t, "v2", pipelineModeFor(ctx, q, "acc"), "invalid override falls through")
}

func TestMatchAnyChannel(t *testing.T) {
	assert.Nil(t, matchAnyChannel(nil))
	m := matchAnyChannel([]string{"Fav"})
	assert.True(t, m([]string{"x", "fav"}))
	assert.False(t, m([]string{"x"}))
}
```

(add `assert` import if missing.)

Run: `go test ./cmd/miner/ -run 'PipelineModeFor|MatchAnyChannel' -v`
Expected: FAIL, undefined.

- [ ] **Step 4: Implement helpers + wiring**

In `internal/store/campaign_persister.go`, next to the other prefixes:

```go
// PipelineOverridePrefix keys a per-account pipeline choice in kv. Full key:
// PipelineOverridePrefix + accountID, value "v1" or "v2". Beats GRUB_PIPELINE.
const PipelineOverridePrefix = "pipeline_override:"
```

In `cmd/miner/main.go`, add helpers near `loadAccountWhitelist`:

```go
// pipelineModeFor picks v1 or v2 for an account: kv override first, then
// GRUB_PIPELINE, default v1.
func pipelineModeFor(ctx context.Context, q *gen.Queries, accountID string) string {
	if v, err := q.GetSettingString(ctx, store.PipelineOverridePrefix+accountID); err == nil {
		if s := string(v); s == "v1" || s == "v2" {
			return s
		}
	}
	if os.Getenv("GRUB_PIPELINE") == "v2" {
		return "v2"
	}
	return "v1"
}

// matchAnyChannel reports whether any campaign channel is one of logins.
// Nil when logins is empty, so the null-game gate stays off.
func matchAnyChannel(logins []string) func([]string) bool {
	if len(logins) == 0 {
		return nil
	}
	set := make(map[string]bool, len(logins))
	for _, l := range logins {
		set[strings.ToLower(l)] = true
	}
	return func(chs []string) bool {
		for _, c := range chs {
			if set[strings.ToLower(c)] {
				return true
			}
		}
		return false
	}
}
```

After `claimRecorder := store.NewClaimRecorder(q)` (around line 337) add:

```go
	dropStore := store.NewDropStateStore(q)
	if n, err := store.BackfillDropState(ctx, q, time.Now()); err != nil {
		logger.Warn("pipeline v2: drop_state backfill failed", "err", err)
	} else if n > 0 {
		logger.Info("pipeline v2: drop_state backfilled from v1 state", "rows", n)
	}
```

In `build`, immediately before `acctLabel := a.DisplayName` / `w := watcher.New(...)`, insert:

```go
		if pipelineModeFor(ctx, q, a.ID) == "v2" {
			prio, err := dropStore.StreamerPriority(ctx, a.ID)
			if err != nil {
				logger.Warn("pipeline v2: load streamer priority failed", "account", a.ID, "err", err)
			}
			var force []string
			if rows, err := q.ListForceChannels(ctx, a.ID); err == nil {
				for _, r := range rows {
					force = append(force, r.Channel)
				}
			}
			l, err := loop.New(loop.Config{
				AccountID: a.ID, AccountLabel: a.DisplayName, Platform: a.Platform,
				Backend: b, Session: sess,
				Store: dropStore, History: claimRecorder, Persister: campaignPersister,
				Notifier:  notifier,
				AllowGame: allow, AllowChannel: matchAnyChannel(prio), GameRank: rank,
				PriorityMode: priorityMode, ForceLinked: forceLinked,
				StreamerPriority: prio, ForceWatch: force,
				ProgressNotifyStepPct: progressStep,
			})
			if err == nil {
				logger.Info("pipeline v2 enabled for account", "account", a.ID, "platform", a.Platform)
				return scheduler.NewEntry(a.ID, l), nil
			}
			logger.Warn("pipeline v2 unavailable, falling back to v1", "account", a.ID, "err", err)
		}
```

Add import `"github.com/aalejandrofer/grubdrops/internal/pipeline/loop"`. `notifier` must satisfy `loop.Notifier` (same `Notify` signature as `watcher.Notifier`); if the variable's type is a concrete notify type, it already has the method. If `go build` reports `forceCollected` / `persistedSkips` unused only in some path, leave v1 code as is: they are still used by the v1 branch below.

- [ ] **Step 5: Build + full tests**

Run: `gofmt -w . && go build ./... && go test ./... 2>&1 | tail -30`
Expected: all packages `ok`.

- [ ] **Step 6: Local smoke run**

```bash
mkdir -p /tmp/grub-v2 && GRUB_MASTER_KEY=$(go run ./cmd/miner keygen) \
  GRUB_DB_PATH=/tmp/grub-v2/miner.db GRUB_SECURE_COOKIES=0 GRUB_PIPELINE=v2 \
  timeout 20 go run ./cmd/miner 2>&1 | grep -E "pipeline|backfill|error" | head -20
```

Expected: starts without panic. With no accounts, there are no `pipeline v2 enabled` lines, and that's fine. This only proves boot + migration + backfill.

- [ ] **Step 7: Commit**

```bash
git add internal/scheduler internal/store/campaign_persister.go cmd/miner
git commit -m "feat(pipeline-v2): opt-in GRUB_PIPELINE=v2 wiring and scheduler interfaces

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Changelog + spec refinements + whole-branch verification

**Files:**
- Modify: `docs/CHANGELOG.md` (`## [Unreleased]`)
- Modify: `docs/superpowers/specs/2026-09-27-pipeline-v2-design.md`

- [ ] **Step 1: Changelog entries** under `## [Unreleased]`

```markdown
### Added
- Pipeline v2 (opt-in, `GRUB_PIPELINE=v2` or kv `pipeline_override:<account>`): drop state now comes from each campaign's platform details, so drops claimed on the website or with a lost claim response show as claimed and mining moves on (#50).
- Streamer priority for pipeline v2: per-account priority streamers are watched first, then any live channel in the category (#49). Seeded from the existing null-game channel list.
- `drop_state` and `account_streamer_priority` tables (migration 0016), with a one-time backfill from claim history, manual marks and ghost-skips.

### Changed
- Twitch claim code split into status fetch + classification (behaviour unchanged).
- Scheduler reads dashboard snapshots and discoveries through interfaces, so v1 and v2 runners both report state.

### Fixed (pipeline v2 only)
- Claim failures back off 1m, 5m, 30m and stop after 5 attempts instead of looping forever.
- Kick link-required claims block with a reason instead of retrying every poll.
- Kick required minutes come from the platform, not an invented 120.
- Ghost-skipped drops are re-checked instead of being skipped forever.
```

- [ ] **Step 2: Record spec refinements**

In the spec, add a short "## 10. Refinements during planning" section listing the four refinements from this plan's header (serial reconcile, unix INTEGER timestamps, `user_skip` no auto-expiry, interface names).

- [ ] **Step 3: Whole-branch verification**

Run: `gofmt -l . ; go vet ./... && go build ./... && go test -race ./... 2>&1 | tail -40`
Expected: `gofmt -l` prints nothing; all packages `ok`.

- [ ] **Step 4: Commit**

```bash
git add docs/CHANGELOG.md docs/superpowers/specs/2026-09-27-pipeline-v2-design.md
git commit -m "docs(pipeline-v2): changelog and spec refinements

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: Staging live verification (HUMAN-GATED, no tag)

Accrual/claim change: CLAUDE.md requires a live drop before any tag. This task never tags.

- [ ] **Step 1:** Push the branch and ask the user before deploying (staging runbook lives in the user's memory notes: local build, scp tar, `docker compose up` on the staging host; accounts are disabled by default there).
- [ ] **Step 2:** On staging, enable ONE Twitch account and ONE Kick account that have a live drop available, and set `GRUB_PIPELINE=v2` (or kv `pipeline_override:<acct>=v2` via `sqlite3`).
- [ ] **Step 3:** Watch logs for `pipeline decision`, `pipeline claim attempt`, `pipeline drop claimed`. Confirm on the platform's own site that minutes accrue and the claim lands. Confirm a drop claimed manually on the website flips to claimed within one reconcile (15 min) without a watch.
- [ ] **Step 4:** Record results (drop, channel, minutes, claim outcome) in the PR description.
- [ ] **Step 5:** Tear staging down (`docker compose down`) as soon as verification ends.
- [ ] **Step 6:** Hand back to the user for PR review and Plan 2. Do not merge or tag without their say-so.
