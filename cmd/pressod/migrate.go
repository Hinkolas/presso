package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/migrations"
)

// runMigrate applies the embedded goose migrations. serve migrates on boot as
// well; this subcommand exists to migrate without starting the API and to
// inspect the applied state.
func runMigrate(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: pressod migrate <up|status>")
	}
	ctx := context.Background()

	cfg, err := config.Load[config.Base](ctx)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()

	switch args[0] {
	case "up":
		results, err := migrations.Up(ctx, db)
		if err != nil {
			return err
		}
		fmt.Printf("migrations applied (%d new)\n", len(results))
		return nil
	case "status":
		statuses, err := migrations.Status(ctx, db)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			applied := "pending"
			if !s.AppliedAt.IsZero() {
				applied = s.AppliedAt.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%05d  %-19s  %s\n", s.Source.Version, applied, s.Source.Path)
		}
		return nil
	default:
		return fmt.Errorf("unknown migrate command %q (available: up, status)", args[0])
	}
}
