-- name: ListAllGames :many
SELECT id, name, slug, priority FROM games ORDER BY name;

-- name: UpsertGame :exec
INSERT INTO games (id, name, slug, priority)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, slug = excluded.slug;

-- name: ListAccountGames :many
SELECT g.id, g.name, g.slug, ag.rank
FROM account_games ag
JOIN games g ON g.id = ag.game_id
WHERE ag.account_id = ?
ORDER BY ag.rank ASC;

-- name: AddAccountGame :exec
INSERT INTO account_games (account_id, game_id, rank)
VALUES (?, ?, ?)
ON CONFLICT(account_id, game_id) DO UPDATE SET rank = excluded.rank;

-- name: RemoveAccountGame :exec
DELETE FROM account_games WHERE account_id = ? AND game_id = ?;

-- name: ClearAccountGames :exec
DELETE FROM account_games WHERE account_id = ?;

-- name: ListGlobalGames :many
SELECT g.id, g.name, g.slug, gg.rank
FROM global_games gg
JOIN games g ON g.id = gg.game_id
ORDER BY gg.rank ASC;

-- name: AddGlobalGame :exec
INSERT INTO global_games (game_id, rank)
VALUES (?, ?)
ON CONFLICT(game_id) DO UPDATE SET rank = excluded.rank;

-- name: ClearGlobalGames :exec
DELETE FROM global_games;
