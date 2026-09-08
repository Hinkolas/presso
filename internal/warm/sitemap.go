package warm

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	maxSitemaps  = 50      // child documents fetched per run
	maxDepth     = 3       // index → index → urlset
	sitemapLimit = 8 << 20 // bytes per document
	robotsLimit  = 64 << 10
)

// Default sitemap locations when robots.txt does not name one: WordPress
// core, Yoast / Rank Math, and the generic name.
var defaultSitemaps = []string{"/wp-sitemap.xml", "/sitemap_index.xml", "/sitemap.xml"}

var robotsSitemapRe = regexp.MustCompile(`(?im)^\s*sitemap:\s*(\S+)`)

type sitemapIndex struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

type urlSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// Discover returns the page URLs to warm for a site: the front page first,
// then every same-host URL from the site's sitemap, deduplicated and capped
// at maxURLs. source names the sitemap document that was used, or is empty
// when none was found (the result is then just the front page).
func Discover(ctx context.Context, f *Fetcher, base *url.URL, maxURLs int) (urls []string, source string) {
	home := pageURL(base, "/")
	urls = []string{home}
	seen := map[string]struct{}{"/": {}}
	if maxURLs <= 1 {
		return urls, ""
	}

	var pages []string
	for _, root := range roots(ctx, f, base) {
		if ctx.Err() != nil {
			break
		}
		children, locs, res, ok := fetchSitemap(ctx, f, root)
		if !ok {
			continue
		}
		source = res.FinalURL
		pages = append(pages, locs...)
		// Breadth-first over child sitemaps, bounded in count and depth.
		queue := children
		fetched := 1
		for depth := 1; depth < maxDepth && len(queue) > 0 && fetched < maxSitemaps && ctx.Err() == nil; depth++ {
			var next []string
			for _, child := range queue {
				if fetched >= maxSitemaps || ctx.Err() != nil {
					break
				}
				if !sameHost(child, base) {
					continue
				}
				fetched++
				grand, locs, _, ok := fetchSitemap(ctx, f, child)
				if !ok {
					continue
				}
				pages = append(pages, locs...)
				next = append(next, grand...)
			}
			queue = next
		}
		break
	}

	for _, loc := range pages {
		u, err := url.Parse(strings.TrimSpace(loc))
		if err != nil || !sameHost(loc, base) || strings.HasSuffix(strings.ToLower(u.Path), ".gz") {
			continue
		}
		key := u.EscapedPath()
		if key == "" {
			key = "/"
		}
		if u.RawQuery != "" {
			key += "?" + u.RawQuery
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		// Rebuilt on the site's canonical scheme and host: that is what the
		// edge keys on, whatever the sitemap printed.
		urls = append(urls, pageURL(base, key))
		if len(urls) >= maxURLs {
			break
		}
	}
	return urls, source
}

// roots lists candidate sitemap documents in priority order: whatever
// robots.txt announces (a plugin that owns robots.txt owns the sitemap), then
// the well-known names.
func roots(ctx context.Context, f *Fetcher, base *url.URL) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(u string) {
		if _, dup := seen[u]; !dup {
			seen[u] = struct{}{}
			out = append(out, u)
		}
	}
	if res, body, err := f.GetBody(ctx, pageURL(base, "/robots.txt"), robotsLimit); err == nil && res.Status == 200 {
		for _, u := range parseRobotsSitemaps(body) {
			if sameHost(u, base) {
				add(u)
			}
		}
	}
	for _, p := range defaultSitemaps {
		add(pageURL(base, p))
	}
	return out
}

func parseRobotsSitemaps(body []byte) []string {
	var out []string
	for _, m := range robotsSitemapRe.FindAllSubmatch(body, -1) {
		out = append(out, string(m[1]))
	}
	return out
}

// fetchSitemap returns the child sitemaps and page locations of one
// document. ok is false when it is not a sitemap (missing, HTML, malformed).
func fetchSitemap(ctx context.Context, f *Fetcher, rawURL string) (children, pages []string, res Result, ok bool) {
	res, body, err := f.GetBody(ctx, rawURL, sitemapLimit)
	if err != nil || res.Status != 200 {
		return nil, nil, res, false
	}
	children, pages, err = parseSitemap(body)
	if err != nil {
		return nil, nil, res, false
	}
	return children, pages, res, true
}

// parseSitemap decodes either a <sitemapindex> or a <urlset>, whatever the
// namespace; anything else (an HTML error page with a 200) is an error.
func parseSitemap(body []byte) (children, pages []string, err error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "sitemapindex":
			var idx sitemapIndex
			if err := dec.DecodeElement(&idx, &se); err != nil {
				return nil, nil, err
			}
			for _, s := range idx.Sitemaps {
				children = append(children, strings.TrimSpace(s.Loc))
			}
			return children, nil, nil
		case "urlset":
			var set urlSet
			if err := dec.DecodeElement(&set, &se); err != nil {
				return nil, nil, err
			}
			for _, u := range set.URLs {
				pages = append(pages, strings.TrimSpace(u.Loc))
			}
			return nil, pages, nil
		default:
			return nil, nil, fmt.Errorf("not a sitemap: root element <%s>", se.Name.Local)
		}
	}
}

// sameHost accepts http(s) URLs whose host matches the site's, port aside.
func sameHost(rawURL string, base *url.URL) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return strings.EqualFold(u.Hostname(), base.Hostname())
}

// pageURL builds an absolute URL on the site's canonical origin from a
// path (optionally with a query string).
func pageURL(base *url.URL, pathAndQuery string) string {
	return base.Scheme + "://" + base.Host + pathAndQuery
}
