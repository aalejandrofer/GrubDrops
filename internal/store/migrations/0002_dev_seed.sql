-- +goose Up
-- +goose StatementBegin
-- Historically inserted acc_fake_dev. Now a no-op — 0003 drops the
-- row on existing DBs and the FakeBackend has been removed from the
-- runtime. Kept as a placeholder so the migration version stays
-- monotonic for installations that already ran 0002 against the
-- previous content.
SELECT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
