package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// openTestDB opens the database named by TEST_DATABASE_URL, applies the
// migrations and truncates both tables so each test starts from an empty
// schema. It skips the test when TEST_DATABASE_URL is unset.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE events, sessions CASCADE"); err != nil {
		t.Fatalf("truncate test db: %v", err)
	}
	return db
}
