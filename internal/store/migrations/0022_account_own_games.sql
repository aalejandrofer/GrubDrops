-- +goose Up
-- 1 = the account mines its own ranked list in account_games instead of the
-- global chosen list. Off for every existing account, so rows left from 1.x
-- stay unused until the user turns the list on.
ALTER TABLE accounts ADD COLUMN own_games INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE accounts DROP COLUMN own_games;
