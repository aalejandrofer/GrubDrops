-- +goose Up
-- +goose StatementBegin
CREATE TABLE account_channels (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL,
    rank        INTEGER NOT NULL,
    PRIMARY KEY (account_id, channel)
);

CREATE INDEX idx_account_channels_acct ON account_channels(account_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_channels_acct;
DROP TABLE IF EXISTS account_channels;
-- +goose StatementEnd
