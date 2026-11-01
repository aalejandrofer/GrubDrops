-- +goose Up
-- notify_skipped_at records a new-code notice that never went out: no
-- notification target took the event, or the code was first seen already
-- expired. Such a code is not sent later.
ALTER TABLE reward_codes ADD COLUMN notify_skipped_at INTEGER;

-- +goose Down
ALTER TABLE reward_codes DROP COLUMN notify_skipped_at;
