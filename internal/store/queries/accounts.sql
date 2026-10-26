-- name: CreateAccount :one
INSERT INTO accounts (id, platform, display_name, status, webhook_url, fingerprint_json, enabled, created_at, updated_at, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM accounts))
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ?;

-- name: ListEnabledAccounts :many
SELECT * FROM accounts WHERE enabled = 1 ORDER BY sort_order ASC, created_at ASC;

-- name: UpdateAccountStatus :exec
UPDATE accounts SET status = ?, updated_at = ? WHERE id = ?;

-- name: SetAccountEnabled :exec
UPDATE accounts SET enabled = ?, updated_at = ? WHERE id = ?;
