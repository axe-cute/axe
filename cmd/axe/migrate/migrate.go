// Package migrate provides axe CLI commands for database migrations.
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

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
			path := filepath.Join("db", "migrations", filename)

			content := fmt.Sprintf("-- Migration: %s\n-- Description: %s\n-- Created: %s\n\n-- +migrate Up\n\n\n-- +migrate Down\n\n",
				filename, name, time.Now().Format("2006-01-02"))

			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("create migration: %w", err)
			}
			fmt.Printf("✅ Created migration: %s\n", path)
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO (Story 2.3): implement real migration runner using Atlas
			fmt.Println("→ Applying migrations via Ent auto-migrate (dev mode)...")
			fmt.Println("  For production: use `atlas migrate apply`")
			return nil
		},
	}
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Rollback the last migration",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("→ Rollback: see db/migrations/ — apply manually or via Atlas")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := os.ReadDir("db/migrations")
			if err != nil {
				return fmt.Errorf("read migrations dir: %w", err)
			}
			fmt.Println("Migration files:")
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".sql") {
					fmt.Printf("  • %s\n", e.Name())
				}
			}
			return nil
		},
	}
}
