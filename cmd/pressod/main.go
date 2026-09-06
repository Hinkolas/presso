// Command pressod is the orchestration engine: the client-facing REST API
// (web BFF, harness CLI) plus, as they land, the workers that provision and
// manage WordPress site containers through the Docker API.
//
// Besides serving (the default), the binary carries the operator commands,
// one artifact to deploy and exec into:
//
//	pressod [serve]                              run the engine (REST API + workers)
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
	"syscall"
	"time"

	"github.com/Hinkolas/presso/internal/api"
	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/obs"
	"github.com/Hinkolas/presso/internal/store"
	versionpkg "github.com/Hinkolas/presso/internal/version"
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

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.StripAPIPrefix(api.NewRouter(api.Deps{
			Auth:    authSvc,
			Store:   st,
			DB:      db,
			Version: versionpkg.Version,
		})),
		ReadHeaderTimeout: 5 * time.Second,
	}

	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()
	go sweepLoop(loopCtx, authSvc)

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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
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
