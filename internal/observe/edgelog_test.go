package observe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

func TestClassify(t *testing.T) {
	cases := map[string]CacheOutcome{
		"":                                         OutcomeBypass,
		"presso; hit; ttl=43000":                   OutcomeHit,
		"presso; fwd=uri-miss; stored":             OutcomeMiss,
		"presso; fwd=uri-miss; detail=NO-STORE":    OutcomeBypass,
		"presso; hit; fwd=stale":                   OutcomeStale,
		"presso; fwd=bypass; detail=PRIVATE-CACHE": OutcomeBypass,
	}
	for in, want := range cases {
		if got := ClassifyCacheStatus(in); got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
}

func TestAggregator(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()
	site, err := st.CreateSite(ctx, store.CreateSiteParams{
		ID: uuid.New(), Slug: "s", Name: "s", Status: "running", PrimaryDomain: "s.localhost", TlsMode: "off",
		PhpVersion: "8.3", Image: "img", Profile: "prod", FileMods: "locked", PageCache: true,
		DbName: "d", DbUser: "u", DbPasswordEnc: []byte("x"), RedisUser: "r", RedisPasswordEnc: []byte("x"),
		SaltsEnc: []byte("x"), PurgeTokenHash: []byte("x"), LoginSecretEnc: []byte("x"), AdminUser: "a",
		AdminEmail: "a@b.c", Settings: "{}", VolumeName: "v", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	agg := NewAggregator(st)
	agg.SetHosts(map[string]uuid.UUID{"s.localhost": site.ID})

	line := func(status int, dur float64, cache string, host string) string {
		hdr := ""
		if cache != "" {
			hdr = fmt.Sprintf(`"Cache-Status":[%q],`, cache)
		}
		return fmt.Sprintf(`{"level":"info","logger":"http.log.access","msg":"handled request","request":{"remote_ip":"1.2.3.4","proto":"HTTP/2.0","method":"GET","host":%q,"uri":"/"},"duration":%f,"size":1000,"status":%d,"resp_headers":{%s"Content-Type":["text/html"]}}`, host, dur, status, hdr)
	}
	at := now.Truncate(time.Minute).Add(10 * time.Second)
	if !agg.Ingest(line(200, 0.010, "presso; hit", "s.localhost"), at) {
		t.Fatal("hit not ingested")
	}
	agg.Ingest(line(200, 0.200, "presso; fwd=uri-miss; stored", "s.localhost"), at)
	agg.Ingest(line(302, 0.005, "", "s.localhost"), at)
	agg.Ingest(line(500, 1.5, "presso; fwd=uri-miss", "S.LOCALHOST:443"), at)
	if agg.Ingest(line(200, 0.1, "", "unknown.example"), at) {
		t.Fatal("unknown host counted")
	}
	if agg.Ingest(`not json`, at) || agg.Ingest(`{"level":"info","msg":"server running"}`, at) {
		t.Fatal("non-access line counted")
	}
	if agg.Pending() != 1 {
		t.Fatalf("pending: %d", agg.Pending())
	}
	if err := agg.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListRequestMetrics(ctx, store.ListRequestMetricsParams{SiteID: site.ID, Bucket: now.Add(-time.Hour)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %v %d", err, len(rows))
	}
	r := rows[0]
	if r.Requests != 4 || r.CacheHit != 1 || r.CacheMiss != 2 || r.CacheBypass != 1 || r.Status2xx != 2 || r.Status3xx != 1 || r.Status5xx != 1 || r.Bytes != 4000 {
		t.Fatalf("counts: %+v", r)
	}
	if r.DurP95Ms < 1000 || r.DurP50Ms > 200 {
		t.Fatalf("percentiles: p50=%v p95=%v", r.DurP50Ms, r.DurP95Ms)
	}
	// A second flush into the same bucket adds up.
	agg.Ingest(line(200, 0.010, "presso; hit", "s.localhost"), at)
	_ = agg.Flush(ctx)
	rows, _ = st.ListRequestMetrics(ctx, store.ListRequestMetricsParams{SiteID: site.ID, Bucket: now.Add(-time.Hour)})
	if rows[0].Requests != 5 || rows[0].CacheHit != 2 {
		t.Fatalf("merge: %+v", rows[0])
	}
	sums, _ := st.SumRequestMetricsSince(ctx, now.Add(-time.Hour))
	if len(sums) != 1 || sums[0].Requests != 5 {
		t.Fatalf("sums: %+v", sums)
	}
}
