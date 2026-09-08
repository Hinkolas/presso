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
//	pressod runtime-matrix [--bases] [--legacy=false]
//	                                             print the runtime image build matrix (CI)
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Hinkolas/presso/internal/api"
	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/control"
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
	"github.com/Hinkolas/presso/internal/update"
	versionpkg "github.com/Hinkolas/presso/internal/version"
	"github.com/Hinkolas/presso/internal/warm"
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
	case "runtime-matrix":
		return runRuntimeMatrix(args[1:])
	case "version", "--version":
		fmt.Println(versionpkg.Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (available: serve, user, migrate, runtime-matrix, version)", args[0])
	}
}

func runServe() error {
	ctx := context.Background()

	cfg, err := config.Load[config.API](ctx)
	if err != nil {
		return err
	}
	// Fails fast on a default WordPress branch or PHP allowlist the matrix
	// cannot satisfy, the semantic half of the RUNTIME_* validation.
	catalog, err := compat.New(compat.Default, compat.Options{
		ImageTemplate: cfg.ImageTemplate, Release: versionpkg.Release(), LegacyEnabled: cfg.LegacyEnabled, DefaultWP: cfg.DefaultWP, PHPVersions: cfg.PHPVersions,
	})
	if err != nil {
		return fmt.Errorf("config: %w", err)
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

	trust, err := control.Open(cfg.DataDir)
	if err != nil {
		return err
	}

	hub := events.New()
	edgeClient := edge.NewDockerClient(dk, platform.NameEdge, filepath.Join(cfg.DataDir, "edge"))
	plat := platform.New(cfg, dk, st, box, hub, edgeClient)
	defer plat.Close()
	runner := jobs.New(st, hub, 4)
	var shotSvc *shots.Service // nil when previews are disabled
	if cfg.Shots.Enabled {
		shotSvc = shots.NewDocker(filepath.Join(cfg.DataDir, "screenshots"), dk, platform.NameBrowser, cfg.Shots.Timeout)
	}
	warmer := warm.New(warm.Deps{Store: st, Config: cfg})
	sites := site.New(site.Deps{
		Config: cfg, Catalog: catalog, Store: st, Docker: dk, Secrets: box, Hub: hub, Jobs: runner, Platform: plat, Edge: edgeClient,
		Shots: shotSvc, Warmer: warmer,
	})
	// The executable path is resolved once, now: after a self-update swapped
	// the file, /proc/self/exe reports the old inode as "(deleted)".
	exePath, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exePath); rerr == nil {
			exePath = resolved
		}
	}
	updater := update.New(update.Deps{Config: cfg, Store: st, Docker: dk, Hub: hub, Jobs: runner, Sites: sites, Executable: exePath})

	// Streaming handlers (events, logs) watch this context so shutdown does
	// not wait for browsers to hang up.
	streamsCtx, stopStreams := context.WithCancel(context.Background())
	defer stopStreams()
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.StripAPIPrefix(api.NewRouter(api.Deps{
			Shutdown: streamsCtx,
			Auth:     authSvc,
			Store:    st,
			DB:       db,
			Version:  versionpkg.Version,
			Config:   cfg,
			Catalog:  catalog,
			Sites:    sites,
			Hub:      hub,
			Docker:   dk,
			Platform: plat,
			Update:   updater,
			WebUI:    webui.Handler(),
		})),
		ReadHeaderTimeout: 5 * time.Second,
		// The raw peer address feeds the panel guard (PANEL_DOMAIN).
		ConnContext: api.PeerContext,
	}

	internalSrv := &http.Server{Addr: cfg.InternalHTTPSAddr, Handler: api.InternalOnly(srv.Handler), ReadHeaderTimeout: 5 * time.Second, ConnContext: api.PeerContext, TLSConfig: trust.ServerTLS()}
	// Bind both listeners before starting workers so a bad address does not mutate infrastructure.
	localListener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return err
	}
	defer localListener.Close()
	internalListener, err := net.Listen("tcp", cfg.InternalHTTPSAddr)
	if err != nil {
		return err
	}
	defer internalListener.Close()
	// Workers share one context; cancelling it is the first step of shutdown.
	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()
	if err := runner.Start(loopCtx); err != nil {
		return err
	}
	// First boot on a new version: move every site to this release's runtime
	// images (engine.rollout). The platform reconciler below swaps the edge.

	var wg sync.WaitGroup
	spawn := func(name string, fn func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(loopCtx)
			slog.DebugContext(ctx, "worker stopped", "worker", name)
		}()
	}
	spawn("trust", func(ctx context.Context) {
		tick := time.NewTicker(time.Hour)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				changed, err := trust.Refresh(ctx)
				if err != nil {
					slog.Error("control certificate renewal failed", "err", err)
				} else if changed {
					if err := plat.ApplyEdge(ctx); err != nil {
						slog.Error("edge certificate reload failed", "err", err)
					}
				}
			}
		}
	})
	spawn("sessions", func(ctx context.Context) { sweepLoop(ctx, authSvc) })
	// The platform reconciler runs first so the edge and shared services exist
	// before any site work; the status watcher re-inspects sites right after.
	spawn("platform", func(ctx context.Context) {
		// Reconcile before the first rollout so the safe bootstrap and callbacks
		// are ready. A temporary infrastructure failure retries without touching sites.
		for {
			if err := plat.Reconcile(ctx); err == nil {
				updater.OnBoot(ctx)
				break
			} else {
				slog.WarnContext(ctx, "initial platform reconciliation failed", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		}
		plat.Run(ctx, time.Minute)
	})
	watcher := observe.NewStatusWatcher(dk, sites)
	spawn("status", watcher.Run)
	spawn("stats", func(ctx context.Context) { observe.NewStatsSampler(dk, st).Run(ctx, cfg.StatsInterval) })
	spawn("edgelog", func(ctx context.Context) { observe.NewEdgeLogIngester(dk, st).Run(ctx, 10*time.Second) })
	spawn("retention", observe.NewRetention(st, cfg.MetricsRetention, 30*24*time.Hour).Run)
	spawn("cron", func(ctx context.Context) { cron.New(st, sites, 3).Run(ctx, cfg.CronInterval) })
	if cfg.Shots.Enabled {
		spawn("screenshots", func(ctx context.Context) { sites.ScreenshotLoop(ctx, cfg.Shots.Interval) })
	}
	spawn("warm", warmer.Start)
	spawn("update", updater.Run)

	serveErr := make(chan error, 2)
	go func() {
		if err := internalSrv.Serve(tls.NewListener(internalListener, internalSrv.TLSConfig)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()
	go func() {
		slog.InfoContext(ctx, "listening", "addr", cfg.HTTPAddr, "data_dir", cfg.DataDir)
		if err := srv.Serve(localListener); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	restart := false
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	case <-sigCtx.Done():
	case <-updater.RestartRequests():
		restart = true
		slog.InfoContext(ctx, "engine updated, restarting", "exe", exePath)
	}
	slog.InfoContext(ctx, "shutting down")
	cancelLoops()
	stopStreams()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = errors.Join(srv.Shutdown(shutdownCtx), internalSrv.Shutdown(shutdownCtx))
	// Give running jobs a moment to record their outcome.
	done := make(chan struct{})
	go func() { runner.Wait(); wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		slog.WarnContext(ctx, "workers did not stop in time")
	}
	if restart {
		reexec(ctx, exePath)
	}
	return err
}

// reexec replaces this process with the (freshly installed) binary at path,
// keeping PID, arguments and environment: correct under systemd and when run
// by hand. Exec only returns on failure; exiting non-zero then lets the
// supervisor (systemd Restart=always) start the new binary instead.
func reexec(ctx context.Context, path string) {
	slog.InfoContext(ctx, "re-executing", "exe", path, "args", os.Args)
	err := syscall.Exec(path, os.Args, os.Environ())
	slog.ErrorContext(ctx, "re-exec failed; exiting so the supervisor restarts the service", "err", err)
	os.Exit(1)
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
