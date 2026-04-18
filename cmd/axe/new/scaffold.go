package new

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// TemplateData is passed to every project-level template.
type TemplateData struct {
	Name        string // e.g. "blog-api"
	NameTitle   string // e.g. "Blog-Api"
	NameUpper   string // e.g. "BLOG_API" (for env var prefixes)
	Module      string // e.g. "github.com/acme/blog-api"
	DB          string // "postgres" | "mysql" | "sqlite"
	WithWorker  bool
	WithCache   bool
	WithMetrics bool
	WithStorage bool
}

// dbConfig groups per-driver DSN defaults used inside templates.
type dbConfig struct {
	Driver      string // go/ent dialect: "postgres", "mysql", "sqlite3"
	ExampleURL  string // shown inside .env.example
	DockerImage string // used in docker-compose.yml
	EnvName     string // e.g. "PostgreSQL", "MySQL", "SQLite"
}

var dbConfigs = map[string]dbConfig{
	"postgres": {
		Driver:      "postgres",
		ExampleURL:  "postgres://{{.Name}}:{{.Name}}@localhost:5432/{{.Name}}_dev?sslmode=disable",
		DockerImage: "postgres:16-alpine",
		EnvName:     "PostgreSQL",
	},
	"mysql": {
		Driver:      "mysql",
		ExampleURL:  "{{.Name}}:{{.Name}}@tcp(localhost:3306)/{{.Name}}_dev?parseTime=true",
		DockerImage: "mysql:8-debian",
		EnvName:     "MySQL",
	},
	"sqlite": {
		Driver:      "sqlite3",
		ExampleURL:  "file:{{.Name}}_dev.db?_foreign_keys=on",
		DockerImage: "", // no docker service needed
		EnvName:     "SQLite",
	},
}

// scaffold creates the full project directory tree and writes all template files.
func scaffold(name, target string, opts Options) error {
	dbc := dbConfigs[opts.DB]

	data := TemplateData{
		Name:        name,
		NameTitle:   titleWords(strings.ReplaceAll(name, "-", " "), " ", "-"),
		NameUpper:   strings.ToUpper(strings.ReplaceAll(name, "-", "_")),
		Module:      opts.Module,
		DB:          opts.DB,
		WithWorker:  !opts.NoWorker,
		WithCache:   !opts.NoCache,
		WithMetrics: true,
		WithStorage: opts.WithStorage,
	}

	fmt.Printf("\n🪓  axe new — scaffolding %q\n", name)
	fmt.Printf("   module  : %s\n", opts.Module)
	fmt.Printf("   database: %s (%s)\n", dbc.EnvName, dbc.Driver)
	fmt.Printf("   worker  : %v\n", data.WithWorker)
	fmt.Printf("   cache   : %v\n", data.WithCache)
	fmt.Printf("   storage : %v\n\n", data.WithStorage)

	// ── 1. Create directory tree ──────────────────────────────────────────────
	dirs := []string{
		"cmd/api",
		"cmd/axe",
		"config",
		"db/migrations",
		"db/queries",
		"docs",
		"ent/schema",
		"internal/domain",
		"internal/handler/middleware",
		"internal/handler/hook",
		"internal/setup",
		"internal/repository",
		"internal/service",
		"pkg/apperror",
		"pkg/jwtauth",
		"pkg/logger",
		"pkg/metrics",
		"pkg/txmanager",
		"pkg/ws",
		"pkg/devroutes",
	}
	if data.WithCache {
		dirs = append(dirs, "pkg/cache", "pkg/ratelimit")
	}
	if data.WithWorker {
		dirs = append(dirs, "pkg/worker")
	}
	if data.WithStorage {
		dirs = append(dirs, "pkg/storage", "uploads")
	}

	for _, d := range dirs {
		full := filepath.Join(target, d)
		if err := os.MkdirAll(full, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	// ── 2. Write all files ────────────────────────────────────────────────────
	files := buildFileList(data, dbc)
	generated := 0
	for _, f := range files {
		path := filepath.Join(target, f.path)
		if err := renderAndWrite(path, f.tmpl, data); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
		fmt.Printf("   ✓ %s\n", f.path)
		generated++
	}

	// Write cmd/axe/main.go directly — bypasses renderAndWrite's template
	// re-processing, because this file already contains {{.Name}} markers
	// inside embedded Go template const strings.
	axeMainPath := filepath.Join(target, "cmd/axe/main.go")
	if err := os.MkdirAll(filepath.Dir(axeMainPath), 0o755); err != nil {
		return fmt.Errorf("create cmd/axe: %w", err)
	}
	if err := os.WriteFile(axeMainPath, []byte(tmplMainAxeGo(data)), 0o644); err != nil {
		return fmt.Errorf("write cmd/axe/main.go: %w", err)
	}
	fmt.Println("   ✓ cmd/axe/main.go")
	generated++

	// ── 3. Resolve dependencies (populate go.sum) ──────────────────────────────
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = "/usr/local/go/bin/go"
	}
	fmt.Println("\n   ⏳ Running go mod tidy...")
	cmd := exec.Command(goBin, "mod", "tidy") //nolint:gosec
	cmd.Dir = target
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Non-fatal: surface the warning but don't abort, since the project
		// files are already written. The user can run `go mod tidy` manually.
		fmt.Printf("   ⚠️  go mod tidy failed: %v\n", err)
		fmt.Println("      Run `go mod tidy` inside the new project directory to fix this.")
	} else {
		fmt.Println("   ✓ go.sum")
	}

	printSuccess(name, opts)
	return nil
}

// titleWords capitalises the first letter of each word in s (split by sep)
// and rejoins them with joinSep.
// It replaces the deprecated strings.Title without any external dependencies.
// Example: titleWords("blog-api", "-", "-") → "Blog-Api"
func titleWords(s, sep, joinSep string) string {
	words := strings.Split(s, sep)
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		runes := []rune(w)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		words[i] = string(runes)
	}
	return strings.Join(words, joinSep)
}

// fileEntry pairs a relative path with its template string.
type fileEntry struct {
	path string
	tmpl string
}

// buildFileList returns all files to generate, conditionally including
// worker/cache-specific content.
func buildFileList(data TemplateData, dbc dbConfig) []fileEntry {
	files := []fileEntry{
		// Root config files
		{"go.mod", tmplGoMod},
		{".env.example", tmplEnvExample(data, dbc)},
		{".gitignore", tmplGitignore},
		{".air.toml", tmplAirToml},
		{".dockerignore", tmplDockerignore},
		{"Makefile", tmplMakefile},
		{"Dockerfile", tmplDockerfile},
		{"README.md", tmplReadme},

		// docker-compose (only for non-sqlite, or sqlite still gets one for redis if cache enabled)
		{"docker-compose.yml", tmplDockerCompose(data, dbc)},

		// Config
		{"config/config.go", tmplConfigGo},

		// DB migrations
		{"db/migrations/001_init.sql", tmplInitSQL},
		{"docs/openapi.yaml", tmplOpenAPIYaml},

		// cmd/api
		{"cmd/api/main.go", tmplMainAPIGo(data)},

		// cmd/axe (pre-rendered separately in scaffold() to avoid double-processing)
		// {"cmd/axe/main.go", tmplMainAxeGo(data)},  ← written directly via os.WriteFile

		// Pkg stubs — apperror
		{"pkg/apperror/apperror.go", tmplApperror},

		// Logger stub
		{"pkg/logger/logger.go", tmplLogger},

		// Metrics stub
		{"pkg/metrics/metrics.go", tmplMetrics},

		// Txmanager stub
		{"pkg/txmanager/txmanager.go", tmplTxmanager},

		// JWT stub
		{"pkg/jwtauth/jwtauth.go", tmplJwtauth},

		// Internal — domain shared types (Pagination, etc.)
		{"internal/domain/pagination.go", tmplDomainPagination},

		// Internal — middleware package (WriteError, WriteJSON, JWTAuth, RequireRole)
		{"internal/handler/middleware/middleware.go", tmplMiddleware},
		{"internal/handler/middleware/auth.go", tmplMiddlewareAuth},

		// Empty stubs for directories that are filled in by `axe generate resource`
		{"internal/repository/.gitkeep", ""},
		{"internal/service/.gitkeep", ""},
		{"ent/generate.go", tmplEntGenerate},
		{"ent/schema/.gitkeep", ""},

		// Leaders — setup + hook
		{"internal/setup/plugin.go", tmplSetupPlugin(data)},
		{"internal/handler/hook/hook.go", tmplHookLeader},
		{"internal/handler/router.go", tmplRouterLeader},

		// WebSocket hub — always scaffolded (used by cmd/api/main.go)
		{"pkg/ws/adapter.go", tmplWSAdapter},
		{"pkg/ws/metrics.go", tmplWSMetrics},
		{"pkg/ws/room.go", tmplWSRoom},
		{"pkg/ws/client.go", tmplWSClient},
		{"pkg/ws/hub.go", tmplWSHub},
		{"pkg/ws/auth.go", tmplWSAuth},
		{"pkg/ws/redis_adapter.go", tmplWSRedisAdapter},

		// Dev routes — Rails-like route listing on 404
		{"pkg/devroutes/devroutes.go", tmplDevRoutes},
	}

	if data.WithCache {
		files = append(files,
			fileEntry{"pkg/cache/cache.go", tmplCache},
			fileEntry{"pkg/ratelimit/ratelimit.go", tmplRatelimit},
		)
	}
	if data.WithWorker {
		files = append(files,
			fileEntry{"pkg/worker/worker.go", tmplWorker},
		)
	}
	if data.WithStorage {
		files = append(files,
			fileEntry{"pkg/storage/storage.go", TmplStorageCore},
			fileEntry{"pkg/storage/handler.go", TmplStorageHandler},
			fileEntry{"pkg/storage/metrics.go", TmplStorageMetrics},
		)
	}

	return files
}

// renderAndWrite renders a Go text/template string with data and writes it to path.
func renderAndWrite(path, tmplStr string, data TemplateData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if tmplStr == "" {
		return nil // empty file (gitkeep)
	}

	t, err := template.New(filepath.Base(path)).Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
	}).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	return t.Execute(f, data)
}

// printSuccess prints the final success message with next steps.
func printSuccess(name string, opts Options) {
	fmt.Printf(`
✅  Project %q created successfully!

Next steps:
  cd %s

`, name, name)

	if opts.DB == "sqlite" {
		fmt.Printf("  # SQLite — no Docker needed\n")
		fmt.Printf("  make migrate-up\n")
		fmt.Printf("  make run\n")
	} else {
		fmt.Printf("  make setup    # copies .env, starts Docker, runs migrations\n")
		fmt.Printf("  make run      # starts the API server with hot-reload\n")
	}

	fmt.Printf(`
  # Generate your first resource:
  axe generate resource Post --fields="title:string,body:text"

  # API will be available at:
  curl http://localhost:8080/health
`)
}
