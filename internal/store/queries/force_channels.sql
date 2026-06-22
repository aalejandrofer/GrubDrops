-- name: ListForceChannels :many
SELECT channel, rank
FROM account_force_channels
WHERE account_id = ?
ORDER BY rank ASC, channel ASC;

-- name: AddForceChannel :exec
INSERT INTO account_force_channels (account_id, channel, rank, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id, channel) DO UPDATE SET rank = excluded.rank;

-- name: RemoveForceChannel :exec
DELETE FROM account_force_channels WHERE account_id = ? AND channel = ?;

-- name: ClearForceChannels :exec
DELETE FROM account_force_channels WHERE account_id = ?;
