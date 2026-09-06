-- name: CreateSite :one
INSERT INTO sites (
    id, slug, name, status, status_message, primary_domain, tls_mode, php_version, image,
    profile, file_mods, page_cache, db_name, db_user, db_password_enc, redis_user,
    redis_password_enc, salts_enc, purge_token_hash, login_secret_enc, admin_user,
    admin_email, settings, container_id, volume_name, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSiteByID :one
SELECT * FROM sites WHERE id = ?;

-- name: GetSiteBySlug :one
SELECT * FROM sites WHERE slug = ?;

-- name: ListSites :many
SELECT * FROM sites ORDER BY created_at DESC;

-- name: ListSitesByStatus :many
SELECT * FROM sites WHERE status = ? ORDER BY created_at;

-- name: CountSitesByStatus :many
SELECT status, count(*) AS n FROM sites GROUP BY status;

-- name: SetSiteStatus :exec
UPDATE sites SET status = ?, status_message = ?, updated_at = ? WHERE id = ?;

-- name: SetSiteContainerID :exec
UPDATE sites SET container_id = ?, updated_at = ? WHERE id = ?;

-- name: UpdateSiteConfig :one
UPDATE sites SET name = ?, php_version = ?, image = ?, page_cache = ?, file_mods = ?,
    tls_mode = ?, profile = ?, settings = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteSite :execrows
DELETE FROM sites WHERE id = ?;

-- name: CreateSiteDomain :one
INSERT INTO site_domains (id, site_id, host, kind, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSiteDomains :many
SELECT * FROM site_domains WHERE site_id = ? ORDER BY kind, host;

-- name: ListAllDomains :many
SELECT * FROM site_domains ORDER BY site_id, kind, host;

-- name: GetSiteDomainByHost :one
SELECT * FROM site_domains WHERE lower(host) = lower(?);

-- name: DeleteSiteDomain :execrows
DELETE FROM site_domains WHERE id = ? AND site_id = ? AND kind != 'primary';
