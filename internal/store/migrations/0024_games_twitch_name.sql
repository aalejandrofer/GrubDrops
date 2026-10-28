-- +goose Up
-- twitch_name is the name Twitch uses for the game, which can differ from
-- the stored name when Twitch renames a game but Kick keeps the old one.
ALTER TABLE games ADD COLUMN twitch_name TEXT NOT NULL DEFAULT '';
-- Twitch and Kick both call the seeded Overwatch 2 row Overwatch now.
UPDATE games SET name = 'Overwatch', slug = 'overwatch'
WHERE id = 'g_overwatch' AND name = 'Overwatch 2'
  AND NOT EXISTS (SELECT 1 FROM games WHERE slug = 'overwatch');

-- +goose Down
ALTER TABLE games DROP COLUMN twitch_name;
