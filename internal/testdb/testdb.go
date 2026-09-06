// Package testdb gives tests a fresh, migrated SQLite database in a temp
// directory. Real SQLite, no mocks; the file is removed with t.TempDir.
package testdb

import (
	"context"
	"testing"

	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/migrations"
)

// New opens a migrated database for t and closes it at cleanup.
func New(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/presso.db")
	if err != nil {
		t.Fatalf("testdb: open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := migrations.Up(ctx, db); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return store.NewStore(db)
}
