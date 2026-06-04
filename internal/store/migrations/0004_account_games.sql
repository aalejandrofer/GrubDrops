-- +goose Up
-- +goose StatementBegin
CREATE TABLE account_games (
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    game_id     TEXT NOT NULL REFERENCES games(id)    ON DELETE CASCADE,
    rank        INTEGER NOT NULL,
    PRIMARY KEY (account_id, game_id)
);

CREATE INDEX idx_account_games_rank ON account_games(account_id, rank);

-- Seed common games so the operator has something to pick from on
-- day one. The games table grows automatically as backends discover
-- new campaigns.
INSERT OR IGNORE INTO games (id, name, slug, priority) VALUES
    ('g_marathon',  'Marathon',          'marathon',           100),
    ('g_apex',      'Apex Legends',      'apex-legends',       100),
    ('g_cs2',       'Counter-Strike 2',  'counter-strike-2',   100),
    ('g_valorant',  'Valorant',          'valorant',           100),
    ('g_fortnite',  'Fortnite',          'fortnite',           100),
    ('g_wow',       'World of Warcraft', 'world-of-warcraft',  100),
    ('g_lol',       'League of Legends', 'league-of-legends',  100),
    ('g_dota2',     'Dota 2',            'dota-2',             100),
    ('g_dbd',       'Dead by Daylight',  'dead-by-daylight',   100),
    ('g_escape',    'Escape from Tarkov','escape-from-tarkov', 100);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_games_rank;
DROP TABLE IF EXISTS account_games;
-- +goose StatementEnd
