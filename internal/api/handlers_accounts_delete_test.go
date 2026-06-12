package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// TestPurgeAccount_RemovesAccountAndChildren is the regression guard for the
// "deleted account still loads on every boot" bug: deleting an account must
// remove the accounts row (so the boot load query ListEnabledAccounts never
// returns it again) AND every row that belongs to it — session, games,
// campaign links/priorities, progress, claims.
func TestPurgeAccount_RemovesAccountAndChildren(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)

	const id = "acc_0c99deadbeef"
	const now = int64(1)

	// Seed the account plus one row in every account-scoped child table.
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	mustExec(`INSERT INTO accounts (id, platform, display_name, status, fingerprint_json, enabled, created_at, updated_at)
		VALUES (?, 'twitch', 'Test', 'idle', '{}', 1, ?, ?)`, id, now, now)
	mustExec(`INSERT INTO sessions (account_id, ciphertext, expires_at) VALUES (?, x'00', 9999)`, id)
	mustExec(`INSERT INTO account_games (account_id, game_id, rank) VALUES (?, 'g_rust', 1)`, id)
	mustExec(`INSERT INTO campaigns (id, platform, game, name, starts_at, ends_at, status, discovered_at)
		VALUES ('camp1', 'twitch', 'Rust', 'C', 0, 9999, 'active', 0)`)
	mustExec(`INSERT INTO benefits (id, campaign_id, name, required_minutes) VALUES ('ben1', 'camp1', 'B', 60)`)
	mustExec(`INSERT INTO account_campaign_links (account_id, campaign_id, updated_at) VALUES (?, 'camp1', ?)`, id, now)
	mustExec(`INSERT INTO campaign_priorities (account_id, campaign_id, rank) VALUES (?, 'camp1', 1)`, id)
	mustExec(`INSERT INTO progress (account_id, benefit_id, minutes_watched, updated_at) VALUES (?, 'ben1', 10, ?)`, id, now)
	mustExec(`INSERT INTO claims (id, account_id, benefit_id, claimed_at) VALUES ('claim1', ?, 'ben1', ?)`, id, now)

	// Sanity: the account is loaded by the boot query before deletion.
	enabled, err := q.ListEnabledAccounts(ctx)
	if err != nil {
		t.Fatalf("ListEnabledAccounts: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != id {
		t.Fatalf("expected seeded account to be loaded, got %+v", enabled)
	}

	d := accountsDeps{q: q, db: db}
	if err := d.purgeAccount(ctx, id); err != nil {
		t.Fatalf("purgeAccount: %v", err)
	}

	// The account row is gone — the boot loader can never schedule it again.
	enabled, err = q.ListEnabledAccounts(ctx)
	if err != nil {
		t.Fatalf("ListEnabledAccounts after purge: %v", err)
	}
	if len(enabled) != 0 {
		t.Fatalf("deleted account still returned by load query: %+v", enabled)
	}
	if _, err := q.GetAccount(ctx, id); err != sql.ErrNoRows {
		t.Fatalf("GetAccount after purge: want sql.ErrNoRows, got %v", err)
	}

	// Every account-scoped child row is gone too.
	for _, tbl := range []string{"sessions", "account_games", "account_campaign_links", "campaign_priorities", "progress", "claims"} {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+tbl+" WHERE account_id = ?", id).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("%s: %d orphan row(s) survived account deletion", tbl, n)
		}
	}
}
