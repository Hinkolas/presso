package observe

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
)

// StatusWatcher follows Docker container events for site containers and
// feeds them into the site service, which decides what a crash or an
// external stop means for the persisted status.
type StatusWatcher struct {
	dk    docker.Client
	sites *site.Service
	// OnPlatformEvent, when set, is called for events on shared containers
	// so the platform reconciler can react sooner than its next tick.
	OnPlatformEvent func()
}

func NewStatusWatcher(dk docker.Client, sites *site.Service) *StatusWatcher {
	return &StatusWatcher{dk: dk, sites: sites}
}

// Run reconciles every site once, then streams events until ctx ends,
// reconnecting if the event stream drops.
func (w *StatusWatcher) Run(ctx context.Context) {
	if err := w.sites.ReconcileAll(ctx); err != nil {
		slog.WarnContext(ctx, "observe: reconcile sites", "err", err)
	}
	for ctx.Err() == nil {
		events, errc := w.dk.Events(ctx, map[string]string{docker.LabelManaged: "true"})
	loop:
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					break loop
				}
				w.handle(ctx, ev)
			case err := <-errc:
				if err != nil {
					slog.WarnContext(ctx, "observe: docker events", "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		// Events may have been missed while disconnected.
		if err := w.sites.ReconcileAll(ctx); err != nil {
			slog.WarnContext(ctx, "observe: reconcile sites", "err", err)
		}
	}
}

func (w *StatusWatcher) handle(ctx context.Context, ev docker.Event) {
	switch ev.Action {
	case "start", "die", "stop", "kill", "oom", "health_status", "destroy", "pause", "unpause":
	default:
		return
	}
	if ev.Labels[docker.LabelKind] != "site" {
		if w.OnPlatformEvent != nil && (ev.Action == "die" || ev.Action == "destroy") {
			w.OnPlatformEvent()
		}
		return
	}
	siteID, err := uuid.Parse(ev.Labels[docker.LabelSiteID])
	if err != nil {
		return
	}
	if ev.Action == "destroy" {
		w.sites.ObserveContainer(ctx, siteID, nil)
		return
	}
	info, err := w.dk.InspectContainer(ctx, ev.ContainerID)
	if err != nil {
		// Removed between the event and the inspect: treat as missing.
		w.sites.ObserveContainer(ctx, siteID, nil)
		return
	}
	w.sites.ObserveContainer(ctx, siteID, info)
}

// StatsSampler polls container resource usage for running sites and rolls
// it up per minute.
type StatsSampler struct {
	dk docker.Client
	st *store.Store
}

func NewStatsSampler(dk docker.Client, st *store.Store) *StatsSampler {
	return &StatsSampler{dk: dk, st: st}
}

func (s *StatsSampler) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Sample(ctx)
		}
	}
}

// Sample takes one reading per running site container.
func (s *StatsSampler) Sample(ctx context.Context) {
	containers, err := s.dk.ListContainers(ctx, map[string]string{docker.LabelManaged: "true", docker.LabelKind: "site"}, false)
	if err != nil {
		slog.WarnContext(ctx, "observe: list site containers", "err", err)
		return
	}
	bucket := time.Now().UTC().Truncate(time.Minute)
	for _, c := range containers {
		siteID, err := uuid.Parse(c.Labels[docker.LabelSiteID])
		if err != nil {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		st, err := s.dk.Stats(sctx, c.ID)
		cancel()
		if err != nil {
			continue
		}
		err = s.st.UpsertContainerMetric(ctx, store.UpsertContainerMetricParams{
			SiteID: siteID, Bucket: bucket, CpuPct: st.CPUPercent, MemBytes: int64(st.MemUsage), MemLimit: int64(st.MemLimit),
		})
		if err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "observe: store container metric", "err", err)
		}
	}
}

// Retention deletes old rollups and finished jobs.
type Retention struct {
	st         *store.Store
	metricsFor time.Duration
	jobsFor    time.Duration
}

func NewRetention(st *store.Store, metricsFor, jobsFor time.Duration) *Retention {
	return &Retention{st: st, metricsFor: metricsFor, jobsFor: jobsFor}
}

func (r *Retention) Run(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		r.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Retention) Sweep(ctx context.Context) {
	now := time.Now().UTC()
	if _, err := r.st.DeleteRequestMetricsBefore(ctx, now.Add(-r.metricsFor)); err != nil {
		slog.WarnContext(ctx, "observe: retention request metrics", "err", err)
	}
	if _, err := r.st.DeleteContainerMetricsBefore(ctx, now.Add(-r.metricsFor)); err != nil {
		slog.WarnContext(ctx, "observe: retention container metrics", "err", err)
	}
	if _, err := r.st.DeleteJobsBefore(ctx, sql.NullTime{Time: now.Add(-r.jobsFor), Valid: true}); err != nil {
		slog.WarnContext(ctx, "observe: retention jobs", "err", err)
	}
}
