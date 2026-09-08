package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/shots"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/utils"
)

// --- WP-CLI ---

// ExecResult is the outcome of a WP-CLI run.
type ExecResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"duration_ms"`
}

// exec runs `wp <args>` as the ops user inside a container.
func (s *Service) exec(ctx context.Context, containerID string, timeout time.Duration, args ...string) (docker.ExecResult, error) {
	ectx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := append([]string{"wp", "--path=" + wpPath, "--no-color"}, args...)
	return s.dk.Exec(ectx, containerID, docker.ExecOptions{
		User: opsUser, Cmd: cmd, Env: []string{"PRESSO_CLI=1", "HOME=/tmp", "WP_CLI_CACHE_DIR=/tmp/wp-cli-cache"},
		WorkingDir: wpPath,
	})
}

func (s *Service) wpInstalled(ctx context.Context, containerID string) (bool, error) {
	res, err := s.exec(ctx, containerID, time.Minute, "core", "is-installed")
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

// RunWP executes an operator-supplied WP-CLI command on a running site.
func (s *Service) RunWP(ctx context.Context, id uuid.UUID, args []string, by string) (ExecResult, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return ExecResult{}, err
	}
	if len(args) == 0 {
		return ExecResult{}, fmt.Errorf("%w: args are required", ErrInvalidInput)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--path") || strings.HasPrefix(a, "--ssh") || strings.HasPrefix(a, "--http") {
			return ExecResult{}, fmt.Errorf("%w: %s is not allowed", ErrInvalidInput, a)
		}
	}
	cid, err := s.runningContainer(ctx, row)
	if err != nil {
		return ExecResult{}, err
	}
	start := time.Now()
	res, err := s.exec(ctx, cid, 5*time.Minute, args...)
	if err != nil {
		return ExecResult{}, err
	}
	out := ExecResult{Stdout: string(res.Stdout), Stderr: string(res.Stderr), ExitCode: res.ExitCode, Truncated: res.Truncated, DurationMs: time.Since(start).Milliseconds()}
	s.logActivity(ctx, by, id, "wp.exec", "wp "+strings.Join(args, " "), map[string]any{"exit_code": res.ExitCode})
	return out, nil
}

func (s *Service) runningContainer(ctx context.Context, row store.Site) (string, error) {
	info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
	if err != nil {
		if isNotFound(err) {
			return "", ErrNotReady
		}
		return "", err
	}
	if !info.Running() {
		return "", ErrNotReady
	}
	return info.ID, nil
}

// Stats returns a live resource sample for a running site.
func (s *Service) Stats(ctx context.Context, id uuid.UUID) (docker.Stats, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return docker.Stats{}, err
	}
	cid, err := s.runningContainer(ctx, row)
	if err != nil {
		return docker.Stats{}, err
	}
	return s.dk.Stats(ctx, cid)
}

// Logs streams a running (or stopped) site's container output.
func (s *Service) Logs(ctx context.Context, id uuid.UUID, opts docker.LogOptions) (<-chan docker.LogLine, <-chan error, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
	if err != nil {
		if isNotFound(err) {
			return nil, nil, ErrNotReady
		}
		return nil, nil, err
	}
	lines, errc := s.dk.Logs(ctx, info.ID, opts)
	return lines, errc, nil
}

// --- one-click login ---

// LoginLink mints a short-lived URL that logs the operator into wp-admin as
// the site's admin user (validated by the system plugin's login module).
func (s *Service) LoginLink(ctx context.Context, id uuid.UUID, by string) (string, time.Time, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if row.Status != StatusRunning {
		return "", time.Time{}, ErrNotReady
	}
	secret, err := s.box.DecryptString(row.LoginSecretEnc)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := s.now().Add(60 * time.Second)
	nonce, err := utils.RandomToken(12)
	if err != nil {
		return "", time.Time{}, err
	}
	payload, _ := json.Marshal(map[string]any{"login": row.AdminUser, "exp": exp.Unix(), "nonce": nonce})
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	token := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	s.logActivity(ctx, by, id, "site.login", "One-click admin login link issued", nil)
	return siteURL(row.PrimaryDomain, row.TlsMode) + "/wp-login.php?presso_login=" + token, exp, nil
}

// --- cache ---

// hosts lists every hostname routed to the site.
func (s *Service) hosts(ctx context.Context, id uuid.UUID) ([]string, error) {
	domains, err := s.st.ListSiteDomains(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		out = append(out, d.Host)
	}
	return out, nil
}

// PurgeCache invalidates the site's page cache: everything when tags is
// empty, otherwise only the given surrogate keys. Tags not carrying the
// site's prefix are dropped so one site can never purge another.
func (s *Service) PurgeCache(ctx context.Context, id uuid.UUID, tags []string) error {
	if s.edge == nil {
		return nil
	}
	prefix := "s" + id.String()
	if len(tags) == 0 {
		hosts, err := s.hosts(ctx, id)
		if err != nil {
			return err
		}
		if err := s.edge.PurgeTags(ctx, []string{prefix}); err != nil {
			return err
		}
		return s.edge.PurgeHosts(ctx, hosts)
	}
	return s.edge.PurgeTags(ctx, filterTags(prefix, tags))
}

// filterTags keeps only tags inside the site's namespace, capped.
func filterTags(prefix string, tags []string) []string {
	valid := make([]string, 0, len(tags))
	for _, t := range tags {
		if t == prefix || strings.HasPrefix(t, prefix+":") {
			valid = append(valid, t)
		}
	}
	if len(valid) > 200 {
		valid = valid[:200]
	}
	return valid
}

// VerifyPurgeToken checks the bearer token the system plugin presents.
func (s *Service) VerifyPurgeToken(ctx context.Context, id uuid.UUID, token string) error {
	row, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if token == "" || !hmac.Equal(hashToken(token), row.PurgeTokenHash) {
		return ErrInvalidToken
	}
	return nil
}

// FlushObjectCache runs `wp cache flush` inside the site.
func (s *Service) FlushObjectCache(ctx context.Context, id uuid.UUID, by string) error {
	row, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	cid, err := s.runningContainer(ctx, row)
	if err != nil {
		return err
	}
	res, err := s.exec(ctx, cid, time.Minute, "cache", "flush")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("wp cache flush: %s", strings.TrimSpace(string(res.Stderr)))
	}
	s.logActivity(ctx, by, id, "cache.flush", "Object cache flushed", nil)
	return nil
}

// --- previews ---

// Screenshot captures the front page of a running site through the edge and
// stores it as the site's preview, returning the capture time.
func (s *Service) Screenshot(ctx context.Context, id uuid.UUID, by string) (time.Time, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := s.runningContainer(ctx, row); err != nil {
		return time.Time{}, err
	}
	at, err := s.shots.Refresh(ctx, row.ID, siteURL(row.PrimaryDomain, row.TlsMode))
	if err != nil {
		return time.Time{}, err
	}
	s.logActivity(ctx, by, id, "site.screenshot", "Front-page preview refreshed", nil)
	s.publishStatus(ctx, row.ID)
	return at, nil
}

// ScreenshotFile returns the stored preview path and capture time for id.
func (s *Service) ScreenshotFile(id uuid.UUID) (path string, at time.Time, ok bool) {
	at, ok = s.shots.Stat(id)
	if !ok {
		return "", time.Time{}, false
	}
	return s.shots.Path(id), at, true
}

// screenshot is the non-fatal job step variant: failures are reported on the
// job and otherwise ignored, since a missing preview never blocks a site.
func (s *Service) screenshot(ctx context.Context, row store.Site, r *jobs.Reporter) {
	if s.shots == nil {
		return
	}
	r.Step(ctx, "preview", "Capturing a preview of the front page")
	if _, err := s.shots.Refresh(ctx, row.ID, siteURL(row.PrimaryDomain, row.TlsMode)); err != nil {
		r.Warn(ctx, "preview capture failed: "+err.Error())
	}
}

// RefreshScreenshots re-captures every running site once. Errors are logged
// per site; one broken site never stops the sweep.
func (s *Service) RefreshScreenshots(ctx context.Context) {
	if s.shots == nil {
		return
	}
	rows, err := s.st.ListSitesByStatus(ctx, StatusRunning)
	if err != nil {
		slog.WarnContext(ctx, "site: list running sites for previews", "err", err)
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.shots.Refresh(ctx, row.ID, siteURL(row.PrimaryDomain, row.TlsMode)); err != nil {
			slog.WarnContext(ctx, "site: preview capture", "site", row.Slug, "err", err)
			if errors.Is(err, shots.ErrUnavailable) {
				return // the browser is down; the rest would fail the same way
			}
			continue
		}
		s.publishStatus(ctx, row.ID)
	}
}

// ScreenshotLoop refreshes previews on boot (after a grace period for the
// browser container to come up) and then every interval.
func (s *Service) ScreenshotLoop(ctx context.Context, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.RefreshScreenshots(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// publishStatus re-emits the site payload so live clients pick up a new
// screenshot_at without polling.
func (s *Service) publishStatus(ctx context.Context, id uuid.UUID) {
	if s.hub == nil {
		return
	}
	row, err := s.Get(ctx, id)
	if err != nil {
		return
	}
	if p, err := s.Payload(ctx, row); err == nil {
		s.hub.Publish(events.TypeSiteStatus, p)
	}
}

// --- edge ---

// RenderEdge is the platform.EdgeRenderer: every site that is not being
// deleted gets a route.
func (s *Service) RenderEdge(ctx context.Context) (string, error) {
	rows, err := s.st.ListSites(ctx)
	if err != nil {
		return "", err
	}
	domains, err := s.st.ListAllDomains(ctx)
	if err != nil {
		return "", err
	}
	bySite := map[uuid.UUID][]string{}
	for _, d := range domains {
		if d.Kind == "primary" {
			bySite[d.SiteID] = append([]string{d.Host}, bySite[d.SiteID]...)
		} else {
			bySite[d.SiteID] = append(bySite[d.SiteID], d.Host)
		}
	}
	routes := make([]edge.SiteRoute, 0, len(rows))
	for _, r := range rows {
		if r.Status == StatusDeleting {
			continue
		}
		set := parseSettings(r.Settings)
		routes = append(routes, edge.SiteRoute{
			ID: r.ID.String(), Slug: r.Slug, Hosts: bySite[r.ID], TLSMode: r.TlsMode,
			Upstream:  fmt.Sprintf("%s:%d", NetworkAlias(r.ID.String()), containerPort),
			PageCache: r.PageCache, CacheTTL: time.Duration(set.CacheTTL) * time.Second,
		})
	}
	_, cacheAdminPort, _ := splitHostPort(s.cfg.Edge.CacheAdminListen)
	return edge.Render(routes, edge.Options{
		ACMEEmail:        s.cfg.Edge.ACMEEmail,
		DefaultTTL:       s.cfg.Edge.DefaultCacheTTL,
		CacheAdminListen: ":" + strconv.Itoa(cacheAdminPort),
	}, s.now())
}

func splitHostPort(addr string) (string, int, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	return h, port, err
}

// --- updates ---

// UpdateInput carries the PATCH body; nil pointers mean "unchanged".
type UpdateInput struct {
	Name       *string
	PHPVersion *string
	PageCache  *bool
	FileMods   *string
	TLSMode    *string
	Profile    *string
	Settings   map[string]json.RawMessage
	UpdatedBy  string
}

// Update applies configuration changes. Container-affecting changes return a
// recreate job; edge-only changes are pushed synchronously.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (Site, *store.Job, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return Site{}, nil, err
	}
	if active, err := s.ActiveJob(ctx, id); err != nil {
		return Site{}, nil, err
	} else if active != nil {
		return Site{}, nil, ErrJobRunning
	}
	oldSettings := parseSettings(row.Settings)
	newSettings := oldSettings
	if in.Settings != nil {
		merged, _ := json.Marshal(oldSettings)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(merged, &m)
		for k, v := range in.Settings {
			if _, ok := m[k]; !ok {
				return Site{}, nil, fmt.Errorf("%w: unknown setting %q", ErrInvalidInput, k)
			}
			m[k] = v
		}
		b, _ := json.Marshal(m)
		if err := json.Unmarshal(b, &newSettings); err != nil {
			return Site{}, nil, fmt.Errorf("%w: settings: %v", ErrInvalidInput, err)
		}
		newSettings = newSettings.Normalize()
		if err := newSettings.Validate(); err != nil {
			return Site{}, nil, err
		}
	}
	name, php, runtime, image := row.Name, row.PhpVersion, row.Runtime, row.Image
	fileMods, tls, profile, pageCache := row.FileMods, row.TlsMode, row.Profile, row.PageCache
	if in.Name != nil {
		if name = strings.TrimSpace(*in.Name); name == "" {
			return Site{}, nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
		}
	}
	if in.PHPVersion != nil && *in.PHPVersion != row.PhpVersion {
		// A PHP change is re-resolved against the site's WordPress release, so
		// it can also move the site between runtimes (image swap).
		if row.WpVersion == "" {
			return Site{}, nil, fmt.Errorf("%w: the site's WordPress version is not known yet; start it once so it can be detected", ErrInvalidInput)
		}
		res, err := s.catalog.Resolve(row.WpVersion, *in.PHPVersion)
		if err != nil {
			return Site{}, nil, err
		}
		php, runtime, image = res.PHP, res.Runtime, res.Image
	}
	if in.FileMods != nil {
		if *in.FileMods != "locked" && *in.FileMods != "open" {
			return Site{}, nil, fmt.Errorf("%w: file_mods must be locked or open", ErrInvalidInput)
		}
		fileMods = *in.FileMods
	}
	if in.TLSMode != nil {
		if *in.TLSMode != "auto" && *in.TLSMode != "off" {
			return Site{}, nil, fmt.Errorf("%w: tls_mode must be auto or off", ErrInvalidInput)
		}
		tls = *in.TLSMode
		if isLocalhost(row.PrimaryDomain) {
			tls = "off"
		}
	}
	if in.Profile != nil {
		if *in.Profile != "prod" && *in.Profile != "dev" {
			return Site{}, nil, fmt.Errorf("%w: profile must be prod or dev", ErrInvalidInput)
		}
		profile = *in.Profile
	}
	if in.PageCache != nil {
		pageCache = *in.PageCache
	}
	recreate := image != row.Image || fileMods != row.FileMods || profile != row.Profile || tls != row.TlsMode ||
		pageCache != row.PageCache || requiresRecreate(oldSettings, newSettings)
	edgeOnly := !recreate && (newSettings.CacheTTL != oldSettings.CacheTTL)

	updated, err := s.st.UpdateSiteConfig(ctx, store.UpdateSiteConfigParams{
		Name: name, PhpVersion: php, Image: image, Runtime: runtime, PageCache: pageCache, FileMods: fileMods,
		TlsMode: tls, Profile: profile, Settings: newSettings.encode(), UpdatedAt: s.now(), ID: id,
	})
	if err != nil {
		return Site{}, nil, err
	}
	s.logActivity(ctx, in.UpdatedBy, id, "site.update", "Settings updated", nil)

	var job *store.Job
	if recreate && (row.Status == StatusRunning || row.Status == StatusStopped || row.Status == StatusError) {
		j, err := s.jobs.Enqueue(ctx, JobRecreate, nullID(id), nil, in.UpdatedBy)
		if err != nil {
			return Site{}, nil, err
		}
		job = &j
	} else if edgeOnly {
		if err := s.platform.ApplyEdge(ctx); err != nil {
			return Site{}, nil, err
		}
	}
	payload, err := s.Payload(ctx, updated)
	if err != nil {
		return Site{}, nil, err
	}
	s.publish(payload)
	return payload, job, nil
}

// Activity lists audit entries for one site.
func (s *Service) Activity(ctx context.Context, id uuid.UUID, limit int64) ([]store.Activity, error) {
	return s.st.ListActivityBySite(ctx, store.ListActivityBySiteParams{SiteID: nullID(id), Limit: limit})
}

// ExecWP runs a WP-CLI command on behalf of an internal worker (cron,
// hygiene); unlike RunWP it does not write an activity entry per call.
func (s *Service) ExecWP(ctx context.Context, id uuid.UUID, timeout time.Duration, args ...string) (ExecResult, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return ExecResult{}, err
	}
	cid, err := s.runningContainer(ctx, row)
	if err != nil {
		return ExecResult{}, err
	}
	start := time.Now()
	res, err := s.exec(ctx, cid, timeout, args...)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{Stdout: string(res.Stdout), Stderr: string(res.Stderr), ExitCode: res.ExitCode, Truncated: res.Truncated, DurationMs: time.Since(start).Milliseconds()}, nil
}

// LogActivity records an audit entry from another package (workers).
func (s *Service) LogActivity(ctx context.Context, siteID uuid.UUID, kind, message string, meta any) {
	s.logActivity(ctx, "", siteID, kind, message, meta)
}
