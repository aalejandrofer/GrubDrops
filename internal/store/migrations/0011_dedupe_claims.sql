-- +goose Up
-- +goose StatementBegin
-- claims had no uniqueness on (account_id, benefit_id), so every re-claim
-- and repeated PubSub claim event inserted another row. That dup-stacked the
-- /drops COLLECTED marks (one chip per row), the Past list (one row per
-- claim), and /history. Collapse to one row per account+benefit (keep the
-- earliest), then enforce it with a unique index so future writes upsert.
DELETE FROM claims
WHERE rowid NOT IN (
    SELECT MIN(rowid) FROM claims GROUP BY account_id, benefit_id
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_claims_account_benefit ON claims(account_id, benefit_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_claims_account_benefit;
-- +goose StatementEnd
