-- name: CreateSite :one
INSERT INTO sites (
    id, slug, name, status, status_message, primary_domain, tls_mode, php_version, image,
    profile, file_mods, page_cache, db_name, db_user, db_password_enc, redis_user,
    redis_password_enc, salts_enc, purge_token_hash, login_secret_enc, admin_user,
    admin_email, settings, container_id, volume_name, created_at, updated_at,
    runtime, wp_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
UPDATE sites SET name = sqlc.arg(name), php_version = sqlc.arg(php_version), image = sqlc.arg(image), runtime = sqlc.arg(runtime), page_cache = sqlc.arg(page_cache), file_mods = sqlc.arg(file_mods),
    tls_mode = sqlc.arg(tls_mode), profile = sqlc.arg(profile), settings = sqlc.arg(settings), config_revision = config_revision + sqlc.arg(revision_increment), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetSiteImage :exec
UPDATE sites SET config_revision = config_revision + CASE WHEN image <> sqlc.arg(image) OR runtime <> sqlc.arg(runtime) OR php_version <> sqlc.arg(php_version) THEN 1 ELSE 0 END, image = sqlc.arg(image), runtime = sqlc.arg(runtime), php_version = sqlc.arg(php_version), updated_at = sqlc.arg(updated_at) WHERE id = sqlc.arg(id);

-- name: SetSiteWPVersion :exec
UPDATE sites SET wp_version = ?, updated_at = ? WHERE id = ?;

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

-- name: SetSiteTraffic :exec
UPDATE sites SET traffic_ready = ?, updated_at = ? WHERE id = ?;

-- name: SetSiteDesiredState :exec
UPDATE sites SET desired_state = ?, updated_at = ? WHERE id = ?;

-- name: InvalidateSiteApplication :exec
UPDATE sites SET applied_revision = 0, traffic_ready = 0, updated_at = ? WHERE id = ?;

-- name: CompleteSiteApplication :exec
UPDATE sites SET applied_revision = config_revision, applied_image = image, updated_at = ? WHERE id = ?;

-- name: RestoreSiteApplication :exec
UPDATE sites SET applied_revision = ?, applied_image = ?, traffic_ready = 0, updated_at = ? WHERE id = ?;

-- name: InterruptSiteApplications :exec
UPDATE sites SET traffic_ready = 0, applied_revision = 0, status = 'error',
 status_message = 'application interrupted by engine restart; retry explicitly',
 desired_state = CASE WHEN EXISTS (SELECT 1 FROM jobs WHERE jobs.site_id = sites.id AND jobs.kind = 'site.stop' AND jobs.status IN ('queued','running')) THEN 'stopped' ELSE desired_state END
WHERE id IN (SELECT site_id FROM jobs WHERE status IN ('queued','running') AND kind IN ('site.provision','site.start','site.stop','site.restart','site.recreate','site.delete'));
