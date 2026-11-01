-- +goose Up
-- How the account treats channel points: off, drops_points (Force watch
-- channels when no drop is live, claiming the Twitch channel-points bonus
-- while watching) or points_only. Every account starts as off, including
-- accounts already on a Force watch channel; their assignments stay.
ALTER TABLE accounts ADD COLUMN points_mode TEXT NOT NULL DEFAULT 'off';

-- +goose Down
ALTER TABLE accounts DROP COLUMN points_mode;
