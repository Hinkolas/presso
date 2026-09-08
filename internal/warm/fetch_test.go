package warm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetcherRoutesThroughEdge(t *testing.T) {
	type seen struct{ host, ua, enc, path, sni string }
	var got seen
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{host: r.Host, ua: r.UserAgent(), enc: r.Header.Get("Accept-Encoding"), path: r.URL.Path}
		if r.TLS != nil {
			got.sni = r.TLS.ServerName
		}
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
			return
		case "/moved":
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Cache-Status", "presso; hit")
		w.Write([]byte("ok"))
	})
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	f := NewFetcher(plain.Listener.Addr().String(), secure.Listener.Addr().String(), 5*time.Second)
	ctx := context.Background()

	r, err := f.Get(ctx, "http://demo.localhost/")
	if err != nil || r.Status != 200 || r.CacheStatus != "presso; hit" {
		t.Fatalf("http: %v %+v", err, r)
	}
	if got.host != "demo.localhost" || got.ua != "presso-warmer/1" || got.enc != acceptEncoding {
		t.Fatalf("http request seen by edge: %+v", got)
	}

	r, err = f.Get(ctx, "https://demo.example/")
	if err != nil || r.Status != 200 {
		t.Fatalf("https: %v %+v", err, r)
	}
	if got.sni != "demo.example" || got.host != "demo.example" {
		t.Fatalf("https request seen by edge: %+v", got)
	}

	// Redirects stay on the site.
	r, err = f.Get(ctx, "http://demo.localhost/away")
	if err != nil || r.Status != http.StatusFound {
		t.Fatalf("cross-host redirect followed: %v %+v", err, r)
	}
	r, err = f.Get(ctx, "http://demo.localhost/moved")
	if err != nil || r.Status != 200 || got.path != "/final" || r.FinalURL != "http://demo.localhost/final" {
		t.Fatalf("same-host redirect: %v %+v %+v", err, r, got)
	}

	// GetBody asks for a plain body.
	_, body, err := f.GetBody(ctx, "http://demo.localhost/robots.txt", 1<<10)
	if err != nil || string(body) != "ok" || got.enc != "identity" {
		t.Fatalf("body: %v %q %+v", err, body, got)
	}
}

func TestLoopback(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:80":     "127.0.0.1:80",
		":443":           "127.0.0.1:443",
		"[::]:80":        "127.0.0.1:80",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"garbage":        "127.0.0.1:80",
	} {
		if got := loopback(in, "80"); got != want {
			t.Errorf("loopback(%q) = %q, want %q", in, got, want)
		}
	}
}
