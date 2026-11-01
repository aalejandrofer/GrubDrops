-- name: ListPointsBalances :many
SELECT account_id, channel, balance, fetched_at, checked_at, error, failures
FROM points_balances
ORDER BY account_id, channel;

-- name: GetPointsBalance :one
SELECT account_id, channel, balance, fetched_at, checked_at, error, failures
FROM points_balances
WHERE account_id = ? AND channel = ?;

-- name: UpsertPointsBalance :exec
-- A successful read: new balance, error and failures cleared.
INSERT INTO points_balances (account_id, channel, balance, fetched_at, checked_at, error, failures)
VALUES (?, ?, ?, ?, ?, '', 0)
ON CONFLICT(account_id, channel) DO UPDATE SET
    balance = excluded.balance,
    fetched_at = excluded.fetched_at,
    checked_at = excluded.checked_at,
    error = '',
    failures = 0;

-- name: UpsertPointsBalanceError :exec
-- A read without a balance: the last good balance and fetched_at stay.
INSERT INTO points_balances (account_id, channel, balance, fetched_at, checked_at, error, failures)
VALUES (?, ?, NULL, NULL, ?, ?, ?)
ON CONFLICT(account_id, channel) DO UPDATE SET
    checked_at = excluded.checked_at,
    error = excluded.error,
    failures = excluded.failures;

-- name: PrunePointsBalances :execrows
-- Drops rows whose account is no longer assigned to that Twitch Force watch channel.
DELETE FROM points_balances
WHERE NOT EXISTS (
    SELECT 1 FROM force_channel_accounts ca
    JOIN force_channels c ON c.id = ca.channel_id
    WHERE ca.account_id = points_balances.account_id
      AND c.channel = points_balances.channel
      AND c.platform = 'twitch'
);
