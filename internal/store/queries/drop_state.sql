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
