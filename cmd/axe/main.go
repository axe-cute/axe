// axe CLI — Developer tooling for the axe Go framework.
//
// Usage:
//
//	axe new <project-name> [--db=postgres|mysql|sqlite] [--no-worker] [--no-cache]
//	axe generate resource <Name> --fields="field:type,..." [--belongs-to=Entity]
//	axe migrate create <name>
//	axe migrate up / down / status
//	axe version
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/axe-cute/axe/cmd/axe/generate"
	"github.com/axe-cute/axe/cmd/axe/migrate"
	axenew "github.com/axe-cute/axe/cmd/axe/new"
	"github.com/axe-cute/axe/cmd/axe/plugin"
)

var version = "0.1.0"

func main() {
	root := &cobra.Command{
		Use:   "axe",
		Short: "axe — Go framework CLI generator",
		Long: `axe is a developer tool for the axe Go web framework.
It generates production-grade Clean Architecture boilerplate so you can
ship a full CRUD resource in under 10 minutes.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Sub-commands
	root.AddCommand(versionCmd())
	root.AddCommand(axenew.Command())
	root.AddCommand(generate.Command())
	root.AddCommand(migrate.Command())
	root.AddCommand(plugin.Command())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print axe CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("axe version %s\n", version)
		},
	}
}
