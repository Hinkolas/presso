package site

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/google/uuid"
)

// Persist the gate before changing the runtime. A failed push must leave the
// old container intact; reconciliation retries the unavailable configuration.
func (s *Service) quiesce(ctx context.Context, row store.Site, r *jobs.Reporter) error {
	r.Step(ctx, "unavailable", "Disabling traffic")
	if err := s.st.SetSiteTraffic(ctx, store.SetSiteTrafficParams{TrafficReady: false, UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return err
	}
	if err := s.platform.ApplyEdge(ctx); err != nil {
		return fmt.Errorf("disable traffic: %w", err)
	}
	if err := s.PurgeCache(ctx, row.ID, nil); err != nil {
		return fmt.Errorf("purge cache: %w", err)
	}
	return nil
}

func (s *Service) activate(ctx context.Context, row store.Site, cid string, r *jobs.Reporter) (retErr error) {
	defer func() {
		if retErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = s.st.RestoreSiteApplication(cleanup, store.RestoreSiteApplicationParams{AppliedRevision: 0, AppliedImage: row.AppliedImage, UpdatedAt: s.now(), ID: row.ID})
		_ = s.platform.ApplyEdge(cleanup)
	}()

	r.Step(ctx, "validate", "Validating WordPress bootstrap")
	res, err := s.exec(ctx, cid, time.Minute, "eval", "if (!is_blog_installed()) { WP_CLI::error('WordPress is not installed'); }")
	if err != nil {
		return fmt.Errorf("WordPress bootstrap: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("WordPress bootstrap exited %d: %s", res.ExitCode, res.Stderr)
	}
	if err := s.PurgeCache(ctx, row.ID, nil); err != nil {
		return fmt.Errorf("purge cache: %w", err)
	}
	r.Step(ctx, "publish", "Publishing validated site")
	if err := s.st.SetSiteTraffic(ctx, store.SetSiteTrafficParams{TrafficReady: true, UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return err
	}
	if err := s.st.SetSiteStatus(ctx, store.SetSiteStatusParams{Status: StatusRunning, StatusMessage: "", UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return err
	}
	if err := s.platform.ApplyEdge(ctx); err != nil {
		return fmt.Errorf("publish traffic: %w", err)
	}
	if err := s.st.CompleteSiteApplication(ctx, store.CompleteSiteApplicationParams{UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return fmt.Errorf("record applied configuration: %w", err)
	}
	return nil
}

func (s *Service) applicationFailed(ctx context.Context, id uuid.UUID, err error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	s.setStatus(cleanup, id, StatusError, "application failed: "+err.Error())
	return err
}

// ApplyRuntime shares the same submission lock as settings and lifecycle
// commands. A matching target tag alone never proves successful application.
func (s *Service) ApplyRuntime(ctx context.Context, id uuid.UUID, res compat.Resolution, by string) (*store.Job, error) {
	s.commands.Lock()
	defer s.commands.Unlock()
	if s.jobs.Paused() {
		return nil, jobs.ErrPaused
	}
	row, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if active, err := s.ActiveJob(ctx, id); err != nil {
		return nil, err
	} else if active != nil {
		return nil, ErrJobRunning
	}
	if row.Status == StatusDeleting || row.Status == StatusProvisioning {
		return nil, ErrNotReady
	}
	if row.Image != res.Image || row.Runtime != res.Runtime || row.PhpVersion != res.PHP {
		if err := s.st.SetSiteImage(ctx, store.SetSiteImageParams{Image: res.Image, Runtime: res.Runtime, PhpVersion: res.PHP, UpdatedAt: s.now(), ID: id}); err != nil {
			return nil, err
		}
		row, err = s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if row.DesiredState == StatusStopped {
		return nil, nil
	}
	if row.Status == StatusRunning && row.TrafficReady && row.AppliedRevision == row.ConfigRevision && row.AppliedImage == row.Image {
		info, err := s.dk.InspectContainer(ctx, ContainerName(row.Slug))
		if err == nil && info.Running() && info.Health == "healthy" && info.Image == row.Image && info.Labels["presso.config_revision"] == strconv.FormatInt(row.ConfigRevision, 10) {
			return nil, nil
		}
		if err != nil && !isNotFound(err) {
			return nil, err
		}
	}
	job, err := s.enqueue(ctx, row, JobRecreate, nil, by)
	if err != nil {
		return nil, err
	}
	return &job, nil
}
