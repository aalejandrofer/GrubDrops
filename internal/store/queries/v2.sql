-- name: UpsertClaimLog :exec
-- Keeps the first claimed_at and source. A non-empty code replaces the stored
-- one, an empty code keeps it. Other text fields refresh when non-empty.
INSERT INTO claim_log (account_id, benefit_id, platform, game, campaign_id, campaign_name, benefit_name, image_url, code, source, claimed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, benefit_id) DO UPDATE SET
    platform      = CASE WHEN excluded.platform <> '' THEN excluded.platform ELSE claim_log.platform END,
    game          = CASE WHEN excluded.game <> '' THEN excluded.game ELSE claim_log.game END,
    campaign_id   = CASE WHEN excluded.campaign_id <> '' THEN excluded.campaign_id ELSE claim_log.campaign_id END,
    campaign_name = CASE WHEN excluded.campaign_name <> '' THEN excluded.campaign_name ELSE claim_log.campaign_name END,
    benefit_name  = CASE WHEN excluded.benefit_name <> '' THEN excluded.benefit_name ELSE claim_log.benefit_name END,
    image_url     = CASE WHEN excluded.image_url <> '' THEN excluded.image_url ELSE claim_log.image_url END,
    code          = CASE WHEN excluded.code <> '' THEN excluded.code ELSE claim_log.code END;

-- name: ListClaimLogBenefitIDs :many
SELECT benefit_id FROM claim_log WHERE account_id = ?;

-- name: ListManualClaimLogBenefitIDs :many
-- Claims the user marked collected by hand.
SELECT benefit_id FROM claim_log WHERE account_id = ? AND source = 'manual';

-- name: ListRecentClaimLog :many
SELECT * FROM claim_log ORDER BY claimed_at DESC, benefit_id ASC LIMIT ?;

-- name: CountClaimLogSince :one
SELECT COUNT(*) FROM claim_log WHERE claimed_at >= ?;

-- name: DeleteDropStatesForAccount :exec
DELETE FROM drop_states WHERE account_id = ?;

-- name: UpsertDropState :exec
INSERT INTO drop_states (account_id, benefit_id, platform, game, campaign_id, campaign_name, benefit_name, image_url, state, reason, minutes, required_minutes, ends_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, benefit_id) DO UPDATE SET
    platform         = excluded.platform,
    game             = excluded.game,
    campaign_id      = excluded.campaign_id,
    campaign_name    = excluded.campaign_name,
    benefit_name     = excluded.benefit_name,
    image_url        = excluded.image_url,
    state            = excluded.state,
    reason           = excluded.reason,
    minutes          = excluded.minutes,
    required_minutes = excluded.required_minutes,
    ends_at          = excluded.ends_at,
    updated_at       = excluded.updated_at;

-- name: ListDropStatesForAccount :many
SELECT * FROM drop_states WHERE account_id = ?
ORDER BY game ASC, campaign_id ASC, required_minutes ASC, benefit_id ASC;

-- name: ListAllDropStates :many
SELECT * FROM drop_states
ORDER BY account_id ASC, game ASC, campaign_id ASC, required_minutes ASC, benefit_id ASC;

-- name: AddWatchMinutes :exec
INSERT INTO watch_minutes (account_id, day, minutes) VALUES (?, ?, ?)
ON CONFLICT(account_id, day) DO UPDATE SET minutes = watch_minutes.minutes + excluded.minutes;

-- name: SumAllWatchMinutes :one
SELECT CAST(COALESCE(SUM(minutes), 0) AS INTEGER) FROM watch_minutes;

-- name: SumWatchMinutesDay :one
SELECT CAST(COALESCE(SUM(minutes), 0) AS INTEGER) FROM watch_minutes WHERE day = ?;

-- name: PutAccountScan :exec
-- A zero timestamp keeps the stored value. The error is always written.
INSERT INTO account_scans (account_id, scanned_at, reconciled_at, error)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET
    scanned_at    = CASE WHEN excluded.scanned_at <> 0 THEN excluded.scanned_at ELSE account_scans.scanned_at END,
    reconciled_at = CASE WHEN excluded.reconciled_at <> 0 THEN excluded.reconciled_at ELSE account_scans.reconciled_at END,
    error         = excluded.error;

-- name: ListAccountScans :many
SELECT * FROM account_scans ORDER BY account_id ASC;

-- name: ListChosenGames :many
SELECT g.id, g.name, g.slug, g.twitch_game_id
FROM chosen_games cg
JOIN games g ON g.id = cg.game_id
ORDER BY cg.rank ASC;

-- name: ClearChosenGames :exec
DELETE FROM chosen_games;

-- name: AddChosenGame :exec
INSERT INTO chosen_games (game_id, rank) VALUES (?, ?);

-- name: ListClaimLogPage :many
-- An empty game, account_id or pattern matches any. The pattern is a LIKE pattern with bang as the escape char, applied through the like function.
SELECT * FROM claim_log
WHERE (sqlc.arg(game) = '' OR game = sqlc.arg(game))
  AND (sqlc.arg(account_id) = '' OR account_id = sqlc.arg(account_id))
  AND (sqlc.arg(pattern) = ''
    OR like(sqlc.arg(pattern), benefit_name, '!')
    OR like(sqlc.arg(pattern), campaign_name, '!')
    OR like(sqlc.arg(pattern), game, '!'))
ORDER BY claimed_at DESC, benefit_id ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountClaimLogFiltered :one
SELECT COUNT(*) FROM claim_log
WHERE (sqlc.arg(game) = '' OR game = sqlc.arg(game))
  AND (sqlc.arg(account_id) = '' OR account_id = sqlc.arg(account_id))
  AND (sqlc.arg(pattern) = ''
    OR like(sqlc.arg(pattern), benefit_name, '!')
    OR like(sqlc.arg(pattern), campaign_name, '!')
    OR like(sqlc.arg(pattern), game, '!'));

-- name: ListClaimLogGames :many
SELECT DISTINCT game FROM claim_log WHERE game <> '' ORDER BY game ASC;

-- name: ListClaimLogForCampaign :many
-- Every claim of one campaign on one platform, oldest first.
SELECT * FROM claim_log WHERE platform = ? AND campaign_id = ?
ORDER BY claimed_at ASC, account_id ASC, benefit_id ASC;

-- name: GetClaimLog :one
SELECT * FROM claim_log WHERE account_id = ? AND benefit_id = ?;

-- name: DeleteClaimLog :execrows
DELETE FROM claim_log WHERE account_id = ? AND benefit_id = ?;
