-- name: CreateJob :one
INSERT INTO jobs (id, kind, site_id, status, args, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ?;

-- name: ListJobs :many
SELECT * FROM jobs ORDER BY created_at DESC LIMIT ?;

-- name: ListJobsBySite :many
SELECT * FROM jobs WHERE site_id = ? ORDER BY created_at DESC LIMIT ?;

-- name: ListActiveJobsBySite :many
SELECT * FROM jobs WHERE site_id = ? AND status IN ('queued', 'running') ORDER BY created_at;

-- name: ListJobsByStatus :many
SELECT * FROM jobs WHERE status = ? ORDER BY created_at;

-- name: StartJob :exec
UPDATE jobs SET status = 'running', started_at = ?, updated_at = ? WHERE id = ?;

-- name: FinishJob :exec
UPDATE jobs SET status = ?, error = ?, finished_at = ?, updated_at = ? WHERE id = ?;

-- name: FailUnfinishedJobs :execrows
UPDATE jobs SET status = 'failed', error = ?, finished_at = ?, updated_at = ?
WHERE status IN ('queued', 'running');

-- name: CreateJobEvent :one
INSERT INTO job_events (job_id, ts, level, step, message)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListJobEvents :many
SELECT * FROM job_events WHERE job_id = ? ORDER BY id;

-- name: DeleteJobsBefore :execrows
DELETE FROM jobs WHERE status IN ('succeeded', 'failed') AND finished_at < ?;

-- name: ListActiveJobs :many
SELECT * FROM jobs WHERE status IN ('queued', 'running') ORDER BY created_at;

-- name: GetActiveJobByKind :one
SELECT * FROM jobs WHERE kind = ? AND status IN ('queued', 'running') ORDER BY created_at LIMIT 1;
