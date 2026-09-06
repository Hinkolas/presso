-- name: CreateSession :one
INSERT INTO sessions (id, user_id, token_hash, expires_at, ip_address, user_agent, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSessionAndUserByTokenHash :one
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = ?;

-- name: ExtendSession :exec
UPDATE sessions SET expires_at = ?, updated_at = ? WHERE id = ?;

-- name: DeleteSessionByTokenHash :execrows
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteSessionByID :execrows
DELETE FROM sessions WHERE id = ? AND user_id = ?;

-- name: ListSessionsByUser :many
SELECT * FROM sessions
WHERE user_id = ? AND expires_at > ?
ORDER BY created_at DESC;

-- name: DeleteSessionsByUser :execrows
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= ?;
