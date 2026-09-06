-- name: CreateUser :one
INSERT INTO users (id, email, name, role, password_hash, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower(?);

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at;

-- name: CountAdmins :one
SELECT count(*) FROM users WHERE role = 'admin';

-- name: SetUserRole :one
UPDATE users SET role = ?, updated_at = ? WHERE id = ?
RETURNING *;

-- name: SetUserPasswordHash :exec
UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?;

-- name: DeleteUserByID :execrows
DELETE FROM users WHERE id = ?;
