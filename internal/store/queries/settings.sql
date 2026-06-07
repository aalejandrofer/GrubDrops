-- name: GetSettingString :one
SELECT value FROM kv WHERE key = ?;

-- name: UpsertSettingString :exec
INSERT INTO kv (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- name: ListKVByPrefix :many
SELECT key, value FROM kv WHERE key LIKE ?1 || '%';

-- name: DeleteKV :exec
DELETE FROM kv WHERE key = ?;
