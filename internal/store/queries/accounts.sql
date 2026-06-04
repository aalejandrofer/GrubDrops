-- name: CreateAccount :one
INSERT INTO accounts (id, platform, login, display_name, status, proxy_url, webhook_url, fingerprint_json, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ?;

-- name: ListEnabledAccounts :many
SELECT * FROM accounts WHERE enabled = 1 ORDER BY created_at ASC;

-- name: UpdateAccountStatus :exec
UPDATE accounts SET status = ?, updated_at = ? WHERE id = ?;

-- name: SetAccountEnabled :exec
UPDATE accounts SET enabled = ?, updated_at = ? WHERE id = ?;
