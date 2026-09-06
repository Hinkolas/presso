// Package cron replaces WordPress's visitor-triggered pseudo-cron: the
// runtime sets DISABLE_WP_CRON, and this worker runs `wp cron event run
// --due-now` in every running site on a fixed interval, a few sites at a
// time. Failures are recorded in the site's activity log.
package cron

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
)

type Runner struct {
	st          *store.Store
	sites       *site.Service
	concurrency int
}

func New(st *store.Store, sites *site.Service, concurrency int) *Runner {
	if concurrency <= 0 {
		concurrency = 3
	}
	return &Runner{st: st, sites: sites, concurrency: concurrency}
}

// Run ticks every interval until ctx ends. The first tick waits one interval
// so a freshly booted engine does not hammer sites that are still starting.
func (r *Runner) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Tick(ctx)
		}
	}
}

// Tick runs due cron events for every running site once.
func (r *Runner) Tick(ctx context.Context) {
	rows, err := r.st.ListSitesByStatus(ctx, site.StatusRunning)
	if err != nil {
		slog.WarnContext(ctx, "cron: list sites", "err", err)
		return
	}
	sem := make(chan struct{}, r.concurrency)
	done := make(chan struct{})
	for _, row := range rows {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		go func(row store.Site) {
			defer func() { <-sem; done <- struct{}{} }()
			r.runSite(ctx, row)
		}(row)
	}
	for range rows {
		<-done
	}
}

func (r *Runner) runSite(ctx context.Context, row store.Site) {
	res, err := r.sites.ExecWP(ctx, row.ID, 5*time.Minute, "cron", "event", "run", "--due-now", "--quiet")
	switch {
	case err != nil:
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "cron: run", "site", row.Slug, "err", err)
		}
	case res.ExitCode != 0:
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		slog.WarnContext(ctx, "cron: wp cron failed", "site", row.Slug, "exit", res.ExitCode, "output", msg)
		r.sites.LogActivity(ctx, row.ID, "cron.failed", "WP cron run failed: "+truncate(msg, 500), map[string]any{"exit_code": res.ExitCode})
	default:
		slog.DebugContext(ctx, "cron: ran", "site", row.Slug, "duration_ms", res.DurationMs)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
