-- +goose Up
-- +goose StatementBegin
INSERT OR IGNORE INTO games (id, name, slug, priority) VALUES
    ('g_minecraft',  'Minecraft',          'minecraft',          50),
    ('g_pubg',       'PUBG: BATTLEGROUNDS','pubg',               100),
    ('g_overwatch',  'Overwatch 2',        'overwatch-2',        100),
    ('g_diablo4',    'Diablo IV',          'diablo-iv',          100),
    ('g_hellet',     'Helldivers 2',       'helldivers-2',       100),
    ('g_warframe',   'Warframe',           'warframe',           100),
    ('g_eft',        'Escape from Tarkov', 'escape-from-tarkov', 100),
    ('g_eldenring',  'ELDEN RING',         'elden-ring',         100),
    ('g_warzone',    'Call of Duty: Warzone', 'call-of-duty-warzone', 100),
    ('g_palworld',   'Palworld',           'palworld',           100),
    ('g_throneliberty', 'THRONE AND LIBERTY', 'throne-and-liberty', 100),
    ('g_xdefiant',   'XDefiant',           'xdefiant',           100),
    ('g_brawlhalla', 'Brawlhalla',         'brawlhalla',         100),
    ('g_marvelrivals', 'Marvel Rivals',    'marvel-rivals',      100);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
