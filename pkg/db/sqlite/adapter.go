// Package sqlite registers the SQLite 3 adapter for the axe db package.
//
// Import this package for its side effects:
//
//	import _ "github.com/axe-cute/axe/pkg/db/sqlite"
//
// This adapter uses modernc.org/sqlite — a pure-Go SQLite implementation that
// requires no CGO and no system libsqlite3. It is ideal for:
//   - Local development without Docker
//   - CI environments where container startup is undesirable
//   - Embedded / lightweight testing
//
// DSN format (DATABASE_URL when DB_DRIVER=sqlite3):
//
//	:memory:                    — ephemeral in-memory database
//	file::memory:?cache=shared  — shared in-memory (safe for tests)
//	file:/path/to/db.sqlite3    — on-disk database
//
// WAL mode and foreign-key enforcement are enabled automatically.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver

	"github.com/axe-cute/axe/pkg/db"
)

const (
	// driverName is the driver registered by modernc.org/sqlite.
	driverName = "sqlite"
	// entDialect is the Ent ORM dialect constant for SQLite.
	entDialect = "sqlite3"
)

func init() {
	db.Register("sqlite3", &adapter{})
}

type adapter struct{}

// DriverName returns the sql.Open driver name for modernc.org/sqlite.
func (a *adapter) DriverName() string { return driverName }

// EntDialect returns the Ent ORM dialect for SQLite.
func (a *adapter) EntDialect() string { return entDialect }

// DSN enriches the connection string with SQLite pragmas that are safe
// for server-style usage.
//
// Added automatically (if not already present):
//   - _pragma=journal_mode(WAL)   — better concurrency than DELETE mode
//   - _pragma=busy_timeout(5000)  — 5 s busy-timeout avoids "database is locked"
//   - _pragma=foreign_keys(1)     — enforce FK constraints (off by default in SQLite)
//
// Note: modernc.org/sqlite uses the _pragma=name(value) syntax, not the
// _name=value syntax used by the CGO mattn/go-sqlite3 driver.
func (a *adapter) DSN(cfg db.AdapterConfig) string {
	u := cfg.URL

	// Strip scheme prefix so both "sqlite3://path" and bare paths work.
	u = strings.TrimPrefix(u, "sqlite3://")
	u = strings.TrimPrefix(u, "sqlite://")

	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}

	if !strings.Contains(u, "_pragma=journal_mode") {
		u += sep + "_pragma=journal_mode(WAL)"
		sep = "&"
	}
	if !strings.Contains(u, "_pragma=busy_timeout") {
		u += sep + "_pragma=busy_timeout(5000)"
		sep = "&"
	}
	if !strings.Contains(u, "_pragma=foreign_keys") {
		u += sep + "_pragma=foreign_keys(1)"
	}

	return u
}

// Open creates a SQLite connection pool.
//
// SQLite only supports one writer at a time, so MaxOpenConns is forced to 1
// to prevent "database is locked" errors. MaxIdleConns and ConnMaxLifetime
// are set to sane defaults if not configured.
func (a *adapter) Open(cfg db.AdapterConfig) (*sql.DB, error) {
	dsn := a.DSN(cfg)
	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}

	// SQLite is single-writer: always cap at 1 open connection.
	sqlDB.SetMaxOpenConns(1)

	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 1
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}

	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(lifetime)

	return sqlDB, nil
}

// Ping verifies the SQLite connection is alive.
func (a *adapter) Ping(ctx context.Context, sqlDB *sql.DB) error {
	return sqlDB.PingContext(ctx)
}
