// Package store owns the Postgres connection and schema. It opens the
// database/sql handle the rest of the server uses and applies the embedded
// migrations at startup.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

const (
	pingTimeout     = 5 * time.Second
	maxOpenConns    = 10
	connMaxIdleTime = 5 * time.Minute
)

// Open connects to Postgres at databaseURL using the pgx driver, verifies the
// connection with a bounded ping and applies pool limits. The caller owns the
// returned handle and must Close it.
func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetConnMaxIdleTime(connMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// Migrate applies every pending embedded migration to db. It is safe to call
// on every startup: goose records applied versions and skips them.
func Migrate(ctx context.Context, db *sql.DB) error {
	p, err := newProvider(db)
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read version: %w", err)
	}
	slog.Info("migrations applied", "applied", len(results), "version", version)
	return nil
}

// newProvider builds a goose provider over the embedded migrations. Goose's
// own logging is not enabled; callers report results via slog.
func newProvider(db *sql.DB) (*goose.Provider, error) {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("migrate: migrations fs: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return nil, fmt.Errorf("migrate: provider: %w", err)
	}
	return p, nil
}
