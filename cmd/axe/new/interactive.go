package new

import (
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
)

// runInteractive launches the interactive wizard and then calls scaffold.
// It is only triggered when no project name argument is given and --yes is not set.
func runInteractive(opts *Options) error {
	fmt.Println("🪓  axe new — interactive project wizard")
	fmt.Println("   Press Ctrl+C to cancel at any time.")
	fmt.Println()

	// ── Name ──────────────────────────────────────────────────────────────────
	var name string
	if err := survey.AskOne(&survey.Input{
		Message: "Project name:",
		Help:    "Lowercase, hyphen-separated. Example: blog-api",
	}, &name, survey.WithValidator(survey.Required)); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return err
	}

	// ── Module ────────────────────────────────────────────────────────────────
	defaultModule := "github.com/" + name + "/" + name
	var module string
	if err := survey.AskOne(&survey.Input{
		Message: "Go module path:",
		Default: defaultModule,
		Help:    "Example: github.com/acme/blog-api",
	}, &module); err != nil {
		return err
	}
	module = strings.TrimSpace(module)
	if module == "" {
		module = defaultModule
	}
	if err := validateModule(module); err != nil {
		return err
	}

	// ── Database ──────────────────────────────────────────────────────────────
	var db string
	if err := survey.AskOne(&survey.Select{
		Message: "Database driver:",
		Options: []string{"postgres", "mysql", "sqlite"},
		Default: "postgres",
		Description: func(value string, index int) string {
			switch value {
			case "postgres":
				return "PostgreSQL 16 (recommended)"
			case "mysql":
				return "MySQL 8"
			case "sqlite":
				return "SQLite — no Docker needed, great for local dev"
			}
			return ""
		},
	}, &db); err != nil {
		return err
	}

	// ── Features ──────────────────────────────────────────────────────────────
	allFeatures := []string{"Redis cache", "Asynq worker"}
	var selectedFeatures []string
	if err := survey.AskOne(&survey.MultiSelect{
		Message: "Optional features:",
		Options: allFeatures,
		Default: allFeatures, // all on by default
		Description: func(value string, index int) string {
			switch value {
			case "Redis cache":
				return "pkg/cache + pkg/ratelimit (requires Redis)"
			case "Asynq worker":
				return "pkg/worker — background job processing (requires Redis)"
			}
			return ""
		},
	}, &selectedFeatures); err != nil {
		return err
	}

	withCache := contains(selectedFeatures, "Redis cache")
	withWorker := contains(selectedFeatures, "Asynq worker")

	// ── Preview ───────────────────────────────────────────────────────────────
	fmt.Printf(`
  Project  : %s
  Module   : %s
  Database : %s
  Cache    : %v
  Worker   : %v

`, name, module, db, withCache, withWorker)

	// ── Confirm ───────────────────────────────────────────────────────────────
	var ok bool
	if err := survey.AskOne(&survey.Confirm{
		Message: "Generate project?",
		Default: true,
	}, &ok); err != nil {
		return err
	}
	if !ok {
		fmt.Println("Aborted.")
		return nil
	}

	// Build options and scaffold.
	opts.Module = module
	opts.DB = db
	opts.NoCache = !withCache
	opts.NoWorker = !withWorker

	target := "./" + name
	return scaffold(name, target, *opts)
}

// contains checks if slice contains the target string.
func contains(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
