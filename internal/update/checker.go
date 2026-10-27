package update

import (
	"context"
	"encoding/json"
	"errors"
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

// release is what the checker keeps about the newest published release on
// the channel.
type release struct {
	Tag         string            `json:"tag"`
	URL         string            `json:"url"`
	PublishedAt time.Time         `json:"published_at"`
	Assets      map[string]string `json:"assets"` // file name → download URL
}

// stored is the persisted checker state (platform_kv), so the Updates page
// knows about an update right after a restart. Channel is the channel the
// result belongs to; a result for another channel is not loaded.
type stored struct {
	Channel   string    `json:"channel"`
	Latest    *release  `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error"`
	ErrorKind string    `json:"error_kind"`
}

const (
	// minCheckGap rate-limits on-demand checks (GitHub allows 60 anonymous
	// requests per hour per IP).
	minCheckGap = time.Minute
	// feedPage is how many recent releases a check reads; the newest release
	// of either channel is always among them.
	feedPage = 30
)

// Error kinds of a failed check, so the UI can say what went wrong.
const (
	kindOffline     = "offline"      // no connection to the feed
	kindNotFound    = "not_found"    // repository missing or private
	kindRateLimited = "rate_limited" // GitHub's anonymous rate limit
	kindUnavailable = "unavailable"  // the feed answered with an error
	kindInvalid     = "invalid"      // the feed answered with something unreadable
	kindNoRelease   = "no_release"   // nothing published on the channel yet
)

// feedError is a failed check with its kind.
type feedError struct {
	kind string
	err  error
}

func (e *feedError) Error() string { return e.err.Error() }
func (e *feedError) Unwrap() error { return e.err }

func feedErr(kind, format string, a ...any) error {
	return &feedError{kind: kind, err: fmt.Errorf("release feed: "+format, a...)}
}

// errorKind classifies a check error.
func errorKind(err error) string {
	var fe *feedError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNoRelease):
		return kindNoRelease
	case errors.As(err, &fe):
		return fe.kind
	default:
		return kindUnavailable
	}
}

// newer reports whether tag is a release newer than current. Non-release
// versions (dev builds) never see an update.
func newer(tag, current string) bool {
	tag, current = version.ReleaseOf(tag), version.ReleaseOf(current)
	if tag == "" || current == "" || !semver.IsValid(tag) || !semver.IsValid(current) {
		return false
	}
	return semver.Compare(tag, current) > 0
}

// Run polls the feed until ctx ends and starts automatic updates. The first
// check happens right away so the Updates page has an answer within seconds
// of boot. With UPDATE_CHECK=false it only restores the last result; checks
// and updates then happen by hand.
func (s *Service) Run(ctx context.Context) {
	if s.cfg == nil {
		return
	}
	s.load(ctx)
	if !s.cfg.Update.Check {
		return
	}
	interval := s.cfg.Update.Interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		status, err := s.Check(ctx, false)
		if err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "update: check", "err", err)
		}
		if err == nil {
			s.autoUpdate(ctx, status)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// load restores the last check result from platform_kv. Results stored
// before channels existed are stable results.
func (s *Service) load(ctx context.Context) {
	raw, err := s.st.GetKV(ctx, kvLatest)
	if err != nil || len(raw) == 0 {
		return
	}
	var st stored
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	if st.Channel == "" {
		st.Channel = ChannelStable
	}
	if st.Channel != s.settings(ctx).Channel {
		return
	}
	s.mu.Lock()
	s.latest, s.checkedAt, s.checkErr, s.checkKind = st.Latest, st.CheckedAt, st.Error, st.ErrorKind
	s.mu.Unlock()
}

// Check fetches the newest release on the channel. force skips the rate
// limit that protects the background loop from being triggered too often by
// hand; the result is persisted and published as a system.update event when
// it changed. A failed check keeps the last known release.
func (s *Service) Check(ctx context.Context, force bool) (Status, error) {
	channel := s.settings(ctx).Channel
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

	rel, err := s.fetchLatest(ctx, channel)
	s.mu.Lock()
	s.checkedAt = s.now()
	if err != nil {
		s.checkErr, s.checkKind = err.Error(), errorKind(err)
	} else {
		s.checkErr, s.checkKind = "", ""
		s.latest = rel
	}
	changed := s.checkErr != prevErr || (s.latest != nil && s.latest.Tag != prevTag)
	st := stored{Channel: channel, Latest: s.latest, CheckedAt: s.checkedAt, Error: s.checkErr, ErrorKind: s.checkKind}
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

// fetchLatest reads the recent releases from the GitHub API and returns the
// highest release tag the channel offers. Drafts are skipped (and invisible
// without a token anyway): they are exactly the releases whose images are
// still building, as the release workflow publishes last. So are tags that
// are not engine releases, such as the sources-<version> prereleases.
func (s *Service) fetchLatest(ctx context.Context, channel string) (*release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", strings.TrimRight(s.cfg.Update.APIBase, "/"), s.cfg.Update.Repo, feedPage)
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, feedErr(kindOffline, "%v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, feedErr(kindOffline, "%v", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, feedErr(kindNotFound, "repository %s not found (renamed, deleted or private)", s.cfg.Update.Repo)
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, feedErr(kindRateLimited, "GitHub's rate limit is exhausted; the next check tries again")
	case resp.StatusCode != http.StatusOK:
		return nil, feedErr(kindUnavailable, "%s returned %d", url, resp.StatusCode)
	}
	var list []struct {
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
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, feedErr(kindInvalid, "decode: %v", err)
	}
	var best *release
	for _, gh := range list {
		tag := version.ReleaseOf(gh.TagName)
		if gh.Draft || tag == "" || !semver.IsValid(tag) {
			continue
		}
		pre := gh.Prerelease || version.IsPrerelease(tag)
		if pre && channel != ChannelBeta {
			continue
		}
		if best != nil && semver.Compare(tag, best.Tag) <= 0 {
			continue
		}
		best = &release{Tag: tag, URL: gh.HTMLURL, PublishedAt: gh.PublishedAt.UTC(), Assets: map[string]string{}}
		for _, a := range gh.Assets {
			best.Assets[a.Name] = a.URL
		}
	}
	if best == nil {
		return nil, ErrNoRelease
	}
	return best, nil
}
