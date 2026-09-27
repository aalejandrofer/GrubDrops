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
