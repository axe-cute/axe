// Package doctor provides the `axe doctor` command — diagnoses and (with
// --fix) auto-repairs known issues in projects scaffolded by older versions
// of the axe CLI.
//
// Each diagnostic is implemented as a [check] with a Detect step that returns
// an issue when something is wrong, and a Fix step that rewrites the
// offending file(s) to the canonical form produced by the current scaffold
// templates.
//
// Usage:
//
//	axe doctor              # report issues only
//	axe doctor --fix        # apply fixes in place
package doctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	axenew "github.com/axe-cute/axe/cmd/axe/new"
)

// Command returns the `axe doctor` cobra command.
func Command() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose (and optionally repair) projects scaffolded by older axe versions",
		Long: `axe doctor scans the current project for known issues introduced
by older scaffold templates and reports them. With --fix, it rewrites the
offending files to match the current canonical templates.

Run from your project root (the directory containing go.mod).`,
		Example: `  axe doctor             # report issues
  axe doctor --fix       # apply fixes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(".", fix, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply fixes in place (default: report only)")
	return cmd
}

// run executes all checks against the project rooted at projectDir.
func run(projectDir string, fix bool, out interface{ Write([]byte) (int, error) }) error {
	module, err := readModule(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		return fmt.Errorf("not an axe project (could not read go.mod): %w", err)
	}

	fmt.Fprintf(out, "axe doctor — scanning project (module=%s)\n\n", module)

	checks := allChecks()
	var issues, fixed int
	for _, c := range checks {
		issue, err := c.Detect(projectDir, module)
		if err != nil {
			return fmt.Errorf("check %s: %w", c.ID, err)
		}
		if issue == nil {
			fmt.Fprintf(out, "  ✓ %-32s  ok\n", c.ID)
			continue
		}
		issues++
		fmt.Fprintf(out, "  ✗ %-32s  %s\n", c.ID, issue.Summary)
		for _, d := range issue.Details {
			fmt.Fprintf(out, "      %s\n", d)
		}
		if fix {
			if err := c.Fix(projectDir, module, issue); err != nil {
				return fmt.Errorf("fix %s: %w", c.ID, err)
			}
			fixed++
			fmt.Fprintf(out, "      ↳ fixed: %s\n", strings.Join(issue.Files, ", "))
		}
	}

	fmt.Fprintln(out)
	switch {
	case issues == 0:
		fmt.Fprintln(out, "All checks passed.")
		return nil
	case fix:
		fmt.Fprintf(out, "Done — %d issue(s) detected, %d fixed.\n", issues, fixed)
		return nil
	default:
		fmt.Fprintf(out, "Found %d issue(s). Re-run with --fix to repair.\n", issues)
		return fmt.Errorf("doctor: %d issue(s) found", issues)
	}
}

// ── Check framework ───────────────────────────────────────────────────────────

// check is a single diagnostic.
type check struct {
	ID     string
	Detect func(projectDir, module string) (*issue, error)
	Fix    func(projectDir, module string, i *issue) error
}

// issue is the result of a failed Detect.
type issue struct {
	Summary string
	Details []string
	Files   []string
}

func allChecks() []check {
	return []check{
		legacySetupPluginCheck(),
	}
}

// ── Check: legacy setup/plugin.go referencing storage.New ─────────────────────

// legacySetupPluginCheck detects the old `tmplSetupPlugin` output (pre-v0.1.10)
// which embedded a storage plugin block calling storage.New / app.Use. The
// scaffolded `pkg/storage` (now `internal/infra/storage`) never exposed a
// `New` constructor or implemented plugin.Plugin, so any project carrying
// that template fails to compile.
//
// Fix: rewrite internal/setup/plugin.go from the current canonical template.
func legacySetupPluginCheck() check {
	return check{
		ID: "legacy-setup-plugin",
		Detect: func(projectDir, _ string) (*issue, error) {
			path := filepath.Join(projectDir, "internal/setup/plugin.go")
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, nil
				}
				return nil, err
			}
			content := string(data)
			// Heuristic: legacy template embedded `storage.New(storage.Config{`
			// and `storagePlug` inside RegisterPlugins.
			if !strings.Contains(content, "storage.New(storage.Config{") &&
				!strings.Contains(content, "storagePlug") {
				return nil, nil
			}
			return &issue{
				Summary: "legacy storage plugin block in internal/setup/plugin.go",
				Details: []string{
					"Old scaffold templates wired storage as a plugin, but pkg/storage exposes NewHandler, not New.",
					"Storage is now wired directly in cmd/api/main.go via storage.NewHandler.",
				},
				Files: []string{path},
			}, nil
		},
		Fix: func(projectDir, module string, _ *issue) error {
			path := filepath.Join(projectDir, "internal/setup/plugin.go")
			return os.WriteFile(path, []byte(axenew.RenderSetupPlugin(module)), 0o644)
		},
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// readModule extracts the module path from a go.mod file.
func readModule(goModPath string) (string, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module directive not found in %s", goModPath)
}
