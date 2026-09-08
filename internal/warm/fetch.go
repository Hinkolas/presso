package warm

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Hinkolas/presso/internal/observe"
)

// Request headers. The edge cache keys on method, scheme, host and URI, so a
// warm request only has to look like a browser where the origin varies on
// it: the legacy nginx runtime sends Vary: Accept-Encoding, and Souin then
// stores one variant per exact header value. Chrome and Firefox both send
// this string.
const (
	acceptEncoding = "gzip, deflate, br, zstd"
	acceptHTML     = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	maxRedirects   = 3
	drainLimit     = 8 << 20
)

// Fetcher requests site URLs through the edge the way a visitor would: the
// URL carries the site's public scheme and host (which the edge keys and
// routes on), while the TCP connection goes to the edge's loopback port.
// The edge's certificate is not verified: it is our own container and a
// fresh domain may still be waiting for its ACME certificate.
type Fetcher struct {
	client *http.Client
}

// Result is what the warmer cares about in a response.
type Result struct {
	Status      int
	CacheStatus string
	Outcome     observe.CacheOutcome
	FinalURL    string // after same-host redirects
}

// NewFetcher dials httpListen for http:// URLs and httpsListen for https://
// ones. The listen addresses are the edge's host-side published ports, as
// configured (0.0.0.0 is rewritten to loopback).
func NewFetcher(httpListen, httpsListen string, timeout time.Duration) *Fetcher {
	httpAddr := loopback(httpListen, "80")
	httpsAddr := loopback(httpsListen, "443")
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			target := httpAddr
			if _, port, err := net.SplitHostPort(addr); err == nil && port == "443" {
				target = httpsAddr
			}
			return dialer.DialContext(ctx, "tcp", target)
		},
		// ServerName stays empty so the Transport fills in the URL host: that
		// is the SNI the edge picks its certificate and site by.
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // our own edge on loopback
		ForceAttemptHTTP2:     true,
		DisableCompression:    true, // Accept-Encoding is set explicitly per request
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &Fetcher{client: &http.Client{
		Transport:     tr,
		Timeout:       timeout,
		CheckRedirect: sameHostRedirects,
	}}
}

// loopback turns a listen address into something a host process can dial.
func loopback(listen, defaultPort string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		port = defaultPort
		host = ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// sameHostRedirects follows a few redirects on the same host (http → https,
// trailing slash, sitemap plugins moving the index) and stops at anything
// else so the warmer never leaves the site.
func sameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errors.New("too many redirects")
	}
	if !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
		return http.ErrUseLastResponse
	}
	return nil
}

func (f *Fetcher) do(ctx context.Context, rawURL, encoding string) (*http.Response, Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, Result{}, err
	}
	req.Header.Set("User-Agent", observe.WarmerUserAgent)
	req.Header.Set("Accept", acceptHTML)
	req.Header.Set("Accept-Encoding", encoding)
	res, err := f.client.Do(req)
	if err != nil {
		return nil, Result{}, err
	}
	cs := res.Header.Get("Cache-Status")
	return res, Result{
		Status:      res.StatusCode,
		CacheStatus: cs,
		Outcome:     observe.ClassifyCacheStatus(cs),
		FinalURL:    res.Request.URL.String(),
	}, nil
}

// Get warms one URL: the response body is drained and discarded.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (Result, error) {
	res, r, err := f.do(ctx, rawURL, acceptEncoding)
	if err != nil {
		return Result{}, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, drainLimit))
	return r, nil
}

// GetBody fetches a document the warmer needs to read (robots.txt,
// sitemaps). It asks for an uncompressed body but copes with gzip in case
// the origin insists; anything else is an error.
func (f *Fetcher) GetBody(ctx context.Context, rawURL string, limit int64) (Result, []byte, error) {
	res, r, err := f.do(ctx, rawURL, "identity")
	if err != nil {
		return Result{}, nil, err
	}
	defer res.Body.Close()
	var body io.Reader = res.Body
	switch enc := strings.ToLower(res.Header.Get("Content-Encoding")); enc {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(res.Body)
		if err != nil {
			return r, nil, err
		}
		defer gz.Close()
		body = gz
	default:
		return r, nil, fmt.Errorf("unsupported content encoding %q", enc)
	}
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return r, nil, err
	}
	if int64(len(b)) > limit {
		return r, nil, fmt.Errorf("document larger than %d bytes", limit)
	}
	return r, b, nil
}
