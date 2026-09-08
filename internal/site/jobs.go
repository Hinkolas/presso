package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/store"
)

func isNotFound(err error) bool { return errors.Is(err, docker.ErrNotFound) }

// enqueue guards the one-job-per-site rule at submission time (the runner
// also serializes execution) and returns ErrJobRunning when busy.
func (s *Service) enqueue(ctx context.Context, row store.Site, kind string, args any, by string) (store.Job, error) {
	if active, err := s.ActiveJob(ctx, row.ID); err != nil {
		return store.Job{}, err
	} else if active != nil {
		return store.Job{}, ErrJobRunning
	}
	return s.jobs.Enqueue(ctx, kind, nullID(row.ID), args, by)
}

// Start, Stop, Restart, Recreate enqueue the lifecycle jobs.
func (s *Service) Start(ctx context.Context, id uuid.UUID, by string) (store.Job, error) {
	return s.lifecycle(ctx, id, JobStart, by, StatusStopped, StatusError, StatusRunning)
}
func (s *Service) Stop(ctx context.Context, id uuid.UUID, by string) (store.Job, error) {
	return s.lifecycle(ctx, id, JobStop, by, StatusRunning, StatusError)
}
func (s *Service) Restart(ctx context.Context, id uuid.UUID, by string) (store.Job, error) {
	return s.lifecycle(ctx, id, JobRestart, by, StatusRunning, StatusStopped, StatusError)
}
func (s *Service) Recreate(ctx context.Context, id uuid.UUID, by string) (store.Job, error) {
	return s.lifecycle(ctx, id, JobRecreate, by, StatusRunning, StatusStopped, StatusError)
}

func (s *Service) lifecycle(ctx context.Context, id uuid.UUID, kind, by string, allowed ...string) (store.Job, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return store.Job{}, err
	}
	if !containsString(allowed, row.Status) {
		return store.Job{}, fmt.Errorf("%w: site is %s", ErrNotReady, row.Status)
	}
	return s.enqueue(ctx, row, kind, nil, by)
}

// Retry re-runs provisioning for a site stuck in error before it ever ran,
// reusing the original job's (encrypted) install arguments.
func (s *Service) Retry(ctx context.Context, id uuid.UUID, by string) (store.Job, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return store.Job{}, err
	}
	if row.Status != StatusError && row.Status != StatusProvisioning {
		return store.Job{}, ErrNotRetryable
	}
	history, err := s.st.ListJobsBySite(ctx, store.ListJobsBySiteParams{SiteID: nullID(id), Limit: 50})
	if err != nil {
		return store.Job{}, err
	}
	var args json.RawMessage
	for _, j := range history {
		if j.Kind == JobProvision {
			args = json.RawMessage(j.Args)
			break
		}
	}
	if args == nil {
		return store.Job{}, ErrNotRetryable
	}
	return s.enqueue(ctx, row, JobProvision, args, by)
}

// Delete marks the site for removal and enqueues the teardown. confirm must
// equal the slug.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, confirm string, keepVolume bool, by string) (store.Job, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return store.Job{}, err
	}
	if confirm != row.Slug {
		return store.Job{}, ErrWrongConfirm
	}
	if active, err := s.ActiveJob(ctx, id); err != nil {
		return store.Job{}, err
	} else if active != nil {
		return store.Job{}, ErrJobRunning
	}
	s.setStatus(ctx, id, StatusDeleting, "deletion queued")
	job, err := s.jobs.Enqueue(ctx, JobDelete, nullID(id), map[string]bool{"keep_volume": keepVolume}, by)
	if err != nil {
		s.setStatus(ctx, id, StatusError, "could not queue deletion: "+err.Error())
		return store.Job{}, err
	}
	s.logActivity(ctx, by, id, "site.delete", fmt.Sprintf("Deletion of %s requested", row.Slug), map[string]bool{"keep_volume": keepVolume})
	return job, nil
}

// --- job handlers ---

func (s *Service) runProvision(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	row, err := s.Get(ctx, job.SiteID.UUID)
	if err != nil {
		return err
	}
	var args provisionArgs
	_ = json.Unmarshal([]byte(job.Args), &args)
	fail := func(step string, err error) error {
		s.setStatus(ctx, row.ID, StatusError, step+": "+err.Error())
		return fmt.Errorf("%s: %w", step, err)
	}
	s.setStatus(ctx, row.ID, StatusProvisioning, "provisioning")
	d, err := s.decrypt(row)
	if err != nil {
		return fail("secrets", err)
	}

	r.Step(ctx, "image", fmt.Sprintf("Ensuring runtime image %s (WordPress %s, PHP %s, %s)", row.Image, row.WpVersion, row.PhpVersion, row.Runtime))
	if err := s.dk.EnsureImage(ctx, row.Image, func(line string) { r.Info(ctx, line) }); err != nil {
		return fail("image", err)
	}

	r.Step(ctx, "database", "Creating database "+row.DbName)
	if err := s.platform.CreateSiteDatabase(ctx, row.DbName, row.DbUser, d.dbPassword); err != nil {
		return fail("database", err)
	}

	r.Step(ctx, "redis", "Creating Redis ACL user "+row.RedisUser)
	if err := s.platform.ApplyRedisACL(ctx, row.RedisUser, d.redisPassword, platform.RedisKeyPrefix(row.ID.String())); err != nil {
		return fail("redis", err)
	}

	r.Step(ctx, "volume", "Ensuring wp-content volume "+row.VolumeName)
	if err := s.dk.EnsureVolume(ctx, row.VolumeName, siteLabels(row)); err != nil {
		return fail("volume", err)
	}

	r.Step(ctx, "container", "Creating container "+ContainerName(row.Slug))
	cid, err := s.ensureContainer(ctx, row, d, false)
	if err != nil {
		return fail("container", err)
	}
	if err := s.dk.StartContainer(ctx, cid); err != nil {
		return fail("container", err)
	}

	r.Step(ctx, "health", "Waiting for the container to become healthy")
	if err := s.waitHealthy(ctx, cid, 3*time.Minute); err != nil {
		return fail("health", err)
	}

	r.Step(ctx, "wordpress", "Installing WordPress")
	installed, err := s.wpInstalled(ctx, cid)
	if err != nil {
		return fail("wordpress", err)
	}
	if installed {
		r.Info(ctx, "WordPress is already installed, skipping")
	} else {
		pw, err := s.box.DecryptString(args.AdminPasswordEnc)
		if err != nil || pw == "" {
			return fail("wordpress", errors.New("the admin password from the original request is unavailable; delete the site and create it again"))
		}
		res, err := s.exec(ctx, cid, 5*time.Minute, "core", "install",
			"--url="+siteURL(row.PrimaryDomain, row.TlsMode), "--title="+args.Title,
			"--admin_user="+row.AdminUser, "--admin_password="+pw, "--admin_email="+row.AdminEmail, "--skip-email")
		if err != nil {
			return fail("wordpress", err)
		}
		if res.ExitCode != 0 {
			return fail("wordpress", fmt.Errorf("wp core install failed: %s", strings.TrimSpace(string(res.Stderr)+string(res.Stdout))))
		}
		r.Info(ctx, strings.TrimSpace(string(res.Stdout)))
		if res, err := s.exec(ctx, cid, time.Minute, "rewrite", "structure", "/%postname%/"); err == nil && res.ExitCode == 0 {
			r.Info(ctx, "Pretty permalinks enabled")
		} else {
			r.Warn(ctx, "could not set permalink structure")
		}
	}
	s.detectWPVersion(ctx, &row, cid)

	r.Step(ctx, "edge", "Publishing route at the edge")
	if err := s.platform.ApplyEdge(ctx); err != nil {
		return fail("edge", err)
	}

	r.Step(ctx, "warm", "Warming the front page")
	s.warm(ctx, row, r)
	s.screenshot(ctx, row, r)

	s.setStatus(ctx, row.ID, StatusRunning, "")
	s.logActivity(ctx, job.CreatedBy, row.ID, "site.provisioned", fmt.Sprintf("Site %s is running at %s", row.Slug, siteURL(row.PrimaryDomain, row.TlsMode)), nil)
	return nil
}

// detectWPVersion records what `wp core version` reports so the row reflects
// the release actually running (images carry a pinned release; sites created
// before versions were tracked start out empty). Best effort: a failure
// leaves the row untouched.
func (s *Service) detectWPVersion(ctx context.Context, row *store.Site, containerID string) {
	res, err := s.exec(ctx, containerID, time.Minute, "core", "version")
	if err != nil || res.ExitCode != 0 {
		return
	}
	v := strings.TrimSpace(string(res.Stdout))
	if v == "" || v == row.WpVersion || strings.ContainsAny(v, " \n") {
		return
	}
	if err := s.st.SetSiteWPVersion(ctx, store.SetSiteWPVersionParams{WpVersion: v, UpdatedAt: s.now(), ID: row.ID}); err == nil {
		row.WpVersion = v
	}
}

// warm issues one request through the edge, on the scheme and host visitors
// use, so the first visitor gets a hit. Best effort: failures are reported,
// not fatal. Without a warmer the step is a no-op.
func (s *Service) warm(ctx context.Context, row store.Site, r *jobs.Reporter) {
	if s.warmer == nil {
		return
	}
	status, cacheStatus, err := s.warmer.Fetch(ctx, siteURL(row.PrimaryDomain, row.TlsMode)+"/")
	if err != nil {
		r.Warn(ctx, "warm request failed: "+err.Error())
		return
	}
	r.Infof(ctx, "front page responded %d (cache: %s)", status, firstNonEmpty(cacheStatus, "n/a"))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Service) runStart(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	row, err := s.Get(ctx, job.SiteID.UUID)
	if err != nil {
		return err
	}
	r.Step(ctx, "start", "Starting container")
	info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
	switch {
	case isNotFound(err), err == nil && info.Image != row.Image:
		// A stopped site whose row moved to a newer runtime image (engine
		// update rollout) gets its container rebuilt on the way up.
		if err == nil {
			r.Info(ctx, "runtime image changed, recreating the container on "+row.Image)
		} else {
			r.Info(ctx, "container missing, recreating it")
		}
		if err := s.dk.EnsureImage(ctx, row.Image, func(line string) { r.Info(ctx, line) }); err != nil {
			s.setStatus(ctx, row.ID, StatusError, "image: "+err.Error())
			return err
		}
		d, derr := s.decrypt(row)
		if derr != nil {
			return derr
		}
		cid, cerr := s.ensureContainer(ctx, row, d, false)
		if cerr != nil {
			return cerr
		}
		info = &docker.ContainerInfo{ID: cid}
	case err != nil:
		return err
	}
	if err := s.dk.StartContainer(ctx, info.ID); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "start: "+err.Error())
		return err
	}
	r.Step(ctx, "health", "Waiting for the container to become healthy")
	if err := s.waitHealthy(ctx, info.ID, 2*time.Minute); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "health: "+err.Error())
		return err
	}
	if row.WpVersion == "" {
		s.detectWPVersion(ctx, &row, info.ID)
	}
	s.screenshot(ctx, row, r)
	s.setStatus(ctx, row.ID, StatusRunning, "")
	s.logActivity(ctx, job.CreatedBy, row.ID, "site.start", "Site started", nil)
	return nil
}

func (s *Service) runStop(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	row, err := s.Get(ctx, job.SiteID.UUID)
	if err != nil {
		return err
	}
	r.Step(ctx, "stop", "Stopping container")
	info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
	if err != nil && !isNotFound(err) {
		return err
	}
	if info != nil {
		if err := s.dk.StopContainer(ctx, info.ID, 10*time.Second); err != nil {
			return err
		}
	}
	s.setStatus(ctx, row.ID, StatusStopped, "stopped by operator")
	s.logActivity(ctx, job.CreatedBy, row.ID, "site.stop", "Site stopped", nil)
	return nil
}

func (s *Service) runRestart(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	row, err := s.Get(ctx, job.SiteID.UUID)
	if err != nil {
		return err
	}
	r.Step(ctx, "restart", "Restarting container")
	info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
	if err != nil {
		return err
	}
	if err := s.dk.RestartContainer(ctx, info.ID, 10*time.Second); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "restart: "+err.Error())
		return err
	}
	r.Step(ctx, "health", "Waiting for the container to become healthy")
	if err := s.waitHealthy(ctx, info.ID, 2*time.Minute); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "health: "+err.Error())
		return err
	}
	s.setStatus(ctx, row.ID, StatusRunning, "")
	s.logActivity(ctx, job.CreatedBy, row.ID, "site.restart", "Site restarted", nil)
	return nil
}

// runRecreate replaces the container with one rendered from the current row
// (new image, PHP settings, limits) and refreshes the edge.
func (s *Service) runRecreate(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	row, err := s.Get(ctx, job.SiteID.UUID)
	if err != nil {
		return err
	}
	d, err := s.decrypt(row)
	if err != nil {
		return err
	}
	r.Step(ctx, "image", fmt.Sprintf("Ensuring runtime image %s (WordPress %s, PHP %s, %s)", row.Image, row.WpVersion, row.PhpVersion, row.Runtime))
	if err := s.dk.EnsureImage(ctx, row.Image, func(line string) { r.Info(ctx, line) }); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "image: "+err.Error())
		return err
	}
	r.Step(ctx, "container", "Recreating container")
	cid, err := s.ensureContainer(ctx, row, d, true)
	if err != nil {
		s.setStatus(ctx, row.ID, StatusError, "container: "+err.Error())
		return err
	}
	if err := s.dk.StartContainer(ctx, cid); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "container: "+err.Error())
		return err
	}
	r.Step(ctx, "health", "Waiting for the container to become healthy")
	if err := s.waitHealthy(ctx, cid, 3*time.Minute); err != nil {
		s.setStatus(ctx, row.ID, StatusError, "health: "+err.Error())
		return err
	}
	s.detectWPVersion(ctx, &row, cid)
	// A recreate is how a site moves to a newer runtime image, and with it
	// possibly a newer WordPress release; the database upgrade that release
	// may need runs here (idempotent, a no-op when nothing changed).
	r.Step(ctx, "wordpress", "Running the WordPress database upgrade")
	if res, err := s.exec(ctx, cid, 3*time.Minute, "core", "update-db"); err != nil {
		r.Warn(ctx, "wp core update-db failed: "+err.Error())
	} else if res.ExitCode != 0 {
		r.Warn(ctx, "wp core update-db exited "+strconv.Itoa(res.ExitCode)+": "+strings.TrimSpace(string(res.Stderr)))
	} else if line := strings.TrimSpace(string(res.Stdout)); line != "" {
		r.Info(ctx, line)
	}
	r.Step(ctx, "edge", "Refreshing the edge and purging the page cache")
	if err := s.platform.ApplyEdge(ctx); err != nil {
		r.Warn(ctx, "edge apply failed: "+err.Error())
	}
	if err := s.PurgeCache(ctx, row.ID, nil); err != nil {
		r.Warn(ctx, "cache purge failed: "+err.Error())
	}
	s.screenshot(ctx, row, r)
	s.setStatus(ctx, row.ID, StatusRunning, "")
	s.logActivity(ctx, job.CreatedBy, row.ID, "site.recreate", "Container recreated with current settings", nil)
	return nil
}

func (s *Service) runDelete(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	row, err := s.Get(ctx, job.SiteID.UUID)
	if err != nil {
		return err
	}
	var args struct {
		KeepVolume bool `json:"keep_volume"`
	}
	_ = json.Unmarshal([]byte(job.Args), &args)
	hosts, _ := s.hosts(ctx, row.ID)

	r.Step(ctx, "edge", "Removing the route from the edge")
	if err := s.platform.ApplyEdge(ctx); err != nil {
		r.Warn(ctx, "edge apply failed: "+err.Error())
	}
	if s.edge != nil && len(hosts) > 0 {
		if err := s.edge.PurgeHosts(ctx, hosts); err != nil {
			r.Warn(ctx, "cache purge failed: "+err.Error())
		}
	}

	r.Step(ctx, "container", "Removing container")
	if info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug)); err == nil {
		if err := s.dk.RemoveContainer(ctx, info.ID, true); err != nil {
			return s.deleteFailed(ctx, row.ID, "container", err)
		}
	} else if !isNotFound(err) {
		return s.deleteFailed(ctx, row.ID, "container", err)
	}

	r.Step(ctx, "database", "Dropping database and user")
	if err := s.platform.DropSiteDatabase(ctx, row.DbName, row.DbUser); err != nil {
		return s.deleteFailed(ctx, row.ID, "database", err)
	}

	r.Step(ctx, "redis", "Removing Redis ACL user")
	if err := s.platform.DeleteRedisACL(ctx, row.RedisUser); err != nil {
		r.Warn(ctx, "redis: "+err.Error())
	}

	if args.KeepVolume {
		r.Info(ctx, "Keeping volume "+row.VolumeName)
	} else {
		r.Step(ctx, "volume", "Removing volume "+row.VolumeName)
		if err := s.dk.RemoveVolume(ctx, row.VolumeName); err != nil {
			return s.deleteFailed(ctx, row.ID, "volume", err)
		}
	}

	r.Step(ctx, "record", "Deleting the site record")
	if _, err := s.st.DeleteSite(ctx, row.ID); err != nil {
		return s.deleteFailed(ctx, row.ID, "record", err)
	}
	if err := s.shots.Remove(row.ID); err != nil {
		r.Warn(ctx, "preview: "+err.Error())
	}
	s.observed.Delete(row.ID)
	if s.hub != nil {
		s.hub.Publish(events.TypeSiteDeleted, map[string]string{"id": row.ID.String(), "slug": row.Slug})
	}
	s.logActivity(ctx, job.CreatedBy, uuid.Nil, "site.deleted", fmt.Sprintf("Site %s deleted", row.Slug), map[string]any{"slug": row.Slug, "keep_volume": args.KeepVolume})
	return nil
}

// observerOwned reports whether the site's error status was set by
// ObserveContainer (as opposed to a failed job).
func observerOwned(row store.Site) bool {
	if row.Status != StatusError {
		return false
	}
	for _, p := range []string{"container exited", "container missing", "container is unhealthy", "container stopped"} {
		if strings.HasPrefix(row.StatusMessage, p) {
			return true
		}
	}
	return false
}

func (s *Service) deleteFailed(ctx context.Context, id uuid.UUID, step string, err error) error {
	s.setStatus(ctx, id, StatusError, "delete "+step+": "+err.Error())
	return fmt.Errorf("%s: %w", step, err)
}

// --- observation hook (status watcher) ---

// ObserveContainer folds an inspected container state into the site: the
// live health is kept in memory and the persisted status follows crashes
// and external stops while no job is manipulating the site.
func (s *Service) ObserveContainer(ctx context.Context, siteID uuid.UUID, info *docker.ContainerInfo) {
	row, err := s.Get(ctx, siteID)
	if err != nil {
		return
	}
	prev, _ := s.observed.Load(siteID)
	cur := observedState{State: "missing", Health: "none"}
	if info != nil {
		cur = observedState{State: info.State, Health: info.Health}
	}
	s.observed.Store(siteID, cur)

	if row.Status == StatusProvisioning || row.Status == StatusDeleting {
		return
	}
	if active, _ := s.ActiveJob(ctx, siteID); active != nil {
		return
	}
	newStatus, msg := row.Status, row.StatusMessage
	switch {
	case info == nil:
		newStatus, msg = StatusError, "container missing"
	case info.State == "running" && (row.Status == StatusStopped || observerOwned(row)):
		// Only undo states the observer itself produced; a job failure stays
		// visible until an operator retries or a job succeeds.
		newStatus, msg = StatusRunning, ""
	case (info.State == "exited" || info.State == "dead") && row.Status == StatusRunning:
		if info.ExitCode == 0 {
			newStatus, msg = StatusStopped, "container stopped"
		} else {
			newStatus, msg = StatusError, fmt.Sprintf("container exited with code %d", info.ExitCode)
		}
	case info.State == "running" && info.Health == "unhealthy" && row.Status == StatusRunning:
		newStatus, msg = StatusError, "container is unhealthy"
	}
	if newStatus != row.Status || msg != row.StatusMessage {
		s.setStatus(ctx, siteID, newStatus, msg)
		return
	}
	if p, ok := prev.(observedState); !ok || p != cur {
		if payload, err := s.Payload(ctx, row); err == nil {
			s.publish(payload)
		}
	}
}

// ReconcileAll inspects every site's container once (engine boot).
func (s *Service) ReconcileAll(ctx context.Context) error {
	rows, err := s.st.ListSites(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Status == StatusProvisioning || row.Status == StatusDeleting {
			// A job was interrupted by a restart: the runner has already
			// failed it, so surface that on the site.
			if active, _ := s.ActiveJob(ctx, row.ID); active == nil {
				s.setStatus(ctx, row.ID, StatusError, "interrupted by an engine restart; retry or delete")
			}
			continue
		}
		info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
		if err != nil && !isNotFound(err) {
			return err
		}
		s.ObserveContainer(ctx, row.ID, info)
		// Backfill for sites created before WordPress versions were tracked.
		if row.WpVersion == "" && info != nil && info.Running() {
			s.detectWPVersion(ctx, &row, info.ID)
		}
	}
	return nil
}
