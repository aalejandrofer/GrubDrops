-- name: UpsertRewardCode :exec
-- A read of one reward. Empty text and NULL values keep what is stored,
-- so a later read without the code or expiry never erases them.
INSERT INTO reward_codes (account_id, reward_key, name, game, code, redeem_url, expires_at, received_at, first_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, reward_key) DO UPDATE SET
    name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE reward_codes.name END,
    game = CASE WHEN excluded.game <> '' THEN excluded.game ELSE reward_codes.game END,
    code = COALESCE(excluded.code, reward_codes.code),
    redeem_url = CASE WHEN excluded.redeem_url <> '' THEN excluded.redeem_url ELSE reward_codes.redeem_url END,
    expires_at = COALESCE(excluded.expires_at, reward_codes.expires_at),
    received_at = COALESCE(excluded.received_at, reward_codes.received_at);

-- name: GetRewardCodeByKey :one
SELECT * FROM reward_codes
WHERE account_id = ? AND reward_key = ?;

-- name: GetRewardCode :one
SELECT * FROM reward_codes
WHERE id = ?;

-- name: ListRewardCodes :many
SELECT * FROM reward_codes
ORDER BY first_seen DESC, id DESC;

-- name: ListOpenRewardCodes :many
-- Rewards neither redeemed nor dismissed.
SELECT * FROM reward_codes
WHERE redeemed_at IS NULL AND dismissed_at IS NULL
ORDER BY first_seen DESC, id DESC;

-- name: MarkRewardCodeNotified :exec
UPDATE reward_codes SET notified_at = ? WHERE id = ?;

-- name: MarkRewardCodeNotifySkipped :exec
UPDATE reward_codes SET notify_skipped_at = ? WHERE id = ?;

-- name: MarkRewardCodeReminded :exec
UPDATE reward_codes SET reminded_at = ? WHERE id = ?;

-- name: SetRewardCodeRedeemed :execrows
UPDATE reward_codes SET redeemed_at = ? WHERE id = ?;

-- name: SetRewardCodeDismissed :execrows
UPDATE reward_codes SET dismissed_at = ? WHERE id = ?;
