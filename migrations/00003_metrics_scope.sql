-- +goose Up
-- Split request rollups by audience: "visitor" traffic (the public site) and
-- "admin" traffic (wp-admin, wp-login, wp-json, wp-cron, xmlrpc). SQLite
-- cannot change a primary key in place, so the table is rebuilt. Rows
-- collected before this migration are kept as visitor traffic.
CREATE TABLE site_request_metrics_v2 (
    site_id      TEXT     NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    bucket       DATETIME NOT NULL,                   -- minute start, UTC
    scope        TEXT     NOT NULL DEFAULT 'visitor', -- visitor | admin
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
    PRIMARY KEY (site_id, bucket, scope)
);
INSERT INTO site_request_metrics_v2 (
    site_id, bucket, scope, requests, cache_hit, cache_miss, cache_bypass, cache_stale,
    status_2xx, status_3xx, status_4xx, status_5xx, bytes, dur_sum_ms, dur_p50_ms, dur_p95_ms
)
SELECT site_id, bucket, 'visitor', requests, cache_hit, cache_miss, cache_bypass, cache_stale,
       status_2xx, status_3xx, status_4xx, status_5xx, bytes, dur_sum_ms, dur_p50_ms, dur_p95_ms
FROM site_request_metrics;
DROP TABLE site_request_metrics;
ALTER TABLE site_request_metrics_v2 RENAME TO site_request_metrics;

-- +goose Down
CREATE TABLE site_request_metrics_v1 (
    site_id      TEXT     NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    bucket       DATETIME NOT NULL,
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
INSERT INTO site_request_metrics_v1
SELECT site_id, bucket, sum(requests), sum(cache_hit), sum(cache_miss), sum(cache_bypass), sum(cache_stale),
       sum(status_2xx), sum(status_3xx), sum(status_4xx), sum(status_5xx), sum(bytes), sum(dur_sum_ms),
       max(dur_p50_ms), max(dur_p95_ms)
FROM site_request_metrics GROUP BY site_id, bucket;
DROP TABLE site_request_metrics;
ALTER TABLE site_request_metrics_v1 RENAME TO site_request_metrics;
