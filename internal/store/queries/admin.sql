-- name: GetAdmin :one
SELECT * FROM admin WHERE id = 1;

-- name: UpsertAdmin :exec
INSERT INTO admin (id, password_hash, created_at)
VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash;

-- name: AdminExists :one
SELECT EXISTS(SELECT 1 FROM admin WHERE id = 1);
