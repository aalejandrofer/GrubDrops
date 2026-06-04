-- +goose Up
-- +goose StatementBegin
INSERT INTO accounts (id, platform, login, display_name, status, proxy_url, webhook_url, fingerprint_json, enabled, created_at, updated_at)
SELECT 'acc_fake_dev', 'fake', 'devuser', 'Dev User', 'idle', NULL, NULL, '{}', 1, strftime('%s','now'), strftime('%s','now')
WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE id = 'acc_fake_dev');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM accounts WHERE id = 'acc_fake_dev';
-- +goose StatementEnd
