-- +goose Up
-- +goose StatementBegin
CREATE TABLE accounts (
    id              TEXT PRIMARY KEY,
    platform        TEXT NOT NULL,
    login           TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'idle',
    proxy_url       TEXT,
    webhook_url     TEXT,
    fingerprint_json TEXT NOT NULL DEFAULT '{}',
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(platform, login)
);

CREATE TABLE sessions (
    account_id  TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    ciphertext  BLOB NOT NULL,
    expires_at  INTEGER NOT NULL
);

CREATE TABLE campaigns (
    id              TEXT PRIMARY KEY,
    platform        TEXT NOT NULL,
    game            TEXT NOT NULL,
    name            TEXT NOT NULL,
    starts_at       INTEGER NOT NULL,
    ends_at         INTEGER NOT NULL,
    status          TEXT NOT NULL,
    raw_json        TEXT NOT NULL DEFAULT '{}',
    discovered_at   INTEGER NOT NULL
);

CREATE TABLE benefits (
    id                TEXT PRIMARY KEY,
    campaign_id       TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    required_minutes  INTEGER NOT NULL,
    image_url         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE progress (
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    benefit_id       TEXT NOT NULL REFERENCES benefits(id) ON DELETE CASCADE,
    minutes_watched  INTEGER NOT NULL DEFAULT 0,
    claimed_at       INTEGER,
    updated_at       INTEGER NOT NULL,
    PRIMARY KEY (account_id, benefit_id)
);

CREATE TABLE claims (
    id               TEXT PRIMARY KEY,
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    benefit_id       TEXT NOT NULL REFERENCES benefits(id) ON DELETE CASCADE,
    claimed_at       INTEGER NOT NULL,
    value_meta_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE campaign_priorities (
    account_id   TEXT REFERENCES accounts(id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    rank         INTEGER NOT NULL,
    PRIMARY KEY (account_id, campaign_id)
);

CREATE TABLE games (
    id        TEXT PRIMARY KEY,
    name      TEXT NOT NULL UNIQUE,
    slug      TEXT NOT NULL UNIQUE,
    priority  INTEGER NOT NULL DEFAULT 100
);

CREATE TABLE notifications (
    id           TEXT PRIMARY KEY,
    account_id   TEXT REFERENCES accounts(id) ON DELETE SET NULL,
    kind         TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    created_at   INTEGER NOT NULL,
    sent_at      INTEGER
);

CREATE TABLE logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    ts           INTEGER NOT NULL,
    level        TEXT NOT NULL,
    account_id   TEXT,
    msg          TEXT NOT NULL,
    fields_json  TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_logs_ts ON logs(ts);

CREATE TABLE kv (
    key    TEXT PRIMARY KEY,
    value  BLOB NOT NULL
);

CREATE TABLE admin (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);

INSERT INTO games (id, name, slug, priority) VALUES ('g_rust', 'Rust', 'rust', 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE admin;
DROP TABLE kv;
DROP TABLE logs;
DROP TABLE notifications;
DROP TABLE games;
DROP TABLE campaign_priorities;
DROP TABLE claims;
DROP TABLE progress;
DROP TABLE benefits;
DROP TABLE campaigns;
DROP TABLE sessions;
DROP TABLE accounts;
-- +goose StatementEnd
