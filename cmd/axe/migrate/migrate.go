// Package migrate provides axe CLI commands for database migrations.
package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

const migrationsDir = "db/migrations"

// Command returns the `axe migrate` cobra command.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration commands",
	}
	cmd.AddCommand(createCmd(), upCmd(), downCmd(), statusCmd())
	return cmd
}

// createCmd creates a new timestamped migration file.
func createCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new migration file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.ReplaceAll(args[0], " ", "_"))
			timestamp := time.Now().Format("20060102150405")
			filename := fmt.Sprintf("%s_%s.sql", timestamp, name)
			path := filepath.Join(migrationsDir, filename)

			content := fmt.Sprintf(
				"-- Migration: %s\n-- Description: %s\n-- Created: %s\n\n-- +migrate Up\n\n\n-- +migrate Down\n\n",
				filename, name, time.Now().Format("2006-01-02"),
			)

			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("create migration: %w", err)
			}
			fmt.Printf("✅ Created migration: %s\n", path)
			return nil
		},
	}
}

// upCmd applies all pending SQL migrations in sorted order.
func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			conn, err := openDB(ctx)
			if err != nil {
				return err
			}
			defer conn.Close(ctx)

			if err := ensureMigrationsTable(ctx, conn); err != nil {
				return err
			}

			files, err := pendingMigrations(ctx, conn)
			if err != nil {
				return err
			}

			if len(files) == 0 {
				fmt.Println("✅ No pending migrations.")
				return nil
			}

			for _, f := range files {
				if err := applyMigration(ctx, conn, f); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// downCmd removes the last applied migration record.
// It does NOT auto-reverse the SQL — write a -- +migrate Down section or a new migration.
func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Remove the last applied migration record (does not reverse SQL)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			conn, err := openDB(ctx)
			if err != nil {
				return err
			}
			defer conn.Close(ctx)

			if err := ensureMigrationsTable(ctx, conn); err != nil {
				return err
			}

			var last string
			err = conn.QueryRow(ctx,
				`SELECT filename FROM schema_migrations ORDER BY applied_at DESC LIMIT 1`,
			).Scan(&last)
			if err == pgx.ErrNoRows {
				fmt.Println("⚠️  No applied migrations to roll back.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("query last migration: %w", err)
			}

			_, err = conn.Exec(ctx,
				`DELETE FROM schema_migrations WHERE filename = $1`, last,
			)
			if err != nil {
				return fmt.Errorf("remove migration record: %w", err)
			}
			fmt.Printf("↩️  Removed migration record: %s\n", last)
			fmt.Println("   SQL was NOT reversed. Write a new migration or run manually.")
			return nil
		},
	}
}

// statusCmd shows applied and pending migrations.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			conn, err := openDB(ctx)
			if err != nil {
				return err
			}
			defer conn.Close(ctx)

			if err := ensureMigrationsTable(ctx, conn); err != nil {
				return err
			}

			applied, err := appliedMigrations(ctx, conn)
			if err != nil {
				return err
			}

			files, err := sqlFiles()
			if err != nil {
				return err
			}

			fmt.Printf("%-55s  %s\n", "MIGRATION", "STATUS")
			fmt.Println(strings.Repeat("─", 70))
			for _, f := range files {
				status := "pending"
				if _, ok := applied[f]; ok {
					status = "applied"
				}
				fmt.Printf("%-55s  %s\n", f, status)
			}
			return nil
		},
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// openDB connects to Postgres using DATABASE_URL from the environment.
// If a .env file exists in the current directory, it is loaded first.
func openDB(ctx context.Context) (*pgx.Conn, error) {
	loadDotEnv(".env")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set (check your .env file)")
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return conn, nil
}

// loadDotEnv reads KEY=VALUE pairs from path into the process environment.
// Lines starting with # and blank lines are ignored. Already-set env vars
// are NOT overwritten (env takes priority over the file).
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // no .env file — rely solely on environment
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Strip inline comments (e.g. "value  # comment")
		if idx := strings.Index(v, " #"); idx != -1 {
			v = strings.TrimSpace(v[:idx])
		}
		// Don't overwrite already-set variables
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

// ensureMigrationsTable creates the schema_migrations tracking table if needed.
func ensureMigrationsTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}
	return nil
}

// appliedMigrations returns a set of filenames already recorded in schema_migrations.
func appliedMigrations(ctx context.Context, conn *pgx.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = struct{}{}
	}
	return applied, rows.Err()
}

// sqlFiles returns all .sql filenames in migrationsDir, sorted alphabetically.
func sqlFiles() ([]string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", migrationsDir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// pendingMigrations returns files that have not yet been applied.
func pendingMigrations(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return nil, err
	}
	all, err := sqlFiles()
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, f := range all {
		if _, ok := applied[f]; !ok {
			pending = append(pending, f)
		}
	}
	return pending, nil
}

// applyMigration reads a SQL file and executes it inside a transaction.
// On success it records the filename in schema_migrations.
func applyMigration(ctx context.Context, conn *pgx.Conn, filename string) error {
	path := filepath.Join(migrationsDir, filename)
	sql, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", filename, err)
	}

	fmt.Printf("→ Applying %s ... ", filename)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("\n  ❌ migration %s failed: %w", filename, err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (filename) VALUES ($1)`, filename,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", filename, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filename, err)
	}

	fmt.Println("✅")
	return nil
}
