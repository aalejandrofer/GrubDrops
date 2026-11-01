-- +goose Up
-- Twitch reward codes an account received, read from its notifications.
-- reward_key is Twitch's own id for the reward and dedupes reads per account.
-- code is age-encrypted with the master key; NULL when the reward has no code.
-- expires_at and received_at are NULL when Twitch did not say.
-- notified_at and reminded_at record the new-code and expiry notifications,
-- so each is sent once across restarts. redeemed_at and dismissed_at are set
-- from the UI and hide the row.
CREATE TABLE reward_codes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    reward_key   TEXT NOT NULL,
    name         TEXT NOT NULL,
    game         TEXT NOT NULL DEFAULT '',
    code         BLOB,
    redeem_url   TEXT NOT NULL DEFAULT '',
    expires_at   INTEGER,
    received_at  INTEGER,
    first_seen   INTEGER NOT NULL,
    notified_at  INTEGER,
    reminded_at  INTEGER,
    redeemed_at  INTEGER,
    dismissed_at INTEGER,
    UNIQUE (account_id, reward_key)
);

-- +goose Down
DROP TABLE IF EXISTS reward_codes;
