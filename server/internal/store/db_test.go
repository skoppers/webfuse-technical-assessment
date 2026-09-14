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
	if !constraintExists(t, db, "events_session_id_client_id_seq_key", "u") {
		t.Error("unique constraint on events(session_id, client_id, seq) missing")
	}
	if constraintExists(t, db, "events_session_id_seq_key", "u") {
		t.Error("old unique constraint on events(session_id, seq) still present")
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

// TestMigrateClientIDOnExistingRows applies 00002 to a database that already
// holds events written under 00001: they keep client_id ” and stay valid,
// and a client may then reuse a seq those rows already used.
func TestMigrateClientIDOnExistingRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p, err := newProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 1); err != nil {
		t.Fatalf("down to 1: %v", err)
	}
	if !constraintExists(t, db, "events_session_id_seq_key", "u") || constraintExists(t, db, "events_session_id_client_id_seq_key", "u") {
		t.Fatal("schema after down is not 00001")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (session_id, status, started_at, source) VALUES ('old', 'live', now(), 'event')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO events (session_id, seq, type, ts) VALUES ('old', 1, 'click', now()), ('old', 2, 'click', now())`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate with existing rows: %v", err)
	}
	if constraintExists(t, db, "events_session_id_seq_key", "u") || !constraintExists(t, db, "events_session_id_client_id_seq_key", "u") {
		t.Error("unique key not moved to (session_id, client_id, seq)")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE session_id = 'old' AND client_id = ''`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("%d pre-existing rows have client_id '', want 2", n)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO events (session_id, client_id, seq, type, ts) VALUES ('old', 'c1', 1, 'click', now())`); err != nil {
		t.Errorf("seq 1 from a new client rejected after migration: %v", err)
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
