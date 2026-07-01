-- +goose Up
-- +goose StatementBegin
-- Drop the dormant per-account proxy_url column. It has existed since the
-- initial schema but was never wired: nothing reads it into any transport and
-- there is no UI to set it. Proxy support is global-only; removing the dead
-- column avoids implying a per-account feature that does not exist.
ALTER TABLE accounts DROP COLUMN proxy_url;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts ADD COLUMN proxy_url TEXT;
-- +goose StatementEnd
