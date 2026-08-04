package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/authcheck"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

func alertTestQueries(t *testing.T) *gen.Queries {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return gen.New(db)
}

func writeAuthResult(t *testing.T, q *gen.Queries, accountID string, ok bool) {
	t.Helper()
	b, err := json.Marshal(authcheck.Result{OK: ok, CheckedAt: time.Now().Unix(), Msg: "test"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := q.UpsertSettingString(context.Background(), gen.UpsertSettingStringParams{
		Key:   authcheck.Prefix + accountID,
		Value: b,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

// An account that is idle (not "needs_auth" by scheduler state) but whose
// persisted auth check FAILED must still raise a dashboard alert. This is the
// gap: the dashboard read green while the account was dead.
func TestAuthAlertAccounts_FlagsFailedCheck(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)
	writeAuthResult(t, q, "acc_dead", false)
	writeAuthResult(t, q, "acc_live", true)

	cards := []dashMineCard{
		{ID: "acc_dead", Name: "dead", State: "sleeping"},
		{ID: "acc_live", Name: "live", State: "watching"},
	}
	got := authAlertAccounts(ctx, q, cards)

	if !got["acc_dead"] {
		t.Fatal("account with a failed auth check should be flagged")
	}
	if got["acc_live"] {
		t.Fatal("account with a passing auth check must not be flagged")
	}
}

// An account with no recorded check yet must NOT be flagged — a fresh install
// would otherwise show a wall of false alerts before the first sweep runs.
func TestAuthAlertAccounts_NoResultIsNotFlagged(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)

	cards := []dashMineCard{{ID: "acc_new", Name: "new", State: "sleeping"}}
	if got := authAlertAccounts(ctx, q, cards); got["acc_new"] {
		t.Fatal("account with no auth-check result must not be flagged")
	}
}

// When BOTH signals fire for one account — the scheduler independently wants
// a "sleeping"/no_drops alert AND the persisted auth check failed — the
// operator needs one row, not two. This is the mixed-signal case: the card's
// State on its own would produce a no_drops alert via the switch, so the
// test only bites if the needs_auth `continue` actually short-circuits it.
func TestBuildAlerts_NoDuplicateForBothSignals(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)
	writeAuthResult(t, q, "acc_dead", false)

	cards := []dashMineCard{{ID: "acc_dead", Name: "dead", Platform: "twitch", State: "sleeping", Enabled: true}}
	alerts := buildDashAlerts(ctx, q, cards, "en")

	var total int
	for _, a := range alerts {
		if a.Account != "dead" {
			continue
		}
		total++
		if a.Kind != "needs_auth" {
			t.Fatalf("expected needs_auth alert for dead account, got kind %q", a.Kind)
		}
	}
	if total != 1 {
		t.Fatalf("expected exactly 1 alert for dead account, got %d", total)
	}
	for _, a := range alerts {
		if a.Account == "dead" && a.Kind == "no_drops" {
			t.Fatal("no_drops alert must not also fire for an account whose auth check failed")
		}
	}
}

// A disabled account with a stale persisted auth-check failure must raise NO
// alert. authcheck.CheckAll only sweeps ListEnabledAccounts, so once an
// operator disables a dead account its authcheck:<id> result is frozen and
// can never self-heal — without this guard the banner would be permanent
// (unclearable short of re-enabling and re-logging in), which teaches the
// operator to ignore the whole banner feature.
func TestBuildAlerts_DisabledAccountNoAlert(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)
	writeAuthResult(t, q, "acc_disabled", false)

	cards := []dashMineCard{
		{ID: "acc_disabled", Name: "disabled", Platform: "kick", State: "stopped", Enabled: false},
	}
	alerts := buildDashAlerts(ctx, q, cards, "en")

	for _, a := range alerts {
		if a.Account == "disabled" {
			t.Fatalf("disabled account must not raise an alert, got %+v", a)
		}
	}
}

// The pre-existing alert kinds must survive the refactor.
func TestBuildAlerts_KeepsOtherKinds(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)

	cards := []dashMineCard{
		{ID: "acc_t", Name: "t", Platform: "twitch", State: "sleeping", Enabled: true},
		{ID: "acc_c", Name: "c", State: "awaiting_connect", Enabled: true},
		{ID: "acc_g", Name: "g", State: "no_games", Enabled: true},
	}
	alerts := buildDashAlerts(ctx, q, cards, "en")

	kinds := map[string]bool{}
	for _, a := range alerts {
		kinds[a.Kind] = true
	}
	for _, want := range []string{"no_drops", "awaiting_connect", "no_games"} {
		if !kinds[want] {
			t.Fatalf("alert kind %q went missing after the refactor", want)
		}
	}
}
