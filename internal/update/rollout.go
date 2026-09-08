package update

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
)

// Global jobs have their own worker, so waiting here also works with a single
// site worker. Child identifiers and outcomes are persisted in the job log.
func (s *Service) runRollout(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	if s.sites == nil {
		return errors.New("rollout: no site service")
	}
	r.Step(ctx, "resolve", "Resolving and scheduling site configurations")
	sites, err := s.sites.List(ctx)
	if err != nil {
		return err
	}
	var children []store.Job
	failed, deferred, current := 0, 0, 0
	for _, st := range sites {
		if st.Status == site.StatusDeleting {
			continue
		}
		if job.CreatedBy == createdBySystem && st.Status == site.StatusError && st.DesiredState != site.StatusStopped {
			failed++
			r.Warn(ctx, st.Slug+": previous application failed or was interrupted; explicit retry required")
			continue
		}
		res, err := s.sites.Catalog().Resolve(st.WPVersion, st.PHPVersion)
		if st.WPVersion == "" {
			err = errors.New("WordPress version has not been detected")
		}
		if err != nil {
			failed++
			r.Warn(ctx, fmt.Sprintf("%s: %v", st.Slug, err))
			continue
		}
		child, err := s.sites.ApplyRuntime(ctx, st.ID, res, job.CreatedBy)
		if err != nil {
			failed++
			r.Warn(ctx, fmt.Sprintf("%s: could not queue application: %v", st.Slug, err))
			continue
		}
		if child == nil {
			if st.DesiredState == site.StatusStopped {
				deferred++
			} else {
				current++
			}
			continue
		}
		children = append(children, *child)
		r.Infof(ctx, "%s: child job %s", st.Slug, child.ID)
	}
	r.Step(ctx, "recreate", "Waiting for site application jobs")
	succeeded := 0
	for _, child := range children {
		for {
			state, err := s.st.GetJob(ctx, child.ID)
			if err != nil {
				return err
			}
			if state.Status == jobs.StatusSucceeded {
				succeeded++
				break
			}
			if state.Status == jobs.StatusFailed {
				failed++
				r.Warn(ctx, fmt.Sprintf("child %s failed: %s", child.ID, state.Error))
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	summary := fmt.Sprintf("%d applied, %d verified current, %d deferred until Start, %d failed", succeeded, current, deferred, failed)
	r.Info(ctx, summary)
	if failed > 0 {
		return fmt.Errorf("rollout: %s; retry to reattempt incomplete sites", summary)
	}
	return nil
}
