-- +goose Up
-- +goose StatementBegin
CREATE TABLE global_games (
    game_id  TEXT NOT NULL PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    rank     INTEGER NOT NULL
);

CREATE INDEX idx_global_games_rank ON global_games(rank);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_global_games_rank;
DROP TABLE IF EXISTS global_games;
-- +goose StatementEnd
