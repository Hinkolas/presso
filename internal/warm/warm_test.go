package warm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

func newSite(t *testing.T, st *store.Store, slug, status, settings string, pageCache bool) store.Site {
	t.Helper()
	now := time.Now().UTC()
	row, err := st.CreateSite(context.Background(), store.CreateSiteParams{
		ID: uuid.New(), Slug: slug, Name: slug, Status: status, PrimaryDomain: "demo.localhost", TlsMode: "off",
		PhpVersion: "8.3", Image: "img", Profile: "prod", FileMods: "locked", PageCache: pageCache,
		DbName: "d" + slug, DbUser: "u" + slug, DbPasswordEnc: []byte("x"), RedisUser: "r" + slug, RedisPasswordEnc: []byte("x"),
		SaltsEnc: []byte("x"), PurgeTokenHash: []byte("x"), LoginSecretEnc: []byte("x"), AdminUser: "a",
		AdminEmail: "a@b.c", Settings: settings, VolumeName: "v" + slug, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func newService(t *testing.T, st *store.Store, f *Fetcher) *Service {
	t.Helper()
	cfg := &config.API{}
	cfg.Warm = config.Warm{Interval: time.Hour, Timeout: 5 * time.Second, MaxURLs: 100}
	s := New(Deps{Store: st, Config: cfg})
	s.fetch = f
	s.debounce = 50 * time.Millisecond
	s.grace = 0
	return s
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRunCountsOutcomes(t *testing.T) {
	st := testdb.New(t)
	fs, srv, f := newFake(map[string]string{
		"/wp-sitemap.xml": `<urlset><url><loc>http://demo.localhost/a/</loc></url><url><loc>http://demo.localhost/b/</loc></url></urlset>`,
		"/":               "home", "/a/": "a", "/b/": "b",
	})
	defer srv.Close()
	row := newSite(t, st, "demo", "running", `{"cache_warm":true}`, true)
	s := newService(t, st, f)

	sum := s.Run(context.Background(), row, ReasonTrigger)
	if sum.URLs != 3 || sum.Hit != 3 || sum.Errors != 0 || sum.Sitemap != "http://demo.localhost/wp-sitemap.xml" {
		t.Fatalf("summary: %+v", sum)
	}
	if fs.hits["/"] != 1 || fs.hits["/a/"] != 1 || fs.hits["/b/"] != 1 {
		t.Fatalf("hits: %v", fs.hits)
	}
}

func TestRunAbortsWhenEdgeIsDown(t *testing.T) {
	st := testdb.New(t)
	_, srv, f := newFake(map[string]string{})
	srv.Close() // nothing listens any more
	row := newSite(t, st, "demo", "running", `{"cache_warm":true}`, true)
	s := newService(t, st, f)
	sum := s.Run(context.Background(), row, ReasonPeriodic)
	if sum.URLs != 1 || sum.Errors != 1 {
		t.Fatalf("summary: %+v", sum)
	}
}

func TestScheduler(t *testing.T) {
	st := testdb.New(t)
	var (
		mu    sync.Mutex
		slow  bool
		walks int
	)
	fs := &fakeSite{docs: map[string]string{
		"/wp-sitemap.xml": `<urlset><url><loc>http://demo.localhost/a/</loc></url></urlset>`,
		"/":               "home", "/a/": "a",
	}, hits: map[string]int{}}
	srv := newServer(fs, func(path string) {
		mu.Lock()
		s := slow
		if path == "/wp-sitemap.xml" {
			walks++
		}
		mu.Unlock()
		if s && path != "/robots.txt" {
			time.Sleep(150 * time.Millisecond)
		}
	})
	defer srv.Close()
	f := NewFetcher(srv.Listener.Addr().String(), srv.Listener.Addr().String(), 5*time.Second)
	s := newService(t, st, f)
	countWalks := func() int { mu.Lock(); defer mu.Unlock(); return walks }

	on := newSite(t, st, "on", "running", `{"cache_warm":true}`, true)
	newSite(t, st, "stopped", "stopped", `{"cache_warm":true}`, true)
	newSite(t, st, "nocache", "running", `{"cache_warm":true}`, false)
	newSite(t, st, "optout", "running", `{}`, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Start(ctx); close(done) }()

	// The periodic seed is staggered inside the hour; only a trigger should
	// walk within the test. Three triggers coalesce into one run.
	s.Trigger(on.ID)
	s.Trigger(on.ID)
	s.Trigger(on.ID)
	waitFor(t, "first walk", func() bool { _, ok := s.LastRun(on.ID); return ok })
	time.Sleep(200 * time.Millisecond)
	if countWalks() != 1 {
		t.Fatalf("walks after coalesced triggers: %d", countWalks())
	}
	if last, _ := s.LastRun(on.ID); last.Reason != ReasonTrigger || last.URLs != 2 {
		t.Fatalf("last run: %+v", last)
	}

	// A trigger during a run schedules exactly one follow-up.
	mu.Lock()
	slow = true
	mu.Unlock()
	s.Trigger(on.ID)
	waitFor(t, "slow walk to start", func() bool { return countWalks() == 2 })
	s.Trigger(on.ID)
	s.Trigger(on.ID)
	waitFor(t, "follow-up walk", func() bool { return countWalks() == 3 })
	time.Sleep(400 * time.Millisecond)
	if countWalks() != 3 {
		t.Fatalf("walks after mid-run triggers: %d", countWalks())
	}

	// Ineligible sites are never walked, even when triggered.
	for _, slug := range []string{"stopped", "nocache", "optout"} {
		row, _ := st.GetSiteBySlug(ctx, slug)
		s.Trigger(row.ID)
	}
	time.Sleep(300 * time.Millisecond)
	if countWalks() != 3 {
		t.Fatalf("ineligible site walked: %d", countWalks())
	}
	s.mu.Lock()
	n := len(s.state)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("state entries: %d (ineligible sites should be dropped)", n)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

// newServer wraps a fakeSite with a per-request hook.
func newServer(fs *fakeSite, hook func(path string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hook(r.URL.Path)
		fs.ServeHTTP(w, r)
	}))
}
