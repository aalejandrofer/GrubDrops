-- +goose Up
-- +goose StatementBegin
CREATE TABLE force_channels (
    id         INTEGER PRIMARY KEY,
    platform   TEXT NOT NULL,
    channel    TEXT NOT NULL,
    rank       INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE(platform, channel)
);

CREATE TABLE force_channel_accounts (
    channel_id INTEGER NOT NULL REFERENCES force_channels(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    PRIMARY KEY (channel_id, account_id)
);

CREATE INDEX idx_force_channel_accounts_acct ON force_channel_accounts(account_id);

-- Carry over 1.x per-account force-watch lists for accounts whose flag is on.
-- The 1.x table and kv rows stay in place. Channel names the store would
-- reject are skipped: Kick allows a-z 0-9 _ -, Twitch a-z 0-9 _, both 2 to 25.
INSERT OR IGNORE INTO force_channels (platform, channel, rank, created_at)
SELECT platform, channel, ROW_NUMBER() OVER (ORDER BY min_rank, min_created, platform, channel) - 1, min_created
FROM (
    SELECT a.platform AS platform, f.channel AS channel,
           MIN(f.rank) AS min_rank, MIN(f.created_at) AS min_created
    FROM account_force_channels f
    JOIN accounts a ON a.id = f.account_id
    JOIN kv ON kv.key = 'force_watch:' || a.id AND CAST(kv.value AS TEXT) = '1'
    WHERE length(f.channel) BETWEEN 2 AND 25
      AND f.channel NOT GLOB '*[^a-z0-9_-]*'
      AND (a.platform = 'kick' OR f.channel NOT GLOB '*-*')
    GROUP BY a.platform, f.channel
);

INSERT OR IGNORE INTO force_channel_accounts (channel_id, account_id)
SELECT c.id, a.id
FROM account_force_channels f
JOIN accounts a ON a.id = f.account_id
JOIN kv ON kv.key = 'force_watch:' || a.id AND CAST(kv.value AS TEXT) = '1'
JOIN force_channels c ON c.platform = a.platform AND c.channel = f.channel;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS force_channel_accounts;
DROP TABLE IF EXISTS force_channels;
-- +goose StatementEnd
