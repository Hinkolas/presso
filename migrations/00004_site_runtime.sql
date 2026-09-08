-- +goose Up

-- Which execution layer a site runs (internal/compat: frankenphp for PHP >=
-- 8.2, legacy = nginx + PHP-FPM for 7.4-8.1) and which WordPress release its
-- image carries. Rows created before this migration have an empty
-- wp_version until the engine reads `wp core version` from the running
-- container (site.ReconcileAll / the start and recreate jobs).
ALTER TABLE sites ADD COLUMN runtime    TEXT NOT NULL DEFAULT 'frankenphp'; -- frankenphp|legacy
ALTER TABLE sites ADD COLUMN wp_version TEXT NOT NULL DEFAULT '';           -- e.g. 6.8.8
UPDATE sites SET runtime = 'legacy' WHERE php_version IN ('7.4', '8.0', '8.1');

-- +goose Down
ALTER TABLE sites DROP COLUMN wp_version;
ALTER TABLE sites DROP COLUMN runtime;
