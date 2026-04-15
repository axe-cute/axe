// Package new provides the `axe new <project-name>` command.
// It scaffolds a complete, production-ready axe project from scratch —
// equivalent to `rails new` or `django-admin startproject`.
package new

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// Options holds the configuration for project scaffolding.
type Options struct {
	// Module is the Go module path (e.g. "github.com/acme/blog-api").
	// If empty, it is derived interactively or defaults to "github.com/<name>/<name>".
	Module string

	// DB is the database driver: "postgres" (default), "mysql", or "sqlite".
	DB string

	// NoWorker removes Asynq background worker infrastructure.
	NoWorker bool

	// NoCache removes Redis cache infrastructure.
	NoCache bool

	// Yes skips all interactive prompts and uses defaults / flag values.
	Yes bool
}

// validModuleRe validates a Go module path like "github.com/org/repo".
var validModuleRe = regexp.MustCompile(`^[a-zA-Z0-9._~\-]+(/[a-zA-Z0-9._~\-]+)*$`)

// Command returns the `axe new` cobra command.
func Command() *cobra.Command {
	var opts Options

	cmd := &cobra.Command{
		Use:   "new <project-name>",
		Short: "Scaffold a new axe project",
		Long: `axe new creates a complete, production-ready Go project wired to the axe framework.

It generates the full directory structure, config, database layer, CLI tooling,
Docker / Compose files, Makefile, and README so you can run:

  cd <project-name> && make setup && make run

in under 5 minutes.`,
		Example: `  axe new blog-api
  axe new shop --db=mysql --module=github.com/acme/shop
  axe new lite --db=sqlite --no-worker --no-cache
  axe new myapp --yes   # non-interactive with all defaults`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = strings.TrimSpace(args[0])
			}

			// If no name provided and not --yes → launch interactive wizard.
			if name == "" && !opts.Yes {
				return runInteractive(&opts)
			}

			// Non-interactive path.
			if name == "" {
				return fmt.Errorf("project name is required (pass it as argument or omit --yes to use the wizard)")
			}

			if err := validateName(name); err != nil {
				return err
			}

			// Default module path.
			if opts.Module == "" {
				opts.Module = "github.com/" + name + "/" + name
			}
			if err := validateModule(opts.Module); err != nil {
				return err
			}

			// Validate DB driver.
			if err := validateDB(opts.DB); err != nil {
				return err
			}

			// Target directory must not already exist.
			target := filepath.Join(".", name)
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("directory %q already exists — choose a different name or remove it first", target)
			}

			return scaffold(name, target, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Module, "module", "", `Go module path (e.g. "github.com/acme/myapp"). Defaults to github.com/<name>/<name>`)
	cmd.Flags().StringVar(&opts.DB, "db", "postgres", `Database driver: postgres | mysql | sqlite`)
	cmd.Flags().BoolVar(&opts.NoWorker, "no-worker", false, "Omit Asynq background worker infrastructure")
	cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "Omit Redis cache infrastructure")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip interactive prompts; use flag values / defaults (CI-friendly)")

	return cmd
}

// ── Validation ────────────────────────────────────────────────────────────────

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	// Disallow path separators or spaces.
	if strings.ContainsAny(name, "/ \\ ") {
		return fmt.Errorf("project name %q must not contain spaces or path separators", name)
	}
	return nil
}

func validateModule(mod string) error {
	if mod == "" {
		return fmt.Errorf("module path cannot be empty")
	}
	if !validModuleRe.MatchString(mod) {
		return fmt.Errorf("module path %q is not a valid Go module path", mod)
	}
	return nil
}

func validateDB(driver string) error {
	switch driver {
	case "postgres", "mysql", "sqlite":
		return nil
	default:
		return fmt.Errorf("unsupported database driver %q — use: postgres, mysql, sqlite", driver)
	}
}
