-- +goose Up
ALTER TABLE games ADD COLUMN twitch_game_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE games DROP COLUMN twitch_game_id;
