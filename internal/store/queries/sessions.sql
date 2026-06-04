-- name: UpsertSession :exec
INSERT INTO sessions (account_id, ciphertext, expires_at)
VALUES (?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET
    ciphertext = excluded.ciphertext,
    expires_at = excluded.expires_at;

-- name: GetSession :one
SELECT * FROM sessions WHERE account_id = ?;
