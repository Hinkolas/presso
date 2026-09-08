package update

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
)

// runRollout is the engine.rollout handler: every site whose runtime image
// under the running release differs from the one on its row is moved over.
// Running sites are recreated (one job each, serialized per site by the
// runner); stopped sites only get their row updated and are rebuilt on their
// next start. Sites the catalog can no longer place are reported, never
// touched.
func (s *Service) runRollout(ctx context.Context, job store.Job, r *jobs.Reporter) error {
	if s.sites == nil {
		return errors.New("rollout: no site service")
	}
	cat := s.sites.Catalog()
	r.Step(ctx, "resolve", "Resolving runtime images for every site")
	sites, err := s.sites.List(ctx)
	if err != nil {
		return err
	}
	type move struct {
		site  site.Site
		image string
		rt    string
		php   string
	}
	var moves []move
	var skipped []string
	current := 0
	for _, st := range sites {
		if st.Status == site.StatusDeleting || st.Status == site.StatusProvisioning {
			continue
		}
		if st.WPVersion == "" {
			skipped = append(skipped, st.Slug+" (WordPress version not detected yet; start it once)")
			continue
		}
		res, err := cat.Resolve(st.WPVersion, st.PHPVersion)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s (%v)", st.Slug, err))
			continue
		}
		if res.Image == st.Image {
			current++
			continue
		}
		moves = append(moves, move{site: st, image: res.Image, rt: res.Runtime, php: res.PHP})
		r.Infof(ctx, "%s: %s → %s", st.Slug, st.Image, res.Image)
	}
	r.Infof(ctx, "%d site(s) already on the current images, %d to move, %d skipped", current, len(moves), len(skipped))

	r.Step(ctx, "recreate", "Moving sites to the new images")
	recreated, deferred, failed := 0, 0, 0
	for _, m := range moves {
		if err := s.st.SetSiteImage(ctx, store.SetSiteImageParams{Image: m.image, Runtime: m.rt, PhpVersion: m.php, UpdatedAt: s.now(), ID: m.site.ID}); err != nil {
			failed++
			r.Warn(ctx, m.site.Slug+": could not update the record: "+err.Error())
			continue
		}
		if m.site.Status != site.StatusRunning {
			deferred++
			r.Infof(ctx, "%s is %s; it moves to the new image on its next start", m.site.Slug, m.site.Status)
			continue
		}
		if _, err := s.sites.Recreate(ctx, m.site.ID, job.CreatedBy); err != nil {
			failed++
			r.Warn(ctx, m.site.Slug+": could not enqueue the recreate: "+err.Error()+" (recreate it from the site page)")
			continue
		}
		recreated++
	}
	for _, sk := range skipped {
		r.Warn(ctx, "stays on its current image: "+sk)
	}
	summary := fmt.Sprintf("%d recreate(s) enqueued, %d deferred to next start, %d skipped", recreated, deferred, len(skipped))
	if failed > 0 {
		return fmt.Errorf("rollout: %d site(s) could not be moved (%s)", failed, summary)
	}
	r.Info(ctx, strings.ToUpper(summary[:1])+summary[1:])
	return nil
}
