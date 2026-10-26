-- +goose Up
-- User-set account order. Lower comes first; new accounts take max + 1. Backfilled
-- from the creation order the list used until now.
ALTER TABLE accounts ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
UPDATE accounts SET sort_order = (
    SELECT COUNT(*) FROM accounts b
    WHERE b.created_at < accounts.created_at
       OR (b.created_at = accounts.created_at AND b.rowid <= accounts.rowid)
);

-- +goose Down
ALTER TABLE accounts DROP COLUMN sort_order;
