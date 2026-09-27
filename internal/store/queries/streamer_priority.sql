-- name: ListStreamerPriority :many
SELECT login, rank FROM account_streamer_priority
WHERE account_id = ?
ORDER BY rank ASC, login ASC;
