-- name: ListAccountChannels :many
SELECT channel, rank
FROM account_channels
WHERE account_id = ?
ORDER BY rank ASC, channel ASC;

-- name: AddAccountChannel :exec
INSERT INTO account_channels (account_id, channel, rank)
VALUES (?, ?, ?)
ON CONFLICT(account_id, channel) DO UPDATE SET rank = excluded.rank;

-- name: RemoveAccountChannel :exec
DELETE FROM account_channels WHERE account_id = ? AND channel = ?;

-- name: ClearAccountChannels :exec
DELETE FROM account_channels WHERE account_id = ?;

-- name: ListAllAccountChannels :many
SELECT account_id, channel FROM account_channels;
