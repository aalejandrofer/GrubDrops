-- name: UpdateAccountDisplayName :exec
UPDATE accounts SET display_name = ?, updated_at = ? WHERE id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

-- name: GetAccountByPlatformLogin :one
SELECT * FROM accounts WHERE platform = ? AND login = ?;

-- name: ListAllAccounts :many
SELECT * FROM accounts ORDER BY created_at ASC;

-- name: UpdateAccountWebhook :exec
UPDATE accounts SET webhook_url = ?, updated_at = ? WHERE id = ?;
