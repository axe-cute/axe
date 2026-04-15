// Package db provides a pluggable database adapter interface for axe.
//
// Supported drivers: "postgres" (default), "mysql", "sqlite3".
//
// Usage in main.go:
//
//	import (
//	    "github.com/axe-cute/axe/pkg/db"
//	    _ "github.com/axe-cute/axe/pkg/db/postgres"
//	    _ "github.com/axe-cute/axe/pkg/db/mysql"
//	)
//
//	sqlDB, entDialect, err := db.Open(cfg.DBDriver, db.AdapterConfig{...})
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// AdapterConfig holds the connection parameters passed to every adapter.
type AdapterConfig struct {
	// URL is the full connection string (postgres DSN or MySQL DSN-like URL).
	URL string
	// MaxOpenConns is the maximum number of open connections.
	MaxOpenConns int
	// MaxIdleConns is the maximum number of idle connections.
	MaxIdleConns int
	// ConnMaxLifetime is the maximum lifetime of a connection.
	ConnMaxLifetime time.Duration
}

// Adapter abstracts over database drivers so that axe can support multiple
// databases (PostgreSQL, MySQL, SQLite) without changing core application code.
type Adapter interface {
	// DriverName returns the sql.Open driver name (e.g. "pgx", "mysql").
	DriverName() string

	// EntDialect returns the Ent ORM dialect constant
	// (e.g. "postgres", "mysql", "sqlite3").
	EntDialect() string

	// DSN formats the AdapterConfig into a driver-specific connection string.
	DSN(cfg AdapterConfig) string

	// Open creates and validates a *sql.DB connection pool.
	Open(cfg AdapterConfig) (*sql.DB, error)

	// Ping checks the database connectivity with a context timeout.
	Ping(ctx context.Context, db *sql.DB) error
}

// ── Registry ──────────────────────────────────────────────────────────────────

var (
	mu       sync.RWMutex
	adapters = map[string]Adapter{}
)

// Register makes an adapter available by driver name.
// It is typically called from an adapter package's init() function.
// Panics if the same driver name is registered twice.
func Register(name string, a Adapter) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := adapters[name]; dup {
		panic(fmt.Sprintf("db: adapter %q already registered", name))
	}
	adapters[name] = a
}

// Open selects the adapter for the given driver name, opens a pool,
// and returns the *sql.DB plus the Ent dialect string.
//
// The caller must import the adapter side-effect package:
//
//	import _ "github.com/axe-cute/axe/pkg/db/postgres"
func Open(driver string, cfg AdapterConfig) (*sql.DB, string, error) {
	mu.RLock()
	a, ok := adapters[driver]
	mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("db: unknown driver %q (did you import the adapter package?)", driver)
	}

	sqlDB, err := a.Open(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("db: open [%s]: %w", driver, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Ping(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, "", fmt.Errorf("db: ping [%s]: %w", driver, err)
	}

	return sqlDB, a.EntDialect(), nil
}

// Drivers returns the names of all registered adapters (sorted).
func Drivers() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	return names
}
