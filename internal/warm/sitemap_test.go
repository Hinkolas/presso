package warm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeSite serves the documents in docs by path and counts requests.
type fakeSite struct {
	docs map[string]string
	hits map[string]int
}

func (f *fakeSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.hits[r.URL.Path]++
	if loc, ok := f.docs["redirect:"+r.URL.Path]; ok {
		http.Redirect(w, r, loc, http.StatusMovedPermanently)
		return
	}
	body, ok := f.docs[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !strings.HasSuffix(r.URL.Path, ".xml") && r.URL.Path != "/robots.txt" {
		w.Header().Set("Cache-Status", "presso; hit")
	}
	w.Write([]byte(body))
}

func newFake(docs map[string]string) (*fakeSite, *httptest.Server, *Fetcher) {
	fs := &fakeSite{docs: docs, hits: map[string]int{}}
	srv := httptest.NewServer(fs)
	f := NewFetcher(srv.Listener.Addr().String(), srv.Listener.Addr().String(), 5*time.Second)
	return fs, srv, f
}

const (
	indexXML = `<?xml version="1.0" encoding="UTF-8"?><?xml-stylesheet type="text/xsl" href="/wp-sitemap.xsl" ?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>http://demo.localhost/wp-sitemap-posts-post-1.xml</loc></sitemap>
  <sitemap><loc>http://demo.localhost/wp-sitemap-posts-page-1.xml</loc></sitemap>
  <sitemap><loc>http://other.example/wp-sitemap-posts-page-1.xml</loc></sitemap>
  <sitemap><loc>http://demo.localhost/missing.xml</loc></sitemap>
</sitemapindex>`
	postsXML = `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url><loc>http://demo.localhost/</loc><lastmod>2026-01-01</lastmod></url>
  <url><loc>https://demo.localhost/hello-world/</loc><image:image><image:loc>http://demo.localhost/a.jpg</image:loc></image:image></url>
  <url><loc>http://demo.localhost/hello-world/#comments</loc></url>
  <url><loc>http://demo.localhost/?p=7</loc></url>
  <url><loc>http://evil.example/steal</loc></url>
  <url><loc>http://demo.localhost/archive.xml.gz</loc></url>
</urlset>`
	pagesXML = `<urlset><url><loc> http://demo.localhost/about/ </loc></url><url><loc>http://demo.localhost/contact/</loc></url></urlset>`
)

func TestDiscoverWordPressCore(t *testing.T) {
	fs, srv, f := newFake(map[string]string{
		"/robots.txt":                  "User-agent: *\nDisallow: /wp-admin/\n\nSitemap: http://demo.localhost/wp-sitemap.xml\n",
		"/wp-sitemap.xml":              indexXML,
		"/wp-sitemap-posts-post-1.xml": postsXML,
		"/wp-sitemap-posts-page-1.xml": pagesXML,
	})
	defer srv.Close()
	base, _ := url.Parse("http://demo.localhost")
	urls, source := Discover(context.Background(), f, base, 100)
	want := []string{
		"http://demo.localhost/",
		"http://demo.localhost/hello-world/",
		"http://demo.localhost/?p=7",
		"http://demo.localhost/about/",
		"http://demo.localhost/contact/",
	}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("urls: %v", urls)
	}
	if source != "http://demo.localhost/wp-sitemap.xml" {
		t.Fatalf("source: %q", source)
	}
	if fs.hits["/missing.xml"] != 1 || fs.hits["/sitemap_index.xml"] != 0 {
		t.Fatalf("fetches: %v", fs.hits)
	}

	// The cap counts the front page.
	urls, _ = Discover(context.Background(), f, base, 2)
	if len(urls) != 2 || urls[1] != "http://demo.localhost/hello-world/" {
		t.Fatalf("capped: %v", urls)
	}
}

func TestDiscoverFallbacks(t *testing.T) {
	// No robots hint; core sitemap disabled and redirected by an SEO plugin.
	fs, srv, f := newFake(map[string]string{
		"redirect:/wp-sitemap.xml": "/sitemap_index.xml",
		"/sitemap_index.xml":       `<sitemapindex><sitemap><loc>http://demo.localhost/page-sitemap.xml</loc></sitemap></sitemapindex>`,
		"/page-sitemap.xml":        pagesXML,
	})
	defer srv.Close()
	base, _ := url.Parse("http://demo.localhost")
	urls, source := Discover(context.Background(), f, base, 100)
	if len(urls) != 3 || source != "http://demo.localhost/sitemap_index.xml" {
		t.Fatalf("yoast: %v %q", urls, source)
	}
	if fs.hits["/robots.txt"] != 1 {
		t.Fatalf("robots not consulted: %v", fs.hits)
	}

	// A 200 HTML page where the sitemap should be is not a sitemap.
	_, srv2, f2 := newFake(map[string]string{"/wp-sitemap.xml": "<html><body>nope</body></html>"})
	defer srv2.Close()
	urls, source = Discover(context.Background(), f2, base, 100)
	if len(urls) != 1 || urls[0] != "http://demo.localhost/" || source != "" {
		t.Fatalf("no sitemap: %v %q", urls, source)
	}
}

func TestParseRobotsSitemaps(t *testing.T) {
	got := parseRobotsSitemaps([]byte("User-agent: *\nsitemap: http://a/x.xml\n  SITEMAP:http://a/y.xml \nSitemapx: nope\n"))
	if !reflect.DeepEqual(got, []string{"http://a/x.xml", "http://a/y.xml"}) {
		t.Fatalf("%v", got)
	}
}
