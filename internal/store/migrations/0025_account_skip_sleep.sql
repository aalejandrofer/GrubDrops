-- +goose Up
-- 1 = the account ignores the global sleep window and keeps mining through
-- it. Off for every existing account.
ALTER TABLE accounts ADD COLUMN skip_sleep INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE accounts DROP COLUMN skip_sleep;
