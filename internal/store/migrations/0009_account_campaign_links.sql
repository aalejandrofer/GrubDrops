-- +goose Up
-- +goose StatementBegin
-- Per-account link state for a campaign. The campaigns.account_linked column
-- is single-valued (last writer wins across accounts); with multiple accounts
-- on one platform we need to know WHICH accounts have connected the required
-- external account. linked=0 + checked=1 means "this account must connect".
CREATE TABLE account_campaign_links (
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    linked       INTEGER NOT NULL DEFAULT 1,
    checked      INTEGER NOT NULL DEFAULT 0,
    link_url     TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (account_id, campaign_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE account_campaign_links;
-- +goose StatementEnd
