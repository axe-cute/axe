// Package mysql registers the MySQL 8.x adapter for the axe db package.
//
// Import this package for its side effects:
//
//	import _ "github.com/axe-cute/axe/pkg/db/mysql"
//
// DSN format (DATABASE_URL when DB_DRIVER=mysql):
//
//	mysql://user:pass@tcp(host:3306)/dbname
//	or: user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/axe-cute/axe/pkg/db"
	_ "github.com/go-sql-driver/mysql" // register "mysql" driver
)

const (
	driverName = "mysql"
	entDialect = "mysql"
)

func init() {
	db.Register("mysql", &adapter{})
}

type adapter struct{}

// DriverName returns the sql.Open driver name for go-sql-driver/mysql.
func (a *adapter) DriverName() string { return driverName }

// EntDialect returns the Ent ORM dialect for MySQL.
func (a *adapter) EntDialect() string { return entDialect }

// DSN converts a mysql:// URL to the go-sql-driver/mysql DSN format.
//
// Input:  mysql://user:pass@tcp(host:3306)/dbname
// Output: user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4&loc=UTC
//
// If the URL does not start with "mysql://", it is returned as-is
// (allows users to pass a raw go-sql-driver DSN directly).
func (a *adapter) DSN(cfg db.AdapterConfig) string {
	u := cfg.URL

	// Strip scheme prefix if present
	u = strings.TrimPrefix(u, "mysql://")

	// Ensure required params are present
	if !strings.Contains(u, "parseTime=") {
		if strings.Contains(u, "?") {
			u += "&parseTime=true"
		} else {
			u += "?parseTime=true"
		}
	}
	if !strings.Contains(u, "charset=") {
		u += "&charset=utf8mb4"
	}
	if !strings.Contains(u, "loc=") {
		u += "&loc=UTC"
	}

	return u
}

// Open creates a MySQL connection pool using go-sql-driver/mysql.
func (a *adapter) Open(cfg db.AdapterConfig) (*sql.DB, error) {
	dsn := a.DSN(cfg)
	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql: open: %w", err)
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

// Ping verifies the MySQL connection is alive.
func (a *adapter) Ping(ctx context.Context, sqlDB *sql.DB) error {
	return sqlDB.PingContext(ctx)
}
