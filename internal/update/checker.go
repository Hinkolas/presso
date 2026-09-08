package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/version"
)

// release is what the checker keeps about the newest published release.
type release struct {
	Tag         string            `json:"tag"`
	URL         string            `json:"url"`
	PublishedAt time.Time         `json:"published_at"`
	Assets      map[string]string `json:"assets"` // file name → download URL
}

// stored is the persisted checker state (platform_kv), so the System page
// knows about an update right after a restart.
type stored struct {
	Latest    *release  `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error"`
}

// minCheckGap rate-limits on-demand checks (GitHub allows 60 anonymous
// requests per hour per IP).
const minCheckGap = time.Minute

// newer reports whether tag is a release newer than current. Non-release
// versions (dev builds) never see an update.
func newer(tag, current string) bool {
	tag, current = version.ReleaseOf(tag), version.ReleaseOf(current)
	if tag == "" || current == "" || !semver.IsValid(tag) || !semver.IsValid(current) {
		return false
	}
	return semver.Compare(tag, current) > 0
}

// Run polls the feed until ctx ends. The first check happens right away so
// the System page has an answer within seconds of boot.
func (s *Service) Run(ctx context.Context) {
	if s.cfg == nil || !s.cfg.Update.Check {
		return
	}
	s.load(ctx)
	interval := s.cfg.Update.Interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.Check(ctx, false); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "update: check", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// load restores the last check result from platform_kv.
func (s *Service) load(ctx context.Context) {
	raw, err := s.st.GetKV(ctx, kvLatest)
	if err != nil || len(raw) == 0 {
		return
	}
	var st stored
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	s.mu.Lock()
	s.latest, s.checkedAt, s.checkErr = st.Latest, st.CheckedAt, st.Error
	s.mu.Unlock()
}

// Check fetches the latest release. force skips the rate limit that protects
// the background loop from being triggered too often by hand; the result is
// persisted and published as a system.update event when it changed.
func (s *Service) Check(ctx context.Context, force bool) (Status, error) {
	s.mu.Lock()
	if !force && !s.lastCheck.IsZero() && s.now().Sub(s.lastCheck) < minCheckGap {
		s.mu.Unlock()
		return s.Status(ctx), nil
	}
	s.lastCheck = s.now()
	prevTag, prevErr := "", s.checkErr
	if s.latest != nil {
		prevTag = s.latest.Tag
	}
	s.mu.Unlock()

	rel, err := s.fetchLatest(ctx)
	s.mu.Lock()
	s.checkedAt = s.now()
	if err != nil {
		s.checkErr = err.Error()
	} else {
		s.checkErr = ""
		s.latest = rel
	}
	changed := s.checkErr != prevErr || (s.latest != nil && s.latest.Tag != prevTag)
	st := stored{Latest: s.latest, CheckedAt: s.checkedAt, Error: s.checkErr}
	s.mu.Unlock()

	if b, jerr := json.Marshal(st); jerr == nil {
		if kerr := s.st.SetKV(ctx, store.SetKVParams{Key: kvLatest, Value: b, UpdatedAt: s.now()}); kerr != nil {
			slog.WarnContext(ctx, "update: persist check", "err", kerr)
		}
	}
	status := s.Status(ctx)
	if changed && s.hub != nil {
		s.hub.Publish(events.TypeSystemUpdate, status)
	}
	if err != nil {
		return status, err
	}
	if status.Available {
		slog.InfoContext(ctx, "update: a newer release is available", "current", version.Version, "latest", status.Latest)
	}
	return status, nil
}

// fetchLatest asks the GitHub API for the newest non-draft, non-prerelease
// release. Drafts are exactly the releases whose images are still building
// (the release workflow publishes last), so this never offers a half-built
// release.
func (s *Service) fetchLatest(ctx context.Context) (*release, error) {
	url := strings.TrimRight(s.cfg.Update.APIBase, "/") + "/repos/" + s.cfg.Update.Repo + "/releases/latest"
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pressod/"+version.Version)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("release feed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("release feed: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNoRelease
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("release feed: %s returned %d", url, resp.StatusCode)
	}
	var gh struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &gh); err != nil {
		return nil, fmt.Errorf("release feed: decode: %w", err)
	}
	if gh.TagName == "" || gh.Draft {
		return nil, ErrNoRelease
	}
	if version.ReleaseOf(gh.TagName) == "" {
		return nil, fmt.Errorf("release feed: %q is not a release tag", gh.TagName)
	}
	rel := &release{Tag: gh.TagName, URL: gh.HTMLURL, PublishedAt: gh.PublishedAt.UTC(), Assets: map[string]string{}}
	for _, a := range gh.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}
