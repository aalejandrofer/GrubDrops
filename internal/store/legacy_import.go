package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

const legacyImportKey = "v2.legacy_import_done"

// ImportLegacy copies 1.x games, claims and minutes into the v2 tables once.
// It runs in one transaction and records legacyImportKey on commit, so later
// boots skip it. chosen_games is only filled while it is empty.
func ImportLegacy(ctx context.Context, db *sql.DB) error {
	if err := importLegacy(ctx, db); err != nil {
		return fmt.Errorf("1.x import: %w", err)
	}
	return nil
}

func importLegacy(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var done int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM kv WHERE key = ?`, legacyImportKey).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}

	var chosen int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM chosen_games`).Scan(&chosen); err != nil {
		return err
	}
	var games int64
	if chosen == 0 {
		res, err := tx.ExecContext(ctx, `
INSERT INTO chosen_games (game_id, rank)
SELECT game_id, ROW_NUMBER() OVER (ORDER BY grp, r, name) - 1
FROM (
    SELECT g.id AS game_id, 0 AS grp, gg.rank AS r, g.name AS name
    FROM global_games gg JOIN games g ON g.id = gg.game_id
    UNION ALL
    SELECT g.id, 1, MIN(ag.rank), g.name
    FROM account_games ag JOIN games g ON g.id = ag.game_id
    WHERE ag.game_id NOT IN (SELECT game_id FROM global_games)
    GROUP BY g.id
)`)
		if err != nil {
			return fmt.Errorf("chosen games: %w", err)
		}
		games, _ = res.RowsAffected()
	}

	res, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO claim_log
    (account_id, benefit_id, platform, game, campaign_id, campaign_name, benefit_name, image_url, code, source, claimed_at)
SELECT c.account_id, c.benefit_id,
    COALESCE(ca.platform, a.platform, ''),
    COALESCE(ca.game, ''), COALESCE(ca.id, ''), COALESCE(ca.name, ''),
    COALESCE(b.name, ''), COALESCE(b.image_url, ''),
    COALESCE(CASE WHEN json_valid(c.value_meta_json) THEN json_extract(c.value_meta_json, '$.code') END, ''),
    ?, c.claimed_at
FROM claims c
LEFT JOIN benefits b ON b.id = c.benefit_id
LEFT JOIN campaigns ca ON ca.id = b.campaign_id
LEFT JOIN accounts a ON a.id = c.account_id`, ClaimSourceLegacy)
	if err != nil {
		return fmt.Errorf("claims: %w", err)
	}
	claims, _ := res.RowsAffected()

	res, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO watch_minutes (account_id, day, minutes)
SELECT account_id, '', SUM(minutes_watched) FROM progress
GROUP BY account_id HAVING SUM(minutes_watched) > 0`)
	if err != nil {
		return fmt.Errorf("minutes: %w", err)
	}
	minutes, _ := res.RowsAffected()

	if _, err := tx.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES (?, '1')`, legacyImportKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("store 1.x import done", "kind", "state", "games", games, "claims", claims, "minutes", minutes)
	return nil
}
