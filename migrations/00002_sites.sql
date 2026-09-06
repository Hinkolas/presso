-- +goose Up

-- Sites: one row per WordPress site the engine runs. Secrets are stored
-- encrypted with internal/secrets (AES-GCM keyed from AUTH_SECRET); the
-- purge token is stored hashed like session tokens because the engine only
-- ever needs to verify it.
CREATE TABLE sites (
    id                 TEXT     PRIMARY KEY,
    slug               TEXT     NOT NULL,
    name               TEXT     NOT NULL,
    status             TEXT     NOT NULL,              -- provisioning|running|stopped|error|deleting (internal/site)
    status_message     TEXT     NOT NULL DEFAULT '',
    primary_domain     TEXT     NOT NULL,
    tls_mode           TEXT     NOT NULL,              -- auto|off
    php_version        TEXT     NOT NULL,              -- e.g. 8.3
    image              TEXT     NOT NULL,              -- runtime image ref the container runs
    profile            TEXT     NOT NULL DEFAULT 'prod',   -- prod|dev
    file_mods          TEXT     NOT NULL DEFAULT 'locked', -- locked|open
    page_cache         BOOLEAN  NOT NULL DEFAULT 1,
    db_name            TEXT     NOT NULL,
    db_user            TEXT     NOT NULL,
    db_password_enc    BLOB     NOT NULL,
    redis_user         TEXT     NOT NULL,
    redis_password_enc BLOB     NOT NULL,
    salts_enc          BLOB     NOT NULL,              -- JSON object of the 8 WordPress salts
    purge_token_hash   BLOB     NOT NULL,              -- sha256 of the system plugin's purge bearer token
    login_secret_enc   BLOB     NOT NULL,              -- HMAC key for one-click login tokens
    admin_user         TEXT     NOT NULL,              -- WordPress admin login created at install
    admin_email        TEXT     NOT NULL,
    settings           TEXT     NOT NULL DEFAULT '{}', -- JSON, see site.Settings
    container_id       TEXT     NOT NULL DEFAULT '',
    volume_name        TEXT     NOT NULL,
    created_at         DATETIME NOT NULL,
    updated_at         DATETIME NOT NULL
);
CREATE UNIQUE INDEX sites_slug_key ON sites (slug);

-- Hostnames routed to a site at the edge. Exactly one row per site has
-- kind='primary' and mirrors sites.primary_domain.
CREATE TABLE site_domains (
    id         TEXT     PRIMARY KEY,
    site_id    TEXT     NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    host       TEXT     NOT NULL,
    kind       TEXT     NOT NULL,                     -- primary|alias
    created_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX site_domains_host_key ON site_domains (lower(host));
CREATE INDEX site_domains_site_id_idx ON site_domains (site_id);

-- Background jobs (internal/jobs). site_id is nullable so job history
-- survives site deletion.
CREATE TABLE jobs (
    id          TEXT     PRIMARY KEY,
    kind        TEXT     NOT NULL,                    -- e.g. site.provision
    site_id     TEXT     REFERENCES sites(id) ON DELETE SET NULL,
    status      TEXT     NOT NULL,                    -- queued|running|succeeded|failed
    args        TEXT     NOT NULL DEFAULT '{}',       -- JSON
    error       TEXT     NOT NULL DEFAULT '',
    created_by  TEXT     NOT NULL DEFAULT '',         -- user id, or 'system'
    started_at  DATETIME,
    finished_at DATETIME,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
CREATE INDEX jobs_site_id_idx ON jobs (site_id, created_at);
CREATE INDEX jobs_status_idx ON jobs (status);

CREATE TABLE job_events (
    id      INTEGER  PRIMARY KEY AUTOINCREMENT,
    job_id  TEXT     NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    ts      DATETIME NOT NULL,
    level   TEXT     NOT NULL,                        -- info|warn|error|step
    step    TEXT     NOT NULL DEFAULT '',
    message TEXT     NOT NULL
);
CREATE INDEX job_events_job_id_idx ON job_events (job_id, id);

-- Small engine-wide key/value state: encrypted platform secrets (MariaDB root
-- password, Redis default password), last edge apply result, cursors.
CREATE TABLE platform_kv (
    key        TEXT     PRIMARY KEY,
    value      BLOB     NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Audit trail of engine operations, shown per site and globally.
CREATE TABLE activity (
    id      TEXT     PRIMARY KEY,
    ts      DATETIME NOT NULL,
    user_id TEXT,                                     -- NULL for system actors
    site_id TEXT     REFERENCES sites(id) ON DELETE SET NULL,
    kind    TEXT     NOT NULL,                        -- e.g. site.created, wp.exec
    message TEXT     NOT NULL,
    meta    TEXT     NOT NULL DEFAULT '{}'            -- JSON
);
CREATE INDEX activity_site_id_idx ON activity (site_id, ts);
CREATE INDEX activity_ts_idx ON activity (ts);

-- Per-minute request rollups from the edge access log (internal/observe).
CREATE TABLE site_request_metrics (
    site_id      TEXT     NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    bucket       DATETIME NOT NULL,                   -- minute start, UTC
    requests     INTEGER  NOT NULL DEFAULT 0,
    cache_hit    INTEGER  NOT NULL DEFAULT 0,
    cache_miss   INTEGER  NOT NULL DEFAULT 0,
    cache_bypass INTEGER  NOT NULL DEFAULT 0,
    cache_stale  INTEGER  NOT NULL DEFAULT 0,
    status_2xx   INTEGER  NOT NULL DEFAULT 0,
    status_3xx   INTEGER  NOT NULL DEFAULT 0,
    status_4xx   INTEGER  NOT NULL DEFAULT 0,
    status_5xx   INTEGER  NOT NULL DEFAULT 0,
    bytes        INTEGER  NOT NULL DEFAULT 0,
    dur_sum_ms   REAL     NOT NULL DEFAULT 0,
    dur_p50_ms   REAL     NOT NULL DEFAULT 0,
    dur_p95_ms   REAL     NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, bucket)
);

-- Per-minute container resource rollups from docker stats.
CREATE TABLE site_container_metrics (
    site_id   TEXT     NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    bucket    DATETIME NOT NULL,
    cpu_pct   REAL     NOT NULL DEFAULT 0,
    mem_bytes INTEGER  NOT NULL DEFAULT 0,
    mem_limit INTEGER  NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, bucket)
);

-- +goose Down
DROP TABLE site_container_metrics;
DROP TABLE site_request_metrics;
DROP TABLE activity;
DROP TABLE platform_kv;
DROP TABLE job_events;
DROP TABLE jobs;
DROP TABLE site_domains;
DROP TABLE sites;
