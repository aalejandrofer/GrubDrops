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

// When BOTH signals fire for one account, the operator needs one row, not two.
func TestBuildAlerts_NoDuplicateForBothSignals(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)
	writeAuthResult(t, q, "acc_dead", false)

	cards := []dashMineCard{{ID: "acc_dead", Name: "dead", State: "needs_auth"}}
	alerts := buildDashAlerts(ctx, q, cards, "en")

	var n int
	for _, a := range alerts {
		if a.Kind == "needs_auth" && a.Account == "dead" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 needs_auth alert, got %d", n)
	}
}

// The pre-existing alert kinds must survive the refactor.
func TestBuildAlerts_KeepsOtherKinds(t *testing.T) {
	ctx := context.Background()
	q := alertTestQueries(t)

	cards := []dashMineCard{
		{ID: "acc_t", Name: "t", Platform: "twitch", State: "sleeping"},
		{ID: "acc_c", Name: "c", State: "awaiting_connect"},
		{ID: "acc_g", Name: "g", State: "no_games"},
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
