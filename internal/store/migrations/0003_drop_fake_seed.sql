-- +goose Up
-- +goose StatementBegin
DELETE FROM accounts WHERE id = 'acc_fake_dev';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
