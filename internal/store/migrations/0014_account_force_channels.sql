-- +goose Up
-- +goose StatementBegin
-- Permanent per-account "force-watch" channels: watched 24/7 when the
-- account is idle (channel-points mining). Distinct from account_channels,
-- which are temporary drop channels tied to a null-game campaign.
CREATE TABLE account_force_channels (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL,
    rank        INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (account_id, channel)
);

CREATE INDEX idx_account_force_channels_acct ON account_force_channels(account_id, rank);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_force_channels_acct;
DROP TABLE IF EXISTS account_force_channels;
-- +goose StatementEnd
