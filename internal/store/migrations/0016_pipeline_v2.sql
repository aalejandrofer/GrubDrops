-- +goose Up
-- +goose StatementBegin
-- Pipeline v2: one authoritative mining state row per account and drop.
-- Timestamps are unix seconds, 0 means unset.
CREATE TABLE drop_state (
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    drop_id       TEXT NOT NULL,
    campaign_id   TEXT NOT NULL,
    platform      TEXT NOT NULL,
    status        TEXT NOT NULL,
    block_reason  TEXT NOT NULL DEFAULT '',
    minutes       INTEGER NOT NULL DEFAULT 0,
    required      INTEGER NOT NULL DEFAULT 0,
    source        TEXT NOT NULL DEFAULT 'platform',
    fail_count    INTEGER NOT NULL DEFAULT 0,
    retry_after   INTEGER NOT NULL DEFAULT 0,
    synced_at     INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (account_id, drop_id)
);
CREATE INDEX idx_drop_state_status ON drop_state(account_id, status);

-- Ordered per-account priority streamers. Seeded from account_channels,
-- the null-game channel whitelist, which is the same concept.
CREATE TABLE account_streamer_priority (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform    TEXT NOT NULL,
    login       TEXT NOT NULL,
    rank        INTEGER NOT NULL,
    PRIMARY KEY (account_id, login)
);
CREATE INDEX idx_account_streamer_priority_acct ON account_streamer_priority(account_id, rank);

INSERT INTO account_streamer_priority (account_id, platform, login, rank)
SELECT ac.account_id, a.platform, ac.channel, ac.rank
FROM account_channels ac
JOIN accounts a ON a.id = ac.account_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_streamer_priority_acct;
DROP TABLE IF EXISTS account_streamer_priority;
DROP INDEX IF EXISTS idx_drop_state_status;
DROP TABLE IF EXISTS drop_state;
-- +goose StatementEnd
