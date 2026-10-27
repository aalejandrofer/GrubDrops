-- name: ListGlobalForceChannels :many
SELECT id, platform, channel, rank, created_at FROM force_channels ORDER BY rank, id;

-- name: ListGlobalForceChannelAccounts :many
SELECT channel_id, account_id FROM force_channel_accounts ORDER BY channel_id, account_id;

-- name: DeleteAllGlobalForceChannels :exec
DELETE FROM force_channels;

-- name: InsertGlobalForceChannel :one
INSERT INTO force_channels (platform, channel, rank, created_at)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: InsertGlobalForceChannelAccount :exec
INSERT INTO force_channel_accounts (channel_id, account_id) VALUES (?, ?);

-- name: ListGlobalForceChannelsForAccount :many
SELECT c.channel FROM force_channels c
JOIN force_channel_accounts ca ON ca.channel_id = c.id
WHERE ca.account_id = ?
ORDER BY c.rank, c.id;
