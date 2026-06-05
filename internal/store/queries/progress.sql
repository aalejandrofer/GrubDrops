-- name: UpsertProgress :exec
INSERT INTO progress (account_id, benefit_id, minutes_watched, claimed_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(account_id, benefit_id) DO UPDATE SET
    minutes_watched = excluded.minutes_watched,
    claimed_at = COALESCE(excluded.claimed_at, progress.claimed_at),
    updated_at = excluded.updated_at;

-- name: GetProgress :one
SELECT * FROM progress WHERE account_id = ? AND benefit_id = ?;

-- name: ListUnclaimedProgressForAccount :many
SELECT p.* FROM progress p
JOIN benefits b ON b.id = p.benefit_id
JOIN campaigns c ON c.id = b.campaign_id
WHERE p.account_id = ?
  AND p.claimed_at IS NULL
  AND c.status = 'active'
  AND c.starts_at <= ?
  AND c.ends_at >= ?;

-- name: InsertClaim :exec
INSERT INTO claims (id, account_id, benefit_id, claimed_at, value_meta_json)
VALUES (?, ?, ?, ?, ?);

-- name: CountClaimedForCampaign :one
-- Distinct benefits already claimed by any account in this campaign.
-- The dashboard divides this by len(Benefits) to render the
-- "Claimed X / Y" badge on each Active Campaigns row.
SELECT COUNT(DISTINCT c.benefit_id) FROM claims c
JOIN benefits b ON b.id = c.benefit_id
WHERE b.campaign_id = ?;
