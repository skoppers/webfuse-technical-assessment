package store

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigrate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"sessions", "events"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q missing after migrate", table)
		}
	}
	if !constraintExists(t, db, "events_session_id_seq_key", "u") {
		t.Error("unique constraint on events(session_id, seq) missing")
	}

	// A second run must find nothing to apply and leave the version unchanged.
	p, err := newProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.GetDBVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	after, err := p.GetDBVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || before == 0 {
		t.Errorf("version changed on re-migrate: before=%d after=%d", before, after)
	}
	pending, err := p.HasPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Error("migrations still pending after Migrate")
	}
}

func TestMigrateDownAndUp(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p, err := newProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	for _, table := range []string{"sessions", "events"} {
		if tableExists(t, db, table) {
			t.Errorf("table %q still present after down", table)
		}
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	for _, table := range []string{"sessions", "events"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q missing after re-up", table)
		}
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var ok bool
	err := db.QueryRowContext(context.Background(),
		"SELECT to_regclass($1) IS NOT NULL", "public."+name).Scan(&ok)
	if err != nil {
		t.Fatalf("check table %q: %v", name, err)
	}
	return ok
}

func constraintExists(t *testing.T, db *sql.DB, name, contype string) bool {
	t.Helper()
	var ok bool
	err := db.QueryRowContext(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1 AND contype = $2)",
		name, contype).Scan(&ok)
	if err != nil {
		t.Fatalf("check constraint %q: %v", name, err)
	}
	return ok
}
