package migrations

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/Hinkolas/presso/internal/store"
)

// TestSiteRuntimeBackfill applies migrations up to the sites schema, inserts
// rows the way pre-runtime engines did, and checks that 00004 derives the
// runtime from the PHP version and leaves wp_version empty for detection.
func TestSiteRuntimeBackfill(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/presso.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectSQLite3, db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO sites (id, slug, name, status, primary_domain, tls_mode, php_version, image, db_name, db_user,
		db_password_enc, redis_user, redis_password_enc, salts_enc, purge_token_hash, login_secret_enc, admin_user, admin_email,
		volume_name, created_at, updated_at) VALUES (?, ?, ?, 'running', ?, 'off', ?, ?, 'db', 'db', x'00', 'r', x'00', x'00', x'00', x'00', 'admin', 'a@b.c', 'v', '2026-01-01', '2026-01-01')`
	for _, row := range [][]string{{"a", "new", "8.3"}, {"b", "old", "8.1"}, {"c", "older", "7.4"}} {
		if _, err := db.ExecContext(ctx, insert, row[0], row[1], row[1], row[1]+".localhost", row[2], "presso-runtime:php"+row[2]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT slug, runtime, wp_version FROM sites ORDER BY slug`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]string{"new": "frankenphp", "old": "legacy", "older": "legacy"}
	n := 0
	for rows.Next() {
		var slug, runtime, wp string
		if err := rows.Scan(&slug, &runtime, &wp); err != nil {
			t.Fatal(err)
		}
		if runtime != want[slug] || wp != "" {
			t.Errorf("%s: runtime %q wp %q", slug, runtime, wp)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("rows: %d", n)
	}
}
