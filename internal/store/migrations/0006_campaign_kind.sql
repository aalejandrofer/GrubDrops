-- +goose Up
-- +goose StatementBegin
ALTER TABLE campaigns ADD COLUMN kind TEXT NOT NULL DEFAULT 'drop';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE campaigns DROP COLUMN kind;
-- +goose StatementEnd
