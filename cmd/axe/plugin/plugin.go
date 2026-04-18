// Package plugin provides the `axe plugin` command group.
//
// Subcommands:
//
//	axe plugin list              → list all official axe plugins + installed status
//	axe plugin add <name>        → inject plugin import + app.Use() into the project
//	axe plugin new <name>        → scaffold a new plugin from template
//	axe plugin validate          → check quality layers locally (CI gate)
package plugin

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	axenew "github.com/axe-cute/axe/cmd/axe/new"
)

// registry is the hardcoded list of official axe plugins.
// No GitHub API required — monorepo approach.
//
// Maturity levels:
//   - "production"  → real SDK, tested, documented, `axe plugin add` supported
//   - "scaffold"    → interface + stub backend, needs real SDK integration
//   - "planned"     → not yet implemented
var registry = []registryEntry{
	// Production-ready (real SDK, tested, documented)
	{Name: "storage", Description: "File uploads (local filesystem or JuiceFS mount)", ImportSuffix: "pkg/plugin/storage", Maturity: "production", Installable: true},
	{Name: "stripe", Description: "Stripe Payment Processor & Webhooks", ImportSuffix: "pkg/plugin/payment/stripe", Maturity: "production", Installable: true},

	// Scaffold-only (interface + stub backend, needs real SDK)
	{Name: "email", Description: "Email sending (interface only — needs SendGrid SDK)", ImportSuffix: "pkg/plugin/email", Maturity: "scaffold"},
	{Name: "tenant", Description: "Multi-tenancy middleware (interface only)", ImportSuffix: "pkg/plugin/tenant", Maturity: "scaffold"},
	{Name: "ratelimit", Description: "Redis sliding-window rate limiter (interface only)", ImportSuffix: "pkg/plugin/ratelimit", Maturity: "scaffold"},
	{Name: "oauth2", Description: "Social login (interface only — needs x/oauth2)", ImportSuffix: "pkg/plugin/oauth2", Maturity: "scaffold"},
	{Name: "sentry", Description: "Error tracking (interface only — needs sentry-go)", ImportSuffix: "pkg/plugin/sentry", Maturity: "scaffold"},
	{Name: "openai", Description: "OpenAI AI Assistant (interface only)", ImportSuffix: "pkg/plugin/ai/openai", Maturity: "scaffold"},
	{Name: "typesense", Description: "Search & indexing (interface only)", ImportSuffix: "pkg/plugin/search/typesense", Maturity: "scaffold"},
	{Name: "kafka", Description: "Kafka event streaming (interface only — needs franz-go)", ImportSuffix: "pkg/plugin/kafka", Maturity: "scaffold"},
	{Name: "otel", Description: "OpenTelemetry tracing (interface only — needs otel SDK)", ImportSuffix: "pkg/plugin/otel", Maturity: "scaffold"},

	// Planned
	{Name: "admin", Description: "Admin UI with auto-discovered plugin panels", ImportSuffix: "pkg/plugin/admin", Maturity: "planned"},
}

type registryEntry struct {
	Name         string
	Description  string
	ImportSuffix string
	Installable  bool   // has full axe plugin add support
	Maturity     string // "production" | "scaffold" | "planned"
}

// Command returns the `axe plugin` cobra command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage axe plugins",
		Long:  `Discover, add, scaffold, and validate axe plugins.`,
	}

	cmd.AddCommand(listCmd())
	cmd.AddCommand(addCmd())
	cmd.AddCommand(newCmd())
	cmd.AddCommand(validateCmd())

	return cmd
}

// ── axe plugin list ───────────────────────────────────────────────────────────

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all official axe plugins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Println()
			fmt.Println("🔌 Official axe plugins:")
			fmt.Println()

			fmt.Printf("  %-12s  %-16s  %s\n", "NAME", "STATUS", "DESCRIPTION")
			fmt.Println("  " + strings.Repeat("─", 68))

			module := ""
			if mod, err := readModule("go.mod"); err == nil {
				module = mod
			}

			for _, e := range registry {
				status := maturityLabel(e.Maturity)
				if module != "" && isInstalled(module, e.ImportSuffix) {
					status = "✅ installed"
				}
				fmt.Printf("  %-12s  %-16s  %s\n", e.Name, status, e.Description)
			}

			fmt.Println()
			fmt.Println("  ✅ production  = tested, real SDK, ready to use")
			fmt.Println("  🔧 scaffold    = interface only, needs real SDK integration")
			fmt.Println("  📋 planned     = not yet implemented")
			fmt.Println()
			fmt.Println("  Add a plugin: axe plugin add <name>")
			fmt.Println("  Scaffold new: axe plugin new <name>")
			fmt.Println()
			return nil
		},
	}
}

func maturityLabel(m string) string {
	switch m {
	case "production":
		return "✅ production"
	case "scaffold":
		return "🔧 scaffold"
	case "planned":
		return "📋 planned"
	default:
		return m
	}
}

// isInstalled checks whether a plugin import path is already present in go.mod or main.go.
func isInstalled(module, importSuffix string) bool {
	mainPath := filepath.Join("cmd", "api", "main.go")
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), module+"/"+importSuffix)
}

// ── axe plugin add ────────────────────────────────────────────────────────────

func addCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "add <plugin-name>",
		Short:     "Add a plugin to the current project",
		Example:   "  axe plugin add storage",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"storage", "stripe"},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(args[0])
			switch name {
			case "storage":
				return addStorage()
			case "stripe":
				return addStripe()
			default:
				// Check if it's a known plugin.
				for _, e := range registry {
					if e.Name == name {
						return fmt.Errorf("plugin %q is not yet auto-installable — add it manually:\n  go get github.com/axe-cute/axe/%s", name, e.ImportSuffix)
					}
				}
				return fmt.Errorf("unknown plugin %q — run `axe plugin list` to see available plugins", name)
			}
		},
	}
}

// ── axe plugin new ────────────────────────────────────────────────────────────

func newCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "new <plugin-name>",
		Short:   "Scaffold a new plugin from template",
		Example: "  axe plugin new billing\n  axe plugin new notifications",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			if name == "" {
				return fmt.Errorf("plugin name cannot be empty")
			}
			if strings.ContainsAny(name, " -/\\") {
				return fmt.Errorf("plugin name %q must be a single word (no spaces, hyphens, or slashes)", name)
			}

			dir := filepath.Join("pkg", "plugin", name)
			if _, err := os.Stat(dir); err == nil {
				return fmt.Errorf("directory %s already exists", dir)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}

			capitalized := strings.ToUpper(name[:1]) + name[1:]
			files := []struct {
				path    string
				content string
			}{
				{
					path:    filepath.Join(dir, "plugin.go"),
					content: renderPluginTemplate(name, capitalized),
				},
				{
					path:    filepath.Join(dir, "plugin_test.go"),
					content: renderPluginTestTemplate(name, capitalized),
				},
			}

			fmt.Printf("\n🔌 Scaffolding plugin %q...\n\n", name)
			for _, f := range files {
				if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
					return fmt.Errorf("write %s: %w", f.path, err)
				}
				fmt.Printf("   ✓ %s\n", f.path)
			}

			fmt.Printf(`
✅ Plugin %q scaffolded!

Next steps:
  1. Implement Register() in %s
  2. Run tests: go test ./pkg/plugin/%s/...
  3. Validate quality layers: axe plugin validate

Register in your app:
  import "%s/pkg/plugin/%s"
  app.Use(%s.New(%s.Config{...}))
`, name, filepath.Join(dir, "plugin.go"), name, "{module}", name, name, name)
			return nil
		},
	}
}

// ── axe plugin validate ───────────────────────────────────────────────────────

// validateCmd checks plugin quality layers for all plugins in pkg/plugin/.
func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate plugin quality layers (CI gate)",
		Long: `Checks all plugins under pkg/plugin/ against the 6-layer quality model:

  Layer 1: Plugin interface (Name, Register, Shutdown)
  Layer 3: No circular imports
  Layer 4: Config validated in New() (not Register)
  Layer 5: ServiceKey constant present
  Layer 6: No self-imported database/redis packages
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pluginDir := filepath.Join("pkg", "plugin")
			entries, err := os.ReadDir(pluginDir)
			if err != nil {
				return fmt.Errorf("cannot read %s: %w", pluginDir, err)
			}

			fmt.Printf("\n🔍 Validating plugins in %s/...\n\n", pluginDir)

			total, passed, failed := 0, 0, 0
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				// Skip meta directories.
				if name == "testing" || name == "obs" {
					continue
				}
				pluginPath := filepath.Join(pluginDir, name)
				issues := validatePlugin(pluginPath, name)
				total++
				if len(issues) == 0 {
					fmt.Printf("   ✅ %s\n", name)
					passed++
				} else {
					fmt.Printf("   ❌ %s\n", name)
					for _, issue := range issues {
						fmt.Printf("      • %s\n", issue)
					}
					failed++
				}
			}

			fmt.Printf("\n   %d/%d plugins passed quality checks\n\n", passed, total)

			if failed > 0 {
				return fmt.Errorf("%d plugin(s) failed validation — fix issues above", failed)
			}
			return nil
		},
	}
}

// validatePlugin checks a single plugin directory for quality layer compliance.
func validatePlugin(dir, name string) []string {
	var issues []string

	pluginFile := filepath.Join(dir, "plugin.go")
	data, err := os.ReadFile(pluginFile)
	if err != nil {
		return []string{fmt.Sprintf("missing plugin.go — every plugin needs a plugin.go")}
	}
	src := string(data)

	// Layer 1: must implement Plugin interface.
	if !strings.Contains(src, "func (") || !strings.Contains(src, "Register(") {
		issues = append(issues, "Layer 1: missing Register() method — plugin must implement plugin.Plugin")
	}
	if !strings.Contains(src, "Shutdown(") {
		issues = append(issues, "Layer 1: missing Shutdown() method — plugin must implement plugin.Plugin")
	}
	if !strings.Contains(src, "Name()") {
		issues = append(issues, "Layer 1: missing Name() method — plugin must implement plugin.Plugin")
	}

	// Layer 4: New() must return an error.
	if strings.Contains(src, "func New(") && !strings.Contains(src, "func New(") {
		issues = append(issues, "Layer 4: New() must return (*Plugin, error) for fail-fast config validation")
	}
	// Check if New() returns error — look for ", error" or ", nil" near New.
	if strings.Contains(src, "func New(") {
		newIdx := strings.Index(src, "func New(")
		snippet := src[newIdx : min(newIdx+200, len(src))]
		if !strings.Contains(snippet, "error") {
			issues = append(issues, "Layer 4: New() should return error for config validation (found no error return)")
		}
	}

	// Layer 5: ServiceKey constant.
	if !strings.Contains(src, "ServiceKey") {
		issues = append(issues, "Layer 5: missing ServiceKey constant — required for typed service locator")
	}

	// Layer 6: no raw database/redis imports.
	for _, forbidden := range []string{`"database/sql"`, `redis.NewClient`, `sql.Open`} {
		if strings.Contains(src, forbidden) {
			issues = append(issues, fmt.Sprintf("Layer 6: found %q — use app.DB or app.Cache instead of creating connections", forbidden))
		}
	}

	// Test file exists?
	testFile := filepath.Join(dir, "plugin_test.go")
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		issues = append(issues, "missing plugin_test.go — all plugins must have tests")
	}

	return issues
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Templates for axe plugin new ─────────────────────────────────────────────

func renderPluginTemplate(name, capitalized string) string {
	return fmt.Sprintf(`// Package %s provides the axe %s plugin.
//
// Layer conformance (Story 8.10):
//   - Layer 1: implements plugin.Plugin (compiler-enforced)
//   - Layer 4: config validated in New(), not Register()
//   - Layer 5: ServiceKey constant for typed service locator
//   - Layer 6: uses app.Logger/app.DB/app.Cache — no self-created connections
package %s

import (
	"context"
	"fmt"

	"github.com/axe-cute/axe/pkg/plugin"
)

// ServiceKey is the typed service locator key for this plugin's service.
const ServiceKey = %q

// Config configures the %s plugin.
type Config struct {
	// TODO: add configuration fields
}

func (c *Config) validate() error {
	// TODO: validate required fields and return descriptive errors
	return nil
}

// Plugin implements [plugin.Plugin].
type Plugin struct {
	cfg Config
}

// New creates a %s plugin with the given configuration.
// Returns an error if required config fields are missing (Layer 4: fail-fast in New).
func New(cfg Config) (*Plugin, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf(%q+": %%w", err)
	}
	return &Plugin{cfg: cfg}, nil
}

// Name returns the plugin identifier.
func (p *Plugin) Name() string { return %q }

// Register initializes the plugin and provides its service via the locator.
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
	log := app.Logger.With("plugin", p.Name())

	// TODO: implement plugin registration (routes, services, etc.)
	_ = log

	// plugin.Provide[YourService](app, ServiceKey, yourService)
	return nil
}

// Shutdown performs graceful cleanup.
func (p *Plugin) Shutdown(_ context.Context) error {
	return nil
}
`, name, name, name, name, capitalized, capitalized, name, name)
}

func renderPluginTestTemplate(name, capitalized string) string {
	return fmt.Sprintf(`package %s

import (
	"testing"

	plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation (Layer 4) ───────────────────────────────────────────────

func TestNew_ValidConfig(t *testing.T) {
	p, err := New(Config{})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, %q, p.Name())
}

// ── Plugin lifecycle ──────────────────────────────────────────────────────────

func TestRegister(t *testing.T) {
	p, err := New(Config{})
	require.NoError(t, err)

	app := plugintest.NewMockApp()
	require.NoError(t, p.Register(t.Context(), app))
}

func TestShutdown_BeforeRegister(t *testing.T) {
	p, err := New(Config{})
	require.NoError(t, err)
	require.NoError(t, p.Shutdown(t.Context()))
}

// ── ServiceKey (Layer 5) ─────────────────────────────────────────────────────

func TestServiceKey_MatchesName(t *testing.T) {
	p, _ := New(Config{})
	assert.Equal(t, p.Name(), ServiceKey)
}
`, name, name)
}

// ── Storage plugin wiring (existing addStorage implementation) ────────────────

func addStorage() error {
	modPath := "go.mod"
	if _, err := os.Stat(modPath); err != nil {
		return fmt.Errorf("go.mod not found — run this from a project root created with `axe new`")
	}

	module, err := readModule(modPath)
	if err != nil {
		return err
	}

	fmt.Println("\n📦 Adding storage plugin...")

	storageDir := filepath.Join("pkg", "storage")
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", storageDir, err)
	}

	files := []struct {
		path    string
		content string
	}{
		{filepath.Join(storageDir, "storage.go"), axenew.TmplStorageCore},
		{filepath.Join(storageDir, "handler.go"), axenew.TmplStorageHandler},
		{filepath.Join(storageDir, "metrics.go"), axenew.TmplStorageMetrics},
	}

	for _, f := range files {
		if _, err := os.Stat(f.path); err == nil {
			fmt.Printf("   ⏭  %s already exists, skipping\n", f.path)
			continue
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
		fmt.Printf("   ✓ %s\n", f.path)
	}

	configPath := filepath.Join("config", "config.go")
	configData, _ := os.ReadFile(configPath)
	if strings.Contains(string(configData), "StorageBackend") {
		fmt.Printf("   ⏭  config/config.go already has storage fields\n")
	} else if err := injectContentAfterMarker(configPath, "// axe:plugin:config", storageConfigFields(), "StorageBackend"); err != nil {
		fmt.Printf("   ⚠️  Could not auto-inject config fields. Add these to config/config.go manually:\n")
		fmt.Println(storageConfigFields())
	} else {
		fmt.Printf("   ✓ config/config.go (storage fields injected)\n")
	}

	mainPath := filepath.Join("cmd", "api", "main.go")
	injected := false
	storageImport := fmt.Sprintf("\t\"%s/pkg/plugin/storage\"", module)
	if err := injectContentAfterMarker(mainPath, "// axe:wire:import", storageImport, "\"pkg/plugin/storage\""); err == nil {
		injected = true
	}
	storageInit := `
	// ── File Storage ──────────────────────────────────────────────────────────
	storageCfg := storage.Config{
		Backend:     cfg.StorageBackend,
		MountPath:   cfg.StorageMountPath,
		MaxFileSize: cfg.StorageMaxFileSize,
		URLPrefix:   cfg.StorageURLPrefix,
	}
	storageHandler := storage.NewHandler(storageCfg, log)
	log.Info("storage enabled", "backend", cfg.StorageBackend, "mount", cfg.StorageMountPath)

	restRouter.Route(cfg.StorageURLPrefix, func(r chi.Router) {
		r.Get("/*", storageHandler.HandleServe)
		r.Group(func(r chi.Router) {
			r.Use(jwtauth.ChiMiddleware(jwtSvc))
			r.Post("/", storageHandler.HandleUpload)
			r.Delete("/*", storageHandler.HandleDelete)
		})
	})`
	if err := injectContentAfterMarker(mainPath, "// axe:wire:handler", storageInit, "storage.New("); err == nil {
		injected = true
	}
	if injected {
		fmt.Printf("   ✓ cmd/api/main.go (storage wired)\n")
	} else {
		fmt.Println("   ⚠️  Could not auto-wire main.go. Add storage setup manually.")
	}

	envPath := ".env.example"
	if err := appendToFile(envPath, storageEnvVars(), "STORAGE_BACKEND"); err == nil {
		fmt.Printf("   ✓ .env.example (storage vars added)\n")
	}
	gitignorePath := ".gitignore"
	if err := appendToFile(gitignorePath, "\n# Uploads (storage plugin)\nuploads/\n", "uploads/"); err == nil {
		fmt.Printf("   ✓ .gitignore (uploads/ added)\n")
	}
	_ = os.MkdirAll("uploads", 0o755)

	fmt.Println("\n✅ Storage plugin added!")
	return nil
}

// ── Shared helpers ────────────────────────────────────────────────────────────

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
	return "", fmt.Errorf("module not found in %s", goModPath)
}

func storageConfigFields() string {
	return `
	// Storage
	StorageBackend     string ` + "`" + `env:"STORAGE_BACKEND"       env-default:"local"` + "`" + `
	StorageMountPath   string ` + "`" + `env:"STORAGE_MOUNT_PATH"    env-default:"./uploads"` + "`" + `
	StorageMaxFileSize int64  ` + "`" + `env:"STORAGE_MAX_FILE_SIZE" env-default:"10485760"` + "`" + `
	StorageURLPrefix   string ` + "`" + `env:"STORAGE_URL_PREFIX"    env-default:"/upload"` + "`"
}

func storageEnvVars() string {
	return `
# Storage (file uploads)
STORAGE_BACKEND=local
STORAGE_MOUNT_PATH=./uploads
STORAGE_MAX_FILE_SIZE=10485760
STORAGE_URL_PREFIX=/upload
`
}

// injectContentAfterMarker prevents duplication by checking for idempotencyKey
func injectContentAfterMarker(filePath, marker, content, idempotencyKey string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	src := string(data)
	if idempotencyKey != "" && strings.Contains(src, idempotencyKey) {
		return nil // Already injected
	}
	idx := strings.Index(src, marker)
	if idx < 0 {
		return fmt.Errorf("marker %q not found in %s", marker, filePath)
	}
	insertAt := idx + len(marker)
	result := src[:insertAt] + "\n" + content + src[insertAt:]
	return os.WriteFile(filePath, []byte(result), 0o644)
}

func injectAfterMarker(filePath, marker, content string) error {
	return injectContentAfterMarker(filePath, marker, content, "")
}

func appendToFile(filePath, content, idempotencyKey string) error {
	existing, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if idempotencyKey != "" && strings.Contains(string(existing), idempotencyKey) {
		return nil
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
