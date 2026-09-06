// Package migrations embeds the goose SQL migrations so binaries and tests can
// migrate the engine database without a goose CLI. The same files are sqlc's
// schema source (sqlc.yaml points schema: at this directory).
package migrations

import (
	"context"
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var FS embed.FS

// Up applies all pending migrations. db must be a database/sql handle on the
// SQLite engine database (see store.Open).
func Up(ctx context.Context, db *sql.DB) ([]*goose.MigrationResult, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, FS)
	if err != nil {
		return nil, err
	}
	return p.Up(ctx)
}

// Status returns one line per migration with its applied state.
func Status(ctx context.Context, db *sql.DB) ([]*goose.MigrationStatus, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, FS)
	if err != nil {
		return nil, err
	}
	return p.Status(ctx)
}
