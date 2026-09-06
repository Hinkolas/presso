// Store-level glue around the sqlc-generated Queries: opening the SQLite
// database and transaction handling. sqlc owns the other files in this
// package except errors.go.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// Open opens (creating if needed) the SQLite engine database at path and
// verifies it is reachable. The returned handle is shared by sqlc queries and
// goose migrations.
//
// Pragmas: WAL for concurrent readers alongside the single writer,
// foreign_keys because SQLite defaults them off, busy_timeout so a second
// writer waits instead of failing with SQLITE_BUSY. _time_format=sqlite makes
// the driver write time.Time as "YYYY-MM-DD HH:MM:SS.fffffffff+00:00", which
// is what makes DATETIME comparisons in SQL work: all timestamps are written
// by Go in UTC (see auth.Service.now), never by SQL defaults.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)" +
		"&_time_format=sqlite"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One connection: SQLite allows one writer at a time anyway, and a single
	// connection removes SQLITE_BUSY from write/write races entirely. The
	// engine's request volume is operator-scale, so this costs nothing
	// measurable; revisit (separate read pool) if it ever does.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return db, nil
}

// Store bundles the database handle with the generated query API.
type Store struct {
	DB *sql.DB
	*Queries
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db, Queries: New(db)}
}

// WithTx runs fn inside a transaction, committing when it returns nil.
func (s *Store) WithTx(ctx context.Context, fn func(q *Queries) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
