-- +goose Up
-- +goose StatementBegin
-- v2 collection tables. claim_log and watch_minutes carry no account FK on
-- purpose: history outlives a removed account. Times are Unix seconds.
CREATE TABLE IF NOT EXISTS chosen_games (
    game_id TEXT PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    rank    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS claim_log (
    account_id    TEXT NOT NULL,
    benefit_id    TEXT NOT NULL,
    platform      TEXT NOT NULL,
    game          TEXT NOT NULL DEFAULT '',
    campaign_id   TEXT NOT NULL DEFAULT '',
    campaign_name TEXT NOT NULL DEFAULT '',
    benefit_name  TEXT NOT NULL DEFAULT '',
    image_url     TEXT NOT NULL DEFAULT '',
    code          TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL,
    claimed_at    INTEGER NOT NULL,
    PRIMARY KEY (account_id, benefit_id)
);
CREATE INDEX IF NOT EXISTS idx_claim_log_claimed_at ON claim_log(claimed_at);
CREATE TABLE IF NOT EXISTS drop_states (
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    benefit_id       TEXT NOT NULL,
    platform         TEXT NOT NULL,
    game             TEXT NOT NULL,
    campaign_id      TEXT NOT NULL,
    campaign_name    TEXT NOT NULL DEFAULT '',
    benefit_name     TEXT NOT NULL DEFAULT '',
    image_url        TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    minutes          INTEGER NOT NULL DEFAULT 0,
    required_minutes INTEGER NOT NULL DEFAULT 0,
    ends_at          INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL,
    PRIMARY KEY (account_id, benefit_id)
);
CREATE TABLE IF NOT EXISTS watch_minutes (
    account_id TEXT NOT NULL,
    day        TEXT NOT NULL,
    minutes    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, day)
);
CREATE TABLE IF NOT EXISTS account_scans (
    account_id    TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    scanned_at    INTEGER NOT NULL DEFAULT 0,
    reconciled_at INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS account_scans;
DROP TABLE IF EXISTS watch_minutes;
DROP TABLE IF EXISTS drop_states;
DROP INDEX IF EXISTS idx_claim_log_claimed_at;
DROP TABLE IF EXISTS claim_log;
DROP TABLE IF EXISTS chosen_games;
-- +goose StatementEnd
