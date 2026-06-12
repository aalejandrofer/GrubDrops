-- name: UpdateAccountDisplayName :exec
UPDATE accounts SET display_name = ?, updated_at = ? WHERE id = ?;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

-- The explicit per-account child deletes below back a transactional account
-- purge in the delete handler. The schema declares ON DELETE CASCADE on every
-- account child, but cascade only fires when foreign_keys is enforced on the
-- live connection; deleting the children first makes the purge correct even if
-- enforcement is ever off. Run them before DeleteAccount in one transaction.

-- name: DeleteAccountSession :exec
DELETE FROM sessions WHERE account_id = ?;

-- name: DeleteAccountCampaignLinks :exec
DELETE FROM account_campaign_links WHERE account_id = ?;

-- name: DeleteAccountCampaignPriorities :exec
DELETE FROM campaign_priorities WHERE account_id = ?;

-- name: DeleteAccountProgress :exec
DELETE FROM progress WHERE account_id = ?;

-- name: DeleteAccountClaims :exec
DELETE FROM claims WHERE account_id = ?;

-- name: ListAllAccounts :many
SELECT * FROM accounts ORDER BY created_at ASC;

-- name: UpdateAccountWebhook :exec
UPDATE accounts SET webhook_url = ?, updated_at = ? WHERE id = ?;
