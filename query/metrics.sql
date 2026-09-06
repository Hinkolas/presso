-- name: UpsertRequestMetric :exec
INSERT INTO site_request_metrics (
    site_id, bucket, requests, cache_hit, cache_miss, cache_bypass, cache_stale,
    status_2xx, status_3xx, status_4xx, status_5xx, bytes, dur_sum_ms, dur_p50_ms, dur_p95_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (site_id, bucket) DO UPDATE SET
    requests     = requests + excluded.requests,
    cache_hit    = cache_hit + excluded.cache_hit,
    cache_miss   = cache_miss + excluded.cache_miss,
    cache_bypass = cache_bypass + excluded.cache_bypass,
    cache_stale  = cache_stale + excluded.cache_stale,
    status_2xx   = status_2xx + excluded.status_2xx,
    status_3xx   = status_3xx + excluded.status_3xx,
    status_4xx   = status_4xx + excluded.status_4xx,
    status_5xx   = status_5xx + excluded.status_5xx,
    bytes        = bytes + excluded.bytes,
    dur_sum_ms   = dur_sum_ms + excluded.dur_sum_ms,
    dur_p50_ms   = max(dur_p50_ms, excluded.dur_p50_ms),
    dur_p95_ms   = max(dur_p95_ms, excluded.dur_p95_ms);

-- name: ListRequestMetrics :many
SELECT * FROM site_request_metrics WHERE site_id = ? AND bucket >= ? ORDER BY bucket;

-- name: SumRequestMetricsSince :many
SELECT site_id,
       CAST(coalesce(sum(requests), 0) AS INTEGER)     AS requests,
       CAST(coalesce(sum(cache_hit), 0) AS INTEGER)    AS cache_hit,
       CAST(coalesce(sum(cache_miss), 0) AS INTEGER)   AS cache_miss,
       CAST(coalesce(sum(cache_bypass), 0) AS INTEGER) AS cache_bypass,
       CAST(coalesce(sum(cache_stale), 0) AS INTEGER)  AS cache_stale,
       CAST(coalesce(sum(status_5xx), 0) AS INTEGER)   AS status_5xx
FROM site_request_metrics WHERE bucket >= ? GROUP BY site_id;

-- name: UpsertContainerMetric :exec
INSERT INTO site_container_metrics (site_id, bucket, cpu_pct, mem_bytes, mem_limit)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (site_id, bucket) DO UPDATE SET
    cpu_pct = excluded.cpu_pct, mem_bytes = excluded.mem_bytes, mem_limit = excluded.mem_limit;

-- name: ListContainerMetrics :many
SELECT * FROM site_container_metrics WHERE site_id = ? AND bucket >= ? ORDER BY bucket;

-- name: DeleteRequestMetricsBefore :execrows
DELETE FROM site_request_metrics WHERE bucket < ?;

-- name: DeleteContainerMetricsBefore :execrows
DELETE FROM site_container_metrics WHERE bucket < ?;
