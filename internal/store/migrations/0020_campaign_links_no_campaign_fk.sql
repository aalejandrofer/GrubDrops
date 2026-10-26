-- +goose Up
-- +goose StatementBegin
-- v2 keeps campaigns in memory and never writes the campaigns table, so the
-- campaign_id foreign key rejected every link row. Rebuild without it; the
-- account cascade stays.
CREATE TABLE account_campaign_links_new (
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL,
    linked       INTEGER NOT NULL DEFAULT 1,
    checked      INTEGER NOT NULL DEFAULT 0,
    link_url     TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (account_id, campaign_id)
);
INSERT INTO account_campaign_links_new SELECT account_id, campaign_id, linked, checked, link_url, updated_at FROM account_campaign_links;
DROP TABLE account_campaign_links;
ALTER TABLE account_campaign_links_new RENAME TO account_campaign_links;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE account_campaign_links_old (
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    linked       INTEGER NOT NULL DEFAULT 1,
    checked      INTEGER NOT NULL DEFAULT 0,
    link_url     TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (account_id, campaign_id)
);
INSERT INTO account_campaign_links_old SELECT l.account_id, l.campaign_id, l.linked, l.checked, l.link_url, l.updated_at
FROM account_campaign_links l JOIN campaigns c ON c.id = l.campaign_id;
DROP TABLE account_campaign_links;
ALTER TABLE account_campaign_links_old RENAME TO account_campaign_links;
-- +goose StatementEnd
