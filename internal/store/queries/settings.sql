-- name: GetSettingString :one
SELECT value FROM kv WHERE key = ?;

-- name: UpsertSettingString :exec
INSERT INTO kv (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
