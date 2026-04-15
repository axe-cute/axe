// Package postgres registers the PostgreSQL adapter for the axe db package.
//
// Import this package for its side effects:
//
//	import _ "github.com/axe-cute/axe/pkg/db/postgres"
package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/axe-cute/axe/pkg/db"
	_ "github.com/jackc/pgx/v5/stdlib" // register "pgx" driver
)

const (
	driverName = "pgx"
	entDialect = "postgres"
)

func init() {
	db.Register("postgres", &adapter{})
}

type adapter struct{}

// DriverName returns the sql.Open driver name used by pgx stdlib.
func (a *adapter) DriverName() string { return driverName }

// EntDialect returns the Ent ORM dialect for PostgreSQL.
func (a *adapter) EntDialect() string { return entDialect }

// DSN returns the connection string as-is — pgx accepts standard PostgreSQL URLs.
// Example: postgres://user:pass@localhost:5432/dbname?sslmode=disable
func (a *adapter) DSN(cfg db.AdapterConfig) string {
	return cfg.URL
}

// Open creates a PostgreSQL connection pool using the pgx stdlib driver.
func (a *adapter) Open(cfg db.AdapterConfig) (*sql.DB, error) {
	sqlDB, err := sql.Open(driverName, a.DSN(cfg))
	if err != nil {
		return nil, err
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 25
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 5
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}

	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(lifetime)

	return sqlDB, nil
}

// Ping verifies the database connection is alive.
func (a *adapter) Ping(ctx context.Context, sqlDB *sql.DB) error {
	return sqlDB.PingContext(ctx)
}
