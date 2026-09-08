package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	sites := []SiteRoute{
		{ID: "b2", Slug: "shop", Hosts: []string{"shop.example.com", "www.shop.example.com"}, TLSMode: "auto", Upstream: "site-b2:8080", PageCache: true, CacheTTL: 2 * time.Hour},
		{ID: "a1", Slug: "demo", Hosts: []string{"demo.localhost"}, TLSMode: "auto", Upstream: "site-a1:8080", PageCache: false},
		{ID: "c3", Slug: "plain", Hosts: []string{"plain.example.org"}, TLSMode: "off", Upstream: "site-c3:8080", PageCache: true},
	}
	out, err := Render(sites, Options{ACMEEmail: "ops@example.com"}, time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"admin 127.0.0.1:2019",
		"email ops@example.com",
		"basepath /-/cache",
		"ttl 12h",
		"http://demo.localhost {", // .localhost never gets TLS
		"https://shop.example.com, https://www.shop.example.com {", // auto TLS
		"http://plain.example.org {",                               // tls off
		"reverse_proxy site-b2:8080",
		"ttl 2h",
		"uri query -utm_source",
		"uri query -fbclid",
		"header X-Presso-Site a1",
		":2020 {",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("missing %q in:\n%s", c, out)
		}
	}
	// Sorted by slug: demo, plain, shop.
	if strings.Index(out, "# site demo") > strings.Index(out, "# site plain") || strings.Index(out, "# site plain") > strings.Index(out, "# site shop") {
		t.Error("sites not sorted by slug")
	}
	// The cache-less site has no cache block inside its route.
	demo := out[strings.Index(out, "# site demo"):strings.Index(out, "# site plain")]
	if strings.Contains(demo, "cache {") {
		t.Error("page cache rendered for a site with page_cache=false")
	}

	if strings.Contains(out, "panel") {
		t.Error("panel vhost rendered without PanelHost")
	}
	withPanel, err := Render(sites, Options{PanelHost: "Panel.Example.com", PanelUpstream: "host.docker.internal:7080"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"https://panel.example.com {", "reverse_proxy https://host.docker.internal:7080 {", "flush_interval -1", "header X-Presso-Edge panel"} {
		if !strings.Contains(withPanel, c) {
			t.Errorf("panel: missing %q in:\n%s", c, withPanel)
		}
	}
	panel := withPanel[strings.Index(withPanel, "https://panel.example.com {"):strings.Index(withPanel, "# site demo")]
	if strings.Contains(panel, "cache {") {
		t.Error("panel vhost must not be cached")
	}
	if _, err := Render(sites, Options{PanelHost: "bad host", PanelUpstream: "u:1"}, time.Now()); err == nil {
		t.Error("invalid panel host accepted")
	}
	if _, err := Render(sites, Options{PanelHost: "panel.example.com"}, time.Now()); err == nil {
		t.Error("panel host without upstream accepted")
	}

	if _, err := Render([]SiteRoute{{ID: "x", Slug: "x", Hosts: []string{"bad host"}, Upstream: "u:1"}}, Options{}, time.Now()); err == nil {
		t.Error("invalid host accepted")
	}
	if _, err := Render([]SiteRoute{{ID: "x", Slug: "x", Upstream: "u:1"}}, Options{}, time.Now()); err == nil {
		t.Error("site without hosts accepted")
	}
}

func TestValidHost(t *testing.T) {
	for _, ok := range []string{"example.com", "a.b.c.example.co.uk", "demo.localhost", "localhost", "xn--bcher-kva.ch", "Example.COM"} {
		if !ValidHost(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-a.com", "a-.com", "a..com", "a.com:80", "*.example.com", "exa mple.com", "http://a.com"} {
		if ValidHost(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestClient(t *testing.T) {
	var gotLoad, gotPurge string
	var gotKeys string
	// The fake admin API "adapts" any Caddyfile to a fixed JSON shape (with a
	// cache handler on :2020) unless it contains BROKEN.
	adapted := `{"apps":{"http":{"servers":{"srv0":{"listen":[":2020"],"routes":[{"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"cache","Configuration":{"DefaultCache":{}}},{"handler":"static_response"}]}]}]}]},"srv1":{"listen":[":80"],"routes":[{"handle":[{"handler":"cache","Configuration":{}}]}]}}}}}`
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/adapt":
			b, _ := io.ReadAll(r.Body)
			if strings.Contains(string(b), "BROKEN") {
				http.Error(w, "adapting config using caddyfile: syntax error", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":` + adapted + `}`))
		case r.Method == "POST" && r.URL.Path == "/load":
			b, _ := io.ReadAll(r.Body)
			gotLoad = string(b)
			if r.Header.Get("Content-Type") != "application/json" {
				http.Error(w, "expected json", 400)
				return
			}
			w.WriteHeader(200)
		case r.URL.Path == "/config/":
			w.Write([]byte(`{"apps":{"http":{"servers":{"srv0":{"routes":[{"handle":[{"handler":"headers","response":{"set":{"X-Presso-Edge":["a"]}}}]}]}}}}}`))
		}
	}))
	defer admin.Close()
	cache := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPurge = r.Method + " " + r.URL.Path
		gotKeys = r.Header.Get("Surrogate-Key")
		w.WriteHeader(204)
	}))
	defer cache.Close()

	dir := t.TempDir()
	c := NewClient(strings.TrimPrefix(admin.URL, "http://"), strings.TrimPrefix(cache.URL, "http://"), dir)
	ctx := context.Background()
	if err := c.Load(ctx, "OK config"); err != nil {
		t.Fatalf("load: %v", err)
	}
	// The API was injected on the :2020 handler only.
	var loaded map[string]any
	if err := json.Unmarshal([]byte(gotLoad), &loaded); err != nil {
		t.Fatalf("loaded config is not json: %v", err)
	}
	if !strings.Contains(gotLoad, `"API":{"basepath":"/-/cache","souin":{"enable":true}}`) || strings.Count(gotLoad, `"API"`) != 1 {
		t.Fatalf("api injection: %s", gotLoad)
	}
	if _, err := os.Stat(dir + "/Caddyfile"); err != nil {
		t.Fatalf("caddyfile not written: %v", err)
	}
	if err := c.Load(ctx, "BROKEN"); err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("expected caddy error, got %v", err)
	}
	ok, err := c.Loaded(ctx)
	if err != nil || !ok {
		t.Fatalf("loaded: %v %v", ok, err)
	}
	if err := c.PurgeTags(ctx, []string{"sA", "sA:p1"}); err != nil || gotPurge != "PURGE /-/cache/souin" || gotKeys != "sA, sA:p1" {
		t.Fatalf("purge tags: %v %q %q", err, gotPurge, gotKeys)
	}
	if err := c.PurgeHosts(ctx, []string{"demo.localhost"}); err != nil || !strings.HasPrefix(gotPurge, "PURGE /-/cache/souin/.*demo") {
		t.Fatalf("purge hosts: %v %q", err, gotPurge)
	}
	bad := NewClient("127.0.0.1:1", "127.0.0.1:1", "")
	if err := bad.Load(ctx, "x"); err == nil {
		t.Fatal("expected unavailable")
	}
}

func TestInjectCacheAPIRequiresHandler(t *testing.T) {
	if _, err := injectCacheAPI(json.RawMessage(`{"apps":{"http":{"servers":{"s":{"listen":[":80"],"routes":[]}}}}}`), "/-/cache"); err == nil {
		t.Fatal("expected error without a cache-admin handler")
	}
}
