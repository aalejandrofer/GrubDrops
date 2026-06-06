-- +goose Up
-- +goose StatementBegin
-- account_linked: 1 when the account can earn this campaign (external account
-- connected, or no link required). 0 = whitelisted but the required account
-- isn't linked, so the watcher skips it and /drops lists it separately.
-- Default 1 so pre-existing rows + platforms that don't gate stay mineable.
ALTER TABLE campaigns ADD COLUMN account_linked INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE campaigns ADD COLUMN account_link_url TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE campaigns DROP COLUMN account_linked;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE campaigns DROP COLUMN account_link_url;
-- +goose StatementEnd
