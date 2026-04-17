// Package plugin provides the `axe plugin` command group.
// Currently supports: axe plugin add storage
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

// Command returns the `axe plugin` cobra command.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage axe plugins",
		Long:  `Add or remove plugins from an existing axe project.`,
	}

	cmd.AddCommand(addCmd())

	return cmd
}

func addCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "add <plugin-name>",
		Short:     "Add a plugin to the current project",
		Example:   "  axe plugin add storage",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"storage"},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(args[0])
			switch name {
			case "storage":
				return addStorage()
			default:
				return fmt.Errorf("unknown plugin %q — available: storage", name)
			}
		},
	}
}

func addStorage() error {
	// Detect project module from go.mod
	modPath := "go.mod"
	if _, err := os.Stat(modPath); err != nil {
		return fmt.Errorf("go.mod not found — run this from a project root created with `axe new`")
	}

	module, err := readModule(modPath)
	if err != nil {
		return err
	}

	fmt.Println("\n📦 Adding storage plugin...")

	// 1. Create pkg/storage/ files
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

	// 2. Inject config fields into config/config.go
	configPath := filepath.Join("config", "config.go")
	configData, _ := os.ReadFile(configPath)
	if strings.Contains(string(configData), "StorageBackend") {
		fmt.Printf("   ⏭  config/config.go already has storage fields\n")
	} else if err := injectAfterMarker(configPath, "// axe:plugin:config", storageConfigFields()); err != nil {
		fmt.Printf("   ⚠️  Could not auto-inject config fields. Add these to config/config.go manually:\n")
		fmt.Println(storageConfigFields())
	} else {
		fmt.Printf("   ✓ config/config.go (storage fields injected)\n")
	}

	// 3. Inject storage wiring into cmd/api/main.go
	mainPath := filepath.Join("cmd", "api", "main.go")
	injected := false

	// Import
	storageImport := fmt.Sprintf("\t\"%s/pkg/storage\"", module)
	if err := injectAfterMarker(mainPath, "// axe:wire:import", storageImport); err == nil {
		injected = true
	}

	// Init block (after axe:wire:handler)
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

	// Storage routes: GET public, POST/DELETE require JWT — secure by design
	restRouter.Route(cfg.StorageURLPrefix, func(r chi.Router) {
		r.Get("/*", storageHandler.HandleServe)
		r.Group(func(r chi.Router) {
			r.Use(jwtauth.ChiMiddleware(jwtSvc))
			r.Post("/", storageHandler.HandleUpload)
			r.Delete("/*", storageHandler.HandleDelete)
		})
	})`
	if err := injectAfterMarker(mainPath, "// axe:wire:handler", storageInit); err == nil {
		injected = true
	}

	if injected {
		fmt.Printf("   ✓ cmd/api/main.go (storage wired)\n")
	} else {
		fmt.Println("   ⚠️  Could not auto-wire main.go. Add storage setup manually.")
	}

	// 4. Add env vars to .env.example
	envPath := ".env.example"
	if err := appendToFile(envPath, storageEnvVars()); err == nil {
		fmt.Printf("   ✓ .env.example (storage vars added)\n")
	}

	// 5. Add uploads/ to .gitignore
	gitignorePath := ".gitignore"
	if err := appendToFile(gitignorePath, "\n# Uploads (storage plugin)\nuploads/\n"); err == nil {
		fmt.Printf("   ✓ .gitignore (uploads/ added)\n")
	}

	// 6. Create uploads directory
	_ = os.MkdirAll("uploads", 0o755)

	fmt.Println("\n✅ Storage plugin added! Usage:")
	fmt.Println("   curl -X POST http://localhost:8080/upload -F \"file=@photo.png\"")
	fmt.Println("   curl http://localhost:8080/upload/<key>")
	fmt.Println("   curl -X DELETE http://localhost:8080/upload/<key>")
	fmt.Println()

	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

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
	StorageURLPrefix   string ` + "`" + `env:"STORAGE_URL_PREFIX"    env-default:"/upload"` + "`" + ``
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

// injectAfterMarker finds a marker comment in a file and injects content after it.
// The dupKey is used to check if the content was already injected.
func injectAfterMarker(filePath, marker, content string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	src := string(data)
	idx := strings.Index(src, marker)
	if idx < 0 {
		return fmt.Errorf("marker %q not found in %s", marker, filePath)
	}

	// Check if content already injected by looking for a unique signature.
	// Use "storage.NewHandler" as the definitive check for the storage plugin.
	if strings.Contains(src, "storage.NewHandler") || strings.Contains(src, "pkg/storage") {
		// For import injection, check specifically for the import line
		if strings.Contains(content, "pkg/storage") && strings.Contains(src, "pkg/storage") {
			return nil
		}
		// For code injection, check for the handler init
		if strings.Contains(content, "storage.NewHandler") && strings.Contains(src, "storage.NewHandler") {
			return nil
		}
	}

	insertAt := idx + len(marker)
	result := src[:insertAt] + "\n" + content + src[insertAt:]

	return os.WriteFile(filePath, []byte(result), 0o644)
}

// appendToFile appends content to a file, checking for duplicates.
func appendToFile(filePath, content string) error {
	existing, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	// Check if already present
	if strings.Contains(string(existing), "STORAGE_BACKEND") {
		return nil // already has storage config
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
