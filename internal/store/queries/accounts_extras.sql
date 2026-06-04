-- name: UpdateAccountDisplayName :exec
UPDATE accounts SET display_name = ?, updated_at = ? WHERE id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

-- name: GetAccountByPlatformLogin :one
SELECT * FROM accounts WHERE platform = ? AND login = ?;
