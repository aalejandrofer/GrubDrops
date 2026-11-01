-- +goose Up
-- Cached Twitch channel-points balance per account and channel login.
-- fetched_at is the last successful read, checked_at the last attempt.
-- error is empty after a success, failed after a failed read and
-- unavailable when the channel has no balance. failures counts failed reads
-- in a row and sets the retry backoff.
CREATE TABLE points_balances (
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel    TEXT NOT NULL,
    balance    INTEGER,
    fetched_at INTEGER,
    checked_at INTEGER NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    failures   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, channel)
);

-- +goose Down
DROP TABLE IF EXISTS points_balances;
