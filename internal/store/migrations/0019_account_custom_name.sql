-- +goose Up
-- User-set account name. Empty means none: the UI shows display_name, the
-- platform login. Kept apart from display_name so login code never clobbers it.
ALTER TABLE accounts ADD COLUMN custom_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE accounts DROP COLUMN custom_name;
