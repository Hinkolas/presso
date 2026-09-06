// Command pressod is the orchestration engine: the client-facing REST API
// (embedded web UI, harness CLI) plus the workers that provision and manage
// WordPress site containers through the Docker API, reconcile the shared
// platform containers (MariaDB, Redis, edge) and collect observability data.
//
// Besides serving (the default), the binary carries the operator commands,
// one artifact to deploy and exec into:
//
//	pressod [serve]                              run the engine (REST API + UI + workers)
//	pressod user create|list|set-role|reset-password|delete
//	                                             manage operator accounts (no signup endpoint)
//	pressod migrate up|status                    apply / inspect database migrations
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Hinkolas/presso/internal/api"
	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/cron"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/obs"
	"github.com/Hinkolas/presso/internal/observe"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/shots"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
	versionpkg "github.com/Hinkolas/presso/internal/version"
	"github.com/Hinkolas/presso/internal/webui"
	"github.com/Hinkolas/presso/migrations"
)

const serviceName = "pressod"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "service", serviceName, "err", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "serve" {
		return runServe()
	}
	switch args[0] {
	case "user":
		return runUser(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "version", "--version":
		fmt.Println(versionpkg.Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (available: serve, user, migrate, version)", args[0])
	}
}

func runServe() error {
	ctx := context.Background()

	cfg, err := config.Load[config.API](ctx)
	if err != nil {
		return err
	}
	obs.Init(obs.Options{
		ServiceName: serviceName,
		Version:     versionpkg.Version,
		LogLevel:    cfg.LogLevel,
		LogFormat:   cfg.LogFormat,
	})

	db, err := store.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()

	// The engine owns its embedded database, so it migrates itself on boot:
	// there is no separate deploy step to forget on a single-host install.
	if _, err := migrations.Up(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	st := store.NewStore(db)

	authSvc := auth.New(st, auth.Config{SessionTTL: cfg.SessionTTL})
	box, err := secrets.New(cfg.AuthSecret)
	if err != nil {
		return err
	}
	dk, err := docker.New(cfg.DockerHost)
	if err != nil {
		return err
	}
	defer dk.Close()
	if info, err := dk.Ping(ctx); err != nil {
		// Not fatal: the UI must come up so the operator can see why.
		slog.WarnContext(ctx, "docker daemon unreachable; sites cannot be managed until it is", "err", err)
	} else {
		slog.InfoContext(ctx, "docker connected", "version", info.Version, "os", info.OS, "arch", info.Arch)
	}

	hub := events.New()
	edgeClient := edge.NewClient(cfg.Edge.AdminListen, cfg.Edge.CacheAdminListen, filepath.Join(cfg.DataDir, "edge"))
	plat := platform.New(cfg, dk, st, box, hub, edgeClient)
	defer plat.Close()
	runner := jobs.New(st, hub, 4)
	var shotSvc *shots.Service // nil when previews are disabled
	if cfg.Shots.Enabled {
		shotSvc = shots.New(filepath.Join(cfg.DataDir, "screenshots"), cfg.Shots.Listen, cfg.Shots.Timeout)
	}
	sites := site.New(site.Deps{
		Config: cfg, Store: st, Docker: dk, Secrets: box, Hub: hub, Jobs: runner, Platform: plat, Edge: edgeClient,
		Shots: shotSvc,
	})

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.StripAPIPrefix(api.NewRouter(api.Deps{
			Auth:     authSvc,
			Store:    st,
			DB:       db,
			Version:  versionpkg.Version,
			Config:   cfg,
			Sites:    sites,
			Hub:      hub,
			Docker:   dk,
			Platform: plat,
			WebUI:    webui.Handler(),
		})),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Workers share one context; cancelling it is the first step of shutdown.
	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()
	if err := runner.Start(loopCtx); err != nil {
		return err
	}

	var wg sync.WaitGroup
	spawn := func(name string, fn func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(loopCtx)
			slog.DebugContext(ctx, "worker stopped", "worker", name)
		}()
	}
	spawn("sessions", func(ctx context.Context) { sweepLoop(ctx, authSvc) })
	// The platform reconciler runs first so the edge and shared services exist
	// before any site work; the status watcher re-inspects sites right after.
	spawn("platform", func(ctx context.Context) { plat.Run(ctx, time.Minute) })
	watcher := observe.NewStatusWatcher(dk, sites)
	spawn("status", watcher.Run)
	spawn("stats", func(ctx context.Context) { observe.NewStatsSampler(dk, st).Run(ctx, cfg.StatsInterval) })
	spawn("edgelog", func(ctx context.Context) { observe.NewEdgeLogIngester(dk, st).Run(ctx, 10*time.Second) })
	spawn("retention", observe.NewRetention(st, cfg.MetricsRetention, 30*24*time.Hour).Run)
	spawn("cron", func(ctx context.Context) { cron.New(st, sites, 3).Run(ctx, cfg.CronInterval) })
	if cfg.Shots.Enabled {
		spawn("screenshots", func(ctx context.Context) { sites.ScreenshotLoop(ctx, cfg.Shots.Interval) })
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "listening", "addr", cfg.HTTPAddr, "data_dir", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErr:
		return err
	case <-sigCtx.Done():
	}
	slog.InfoContext(ctx, "shutting down")
	cancelLoops()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	// Give running jobs a moment to record their outcome.
	done := make(chan struct{})
	go func() { runner.Wait(); wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		slog.WarnContext(ctx, "workers did not stop in time")
	}
	return err
}

// sweepLoop deletes expired sessions periodically. Errors are logged, never
// fatal: housekeeping must not take the API down.
func sweepLoop(ctx context.Context, authSvc *auth.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := authSvc.SweepExpired(ctx); err != nil {
			slog.WarnContext(ctx, "sweep expired sessions", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
