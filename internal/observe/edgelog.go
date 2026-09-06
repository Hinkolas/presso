// Package observe turns raw Docker signals into what the UI shows: container
// events become site status changes, periodic stats samples become resource
// metrics, and the edge's JSON access log becomes per-site request and
// cache-hit rollups. Everything is stored as one-minute buckets in SQLite;
// there is no external metrics stack.
package observe

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/store"
)

// accessEntry is the subset of a Caddy JSON access log line we read.
type accessEntry struct {
	Msg     string  `json:"msg"`
	Logger  string  `json:"logger"`
	Status  int     `json:"status"`
	Size    int64   `json:"size"`
	Dur     float64 `json:"duration"` // seconds
	Request struct {
		Host   string `json:"host"`
		Method string `json:"method"`
		URI    string `json:"uri"`
	} `json:"request"`
	RespHeaders map[string][]string `json:"resp_headers"`
}

// CacheOutcome classifies a response by its Cache-Status header.
type CacheOutcome int

const (
	OutcomeBypass CacheOutcome = iota
	OutcomeHit
	OutcomeMiss
	OutcomeStale
)

// ClassifyCacheStatus reads an RFC 9211 Cache-Status value as emitted by the
// edge cache module. An absent header means the request never reached the
// cache handler (bypass route, cache disabled).
func ClassifyCacheStatus(v string) CacheOutcome {
	if v == "" {
		return OutcomeBypass
	}
	l := strings.ToLower(v)
	switch {
	case strings.Contains(l, "stale"):
		return OutcomeStale
	case strings.Contains(l, "; hit") || strings.HasPrefix(l, "hit"):
		return OutcomeHit
	case strings.Contains(l, "bypass") || strings.Contains(l, "no-store") || strings.Contains(l, "private") || strings.Contains(l, "not-cacheable"):
		return OutcomeBypass
	case strings.Contains(l, "fwd="):
		return OutcomeMiss
	}
	return OutcomeBypass
}

// bucketKey identifies one site-minute.
type bucketKey struct {
	site   uuid.UUID
	minute time.Time
}

type bucketAcc struct {
	store.UpsertRequestMetricParams
	durations []float64
}

// Aggregator accumulates access entries into minute buckets and flushes them
// to the store. It is safe for one producer and one flusher.
type Aggregator struct {
	mu      sync.Mutex
	buckets map[bucketKey]*bucketAcc
	st      *store.Store
	// hostToSite maps a lowercase hostname to a site id; refreshed by the
	// ingester.
	hostToSite map[string]uuid.UUID
}

func NewAggregator(st *store.Store) *Aggregator {
	return &Aggregator{buckets: map[bucketKey]*bucketAcc{}, st: st, hostToSite: map[string]uuid.UUID{}}
}

// SetHosts replaces the host → site map.
func (a *Aggregator) SetHosts(m map[string]uuid.UUID) {
	a.mu.Lock()
	a.hostToSite = m
	a.mu.Unlock()
}

// Ingest parses one log line. Non-access lines and unknown hosts are ignored.
// It returns true when the line was counted.
func (a *Aggregator) Ingest(line string, at time.Time) bool {
	if !strings.HasPrefix(line, "{") || !strings.Contains(line, `"request"`) {
		return false
	}
	var e accessEntry
	if err := json.Unmarshal([]byte(line), &e); err != nil || e.Request.Host == "" {
		return false
	}
	siteID, ok := a.resolveSite(e)
	if !ok {
		return false
	}
	var cacheStatus string
	if v := e.RespHeaders["Cache-Status"]; len(v) > 0 {
		cacheStatus = v[0]
	}
	outcome := ClassifyCacheStatus(cacheStatus)
	ms := e.Dur * 1000

	a.mu.Lock()
	defer a.mu.Unlock()
	key := bucketKey{site: siteID, minute: at.UTC().Truncate(time.Minute)}
	b := a.buckets[key]
	if b == nil {
		b = &bucketAcc{UpsertRequestMetricParams: store.UpsertRequestMetricParams{SiteID: siteID, Bucket: key.minute}}
		a.buckets[key] = b
	}
	b.Requests++
	switch outcome {
	case OutcomeHit:
		b.CacheHit++
	case OutcomeMiss:
		b.CacheMiss++
	case OutcomeStale:
		b.CacheStale++
	default:
		b.CacheBypass++
	}
	switch {
	case e.Status >= 500:
		b.Status5xx++
	case e.Status >= 400:
		b.Status4xx++
	case e.Status >= 300:
		b.Status3xx++
	default:
		b.Status2xx++
	}
	b.Bytes += e.Size
	b.DurSumMs += ms
	b.durations = append(b.durations, ms)
	return true
}

func (a *Aggregator) resolveSite(e accessEntry) (uuid.UUID, bool) {
	if v := e.RespHeaders["X-Presso-Site"]; len(v) > 0 {
		if id, err := uuid.Parse(v[0]); err == nil {
			return id, true
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	host := strings.ToLower(e.Request.Host)
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	id, ok := a.hostToSite[host]
	return id, ok
}

// Flush writes every accumulated bucket and clears the accumulator.
func (a *Aggregator) Flush(ctx context.Context) error {
	a.mu.Lock()
	buckets := a.buckets
	a.buckets = map[bucketKey]*bucketAcc{}
	a.mu.Unlock()
	for _, b := range buckets {
		sort.Float64s(b.durations)
		b.DurP50Ms = percentile(b.durations, 0.50)
		b.DurP95Ms = percentile(b.durations, 0.95)
		if err := a.st.UpsertRequestMetric(ctx, b.UpsertRequestMetricParams); err != nil {
			return err
		}
	}
	return nil
}

// Pending reports the number of unflushed buckets (tests, diagnostics).
func (a *Aggregator) Pending() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.buckets)
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	// Nearest-rank: the smallest value with at least p of the samples at or
	// below it, so a p95 over few samples leans high rather than low.
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	return sorted[rank]
}

// EdgeLogIngester follows the edge container's log and feeds the aggregator.
type EdgeLogIngester struct {
	dk  docker.Client
	st  *store.Store
	agg *Aggregator
}

func NewEdgeLogIngester(dk docker.Client, st *store.Store) *EdgeLogIngester {
	return &EdgeLogIngester{dk: dk, st: st, agg: NewAggregator(st)}
}

// Run follows the edge log until ctx ends, reconnecting when the stream
// drops (edge restart) and flushing buckets every flushEvery.
func (in *EdgeLogIngester) Run(ctx context.Context, flushEvery time.Duration) {
	since := time.Now().UTC()
	flush := time.NewTicker(flushEvery)
	defer flush.Stop()
	hosts := time.NewTicker(30 * time.Second)
	defer hosts.Stop()
	in.refreshHosts(ctx)

	for ctx.Err() == nil {
		info, err := in.dk.InspectContainer(ctx, platform.NameEdge)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		lctx, cancel := context.WithCancel(ctx)
		lines, errc := in.dk.Logs(lctx, info.ID, docker.LogOptions{Follow: true, Since: since})
	stream:
		for {
			select {
			case <-ctx.Done():
				cancel()
				_ = in.agg.Flush(context.WithoutCancel(ctx))
				return
			case <-flush.C:
				if err := in.agg.Flush(ctx); err != nil {
					slog.WarnContext(ctx, "observe: flush request metrics", "err", err)
				}
			case <-hosts.C:
				in.refreshHosts(ctx)
			case l, ok := <-lines:
				if !ok {
					break stream
				}
				since = l.Time.Add(time.Nanosecond)
				in.agg.Ingest(l.Text, l.Time)
			case err := <-errc:
				if err != nil {
					slog.WarnContext(ctx, "observe: edge log stream", "err", err)
				}
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (in *EdgeLogIngester) refreshHosts(ctx context.Context) {
	domains, err := in.st.ListAllDomains(ctx)
	if err != nil {
		return
	}
	m := make(map[string]uuid.UUID, len(domains))
	for _, d := range domains {
		m[strings.ToLower(d.Host)] = d.SiteID
	}
	in.agg.SetHosts(m)
}
