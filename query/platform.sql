-- name: GetKV :one
SELECT value FROM platform_kv WHERE key = ?;

-- name: SetKV :exec
INSERT INTO platform_kv (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: DeleteKV :exec
DELETE FROM platform_kv WHERE key = ?;

-- name: CreateActivity :one
INSERT INTO activity (id, ts, user_id, site_id, kind, message, meta)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListActivity :many
SELECT * FROM activity ORDER BY ts DESC LIMIT ?;

-- name: ListActivityBySite :many
SELECT * FROM activity WHERE site_id = ? ORDER BY ts DESC LIMIT ?;
