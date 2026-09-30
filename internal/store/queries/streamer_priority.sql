-- name: ListStreamerPriority :many
SELECT login, rank FROM account_streamer_priority
WHERE account_id = ?
ORDER BY rank ASC, login ASC;

-- name: AddStreamerPriority :exec
INSERT INTO account_streamer_priority (account_id, platform, login, rank)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id, login) DO UPDATE SET rank = excluded.rank, platform = excluded.platform;

-- name: RemoveStreamerPriority :exec
DELETE FROM account_streamer_priority WHERE account_id = ? AND login = ?;
