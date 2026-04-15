package new

import (
	"fmt"
	"strings"
)


// ─────────────────────────────────────────────────────────────────────────────
// Root-level config files
// ─────────────────────────────────────────────────────────────────────────────

const tmplGoMod = `module {{.Module}}

go 1.22.0

require (
	github.com/go-chi/chi/v5 v5.2.1
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/ilyakaznacheev/cleanenv v1.5.0
	github.com/prometheus/client_golang v1.20.0
	github.com/redis/go-redis/v9 v9.7.0
	github.com/spf13/cobra v1.9.1
)
`

const tmplGitignore = `# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
/bin/

# Test binary, built with ` + "`" + `go test -c` + "`" + `
*.test

# Output of the go coverage tool
*.out
coverage.html

# Dependency directories
vendor/

# Go workspace file
go.work
go.work.sum

# Environment files
.env
.env.local
.env.*.local

# IDE / Editor
.idea/
.vscode/
*.swp
*.swo
*.suo
.DS_Store
Thumbs.db

# Ent generated code (re-generated via ` + "`" + `go generate ./ent/...` + "`" + `)
ent/client.go
ent/ent.go
ent/enttest/
ent/hook/
ent/migrate/
ent/predicate/
ent/runtime.go

# sqlc generated code
internal/repository/db/

# Wire generated
wire_gen.go

# Air (hot reload)
tmp/

# Docker
.dockerignore

# Logs
*.log

# Build artifacts
dist/

# SQLite databases
*.db
*.db-shm
*.db-wal
`

const tmplAirToml = `root = "."
testdata_dir = "testdata"
tmp_dir = "tmp"

[build]
  args_bin = []
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ./cmd/api"
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "testdata", "node_modules", ".git"]
  exclude_file = []
  exclude_regex = ["_test.go"]
  exclude_unchanged = false
  follow_symlink = false
  full_bin = ""
  include_dir = []
  include_ext = ["go", "tpl", "tmpl", "html"]
  include_file = []
  kill_delay = "0s"
  log = "build-errors.log"
  poll = false
  poll_interval = 0
  post_cmd = []
  pre_cmd = []
  rerun = false
  rerun_delay = 500
  send_interrupt = false
  stop_on_error = false

[color]
  app = ""
  build = "yellow"
  main = "magenta"
  runner = "green"
  watcher = "cyan"

[log]
  main_only = false
  time = false

[misc]
  clean_on_exit = false

[screen]
  clear_on_rebuild = false
  keep_scroll = true
`

const tmplDockerignore = `# Binaries
/bin/
/tmp/
*.exe

# Go test artifacts
*.test
*.out

# IDE files
.idea/
.vscode/
*.swp

# Environment
.env
.env.*

# Git
.git/
.gitignore

# Documentation
README.md
docs/

# Local SQLite databases
*.db
*.db-shm
*.db-wal
`

const tmplDockerfile = `# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build — static binary, CGO disabled
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /app-server ./cmd/api/main.go

# ── Final stage ────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

# Security: non-root user
RUN groupadd -r app && useradd -r -g app app

WORKDIR /app

# CA certificates for TLS calls
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy binary
COPY --from=builder /app-server ./app-server

# Own files
RUN chown -R app:app /app
USER app

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["./app-server"]
`

const tmplReadme = `# {{.Name}}

A production-ready Go API built with the [axe](https://github.com/axe-cute/axe) framework.

## Quick Start

` + "```bash" + `
# 1. Copy environment config
cp .env.example .env

# 2. Start services + apply migrations
make setup

# 3. Run the API server
make run
` + "```" + `

The API will be available at http://localhost:8080.

## Available Commands

` + "```bash" + `
make run              # Start API server with hot-reload (requires air)
make build            # Build production binary
make test             # Run all unit tests
make test-integration # Run integration tests (requires Docker)
make migrate-up       # Apply pending migrations
make migrate-down     # Rollback last migration
make migrate-status   # Show migration status
make lint             # Run golangci-lint
make docker-up        # Start Docker services
make docker-down      # Stop Docker services
` + "```" + `

## Generate Resources

` + "```bash" + `
# Generate a full CRUD resource
go run ./cmd/axe generate resource Post --fields="title:string,body:text,published:bool"

# With authentication
go run ./cmd/axe generate resource Order --fields="amount:float" --with-auth
` + "```" + `

## Project Structure

` + "```" + `
.
├── cmd/
│   ├── api/main.go     ← HTTP server entry point
│   └── axe/main.go     ← CLI tooling (generate, migrate)
├── config/             ← App configuration (env-based)
├── db/
│   ├── migrations/     ← SQL migration files
│   └── queries/        ← sqlc query files
├── internal/
│   ├── domain/         ← Domain models & interfaces
│   ├── handler/        ← HTTP handlers
│   ├── repository/     ← Data access layer
│   └── service/        ← Business logic
├── pkg/                ← Reusable packages
├── ent/schema/         ← Ent ORM schemas
└── docs/               ← OpenAPI specification
` + "```" + `

## Environment Variables

See [.env.example](.env.example) for all available configuration options.

## License

MIT
`

// ─────────────────────────────────────────────────────────────────────────────
// Dynamic templates (constructed from TemplateData/dbConfig at runtime)
// ─────────────────────────────────────────────────────────────────────────────

// tmplEnvExample builds the .env.example content for the selected DB driver.
func tmplEnvExample(data TemplateData, dbc dbConfig) string {
	dbSection := fmt.Sprintf(`# Database (%s)
DATABASE_URL=%s
DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=5
DATABASE_CONN_MAX_LIFETIME_MINUTES=30
DB_DRIVER=%s`, dbc.EnvName, renderInlineTemplate(dbc.ExampleURL, data), dbc.Driver)

	redisSection := ""
	if data.WithCache || data.WithWorker {
		redisSection = `
# Redis
REDIS_URL=redis://localhost:6379/0
REDIS_MAX_RETRIES=3
`
	}

	asynqSection := ""
	if data.WithWorker {
		asynqSection = `
# Asynq (uses Redis)
ASYNQ_CONCURRENCY=10
ASYNQ_QUEUE_DEFAULT=default
ASYNQ_QUEUE_CRITICAL=critical
`
	}

	return fmt.Sprintf(`# =============================================================================
# %s — Environment Configuration
# Copy this to .env and fill in your values
# =============================================================================

# Server
SERVER_PORT=8080
ENVIRONMENT=development   # development | staging | production
LOG_LEVEL=debug           # debug | info | warn | error

%s
%s
# Auth
JWT_SECRET=your-256-bit-secret-change-in-production
JWT_ACCESS_TOKEN_EXPIRY_MINUTES=15
JWT_REFRESH_TOKEN_EXPIRY_DAYS=7
%s
# Observability (optional for local dev)
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_SERVICE_NAME=%s
`, data.Name, dbSection, redisSection, asynqSection, data.Name)
}

// tmplDockerCompose builds docker-compose.yml dynamically based on DB and feature flags.
func tmplDockerCompose(data TemplateData, dbc dbConfig) string {
	if dbc.Driver == "sqlite3" && !data.WithCache && !data.WithWorker {
		return `# SQLite project — no Docker services required for local development.
# Services can be added here as the project grows.
services: {}
`
	}

	services := ""

	// DB service
	switch dbc.Driver {
	case "postgres":
		services += fmt.Sprintf(`  postgres:
    image: %s
    container_name: %s_postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: %s
      POSTGRES_PASSWORD: %s
      POSTGRES_DB: %s_dev
    ports:
      - "5432:5432"
    volumes:
      - %s_postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U %s -d %s_dev"]
      interval: 5s
      timeout: 3s
      retries: 10
`, dbc.DockerImage, data.Name, data.Name, data.Name, data.Name, data.Name, data.Name, data.Name)

	case "mysql":
		services += fmt.Sprintf(`  mysql:
    image: %s
    container_name: %s_mysql
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_USER: %s
      MYSQL_PASSWORD: %s
      MYSQL_DATABASE: %s_dev
    ports:
      - "3306:3306"
    volumes:
      - %s_mysql_data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-u%s", "-p%s"]
      interval: 5s
      timeout: 3s
      retries: 10
`, dbc.DockerImage, data.Name, data.Name, data.Name, data.Name, data.Name, data.Name, data.Name)
	}

	// Redis service
	if data.WithCache || data.WithWorker {
		services += `
  redis:
    image: redis:7-alpine
    container_name: ` + data.Name + `_redis
    restart: unless-stopped
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
`
	}

	// Asynqmon (only if worker enabled)
	if data.WithWorker {
		services += `
  asynqmon:
    image: hibiken/asynqmon:latest
    platform: linux/amd64
    container_name: ` + data.Name + `_asynqmon
    restart: unless-stopped
    ports:
      - "8081:8080"
    environment:
      REDIS_ADDR: redis:6379
    depends_on:
      redis:
        condition: service_healthy
`
	}

	// Volumes
	volumes := ""
	switch dbc.Driver {
	case "postgres":
		volumes = fmt.Sprintf(`
volumes:
  %s_postgres_data:
`, data.Name)
	case "mysql":
		volumes = fmt.Sprintf(`
volumes:
  %s_mysql_data:
`, data.Name)
	}

	return "services:\n" + services + volumes
}

// tmplMainAPIGo builds the cmd/api/main.go composition root.
func tmplMainAPIGo(data TemplateData) string {
	imports := `	"context"
	"encoding/json"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"{{.Module}}/config"
	"{{.Module}}/pkg/logger"`

	if data.WithCache {
		imports += `
	"{{.Module}}/pkg/cache"
	"{{.Module}}/pkg/ratelimit"
	"github.com/redis/go-redis/v9"`
	}
	if data.WithWorker {
		imports += `
	"{{.Module}}/pkg/worker"`
	}
	imports += `
	"{{.Module}}/pkg/jwtauth"
	"{{.Module}}/pkg/metrics"`

	cacheInit := ""
	if data.WithCache {
		cacheInit = `
	// ── Redis cache ───────────────────────────────────────────────────────────
	cacheClient, err := cache.New(cache.Config{
		Addr:   cfg.RedisAddr(),
		Prefix: "` + data.Name + `:" + cfg.Environment + ":",
	})
	if err != nil {
		if cfg.IsProduction() {
			log.Error("redis connection failed", "error", err)
			os.Exit(1)
		}
		log.Warn("redis unavailable — cache disabled", "error", err)
		cacheClient = nil
	} else {
		defer cacheClient.Close()
		log.Info("redis connected", "addr", cfg.RedisAddr())
	}

	// ── Rate Limiter ─────────────────────────────────────────────────────────
	var redisForRL *redis.Client
	if cacheClient != nil {
		redisForRL = cacheClient.Redis()
	}
	limiter := ratelimit.New(redisForRL)
	_ = limiter
`
	}

	workerInit := ""
	if data.WithWorker {
		workerInit = `
	// ── Background Worker (Asynq) ─────────────────────────────────────────────
	workerSrv := worker.New(worker.Config{
		RedisAddr:   cfg.RedisAddr(),
		Concurrency: cfg.AsynqConcurrency,
		Queues: map[string]int{
			cfg.AsynqQueueCritical: 6,
			cfg.AsynqQueueDefault:  3,
			"low":                  1,
		},
	}, log)
`
	}

	workerStart := ""
	workerStop := ""
	if data.WithWorker {
		workerStart = `
	go func() {
		if err := workerSrv.Start(); err != nil {
			log.Warn("worker server error (may be expected if no Redis)", "error", err)
		}
	}()
`
		workerStop = `	workerSrv.Shutdown()
`
	}

	return fmt.Sprintf(`package main

import (
%s
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.LoadFromFile(".env")
	if err != nil {
		cfg, err = config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: config: %%v\n", err)
			os.Exit(1)
		}
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New(cfg.Environment)
	slog.SetDefault(log)
	log.Info("%s starting", "port", cfg.ServerPort, "env", cfg.Environment)
%s%s
	_ = log

	// ── JWT service ───────────────────────────────────────────────────────────
	jwtSvc := jwtauth.New(cfg.JWTSecret, cfg.AccessTokenTTL(), cfg.RefreshTokenTTL())
	_ = jwtSvc

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(metrics.Middleware)
	r.Use(chimiddleware.Compress(5))

	r.Get("/health", healthHandler)
	r.Handle("/metrics", metrics.Handler())

	// TODO: Mount your resource handlers here.
	// Example after running: axe generate resource Post --fields="title:string,body:text"
	//   postHandler := handler.NewPostHandler(postSvc)
	//   r.Mount("/api/v1/posts", postHandler.Routes())

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%%d", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancelBg := context.WithCancel(context.Background())
	_ = ctx
	_ = cancelBg
%s
	go func() {
		log.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	log.Info("shutdown signal received — draining...")

	cancelBg()
%s
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	log.Info("server stopped cleanly")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"status": "ok", "service": "` + data.Name + `"})
}

func readyHandler(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"status": "ok"}

		if err := sqlDB.PingContext(r.Context()); err != nil {
			resp["db"] = "error: " + err.Error()
			resp["status"] = "degraded"
		} else {
			resp["db"] = "ok"
		}
		// TODO: add cache readiness check if using Redis.

		status := http.StatusOK
		if resp["status"] == "degraded" {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, resp)
		_ = status
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}
`, imports, data.Name, cacheInit, workerInit, workerStart, workerStop)
}

// ─────────────────────────────────────────────────────────────────────────────
// Static templates (identical for all projects)
// ─────────────────────────────────────────────────────────────────────────────

const tmplMakefile = `SHELL := /bin/bash
.DEFAULT_GOAL := help

# ─── Variables ────────────────────────────────────────────────────────────────
BINARY_NAME  := api
MAIN_PATH    := ./cmd/api
BIN_DIR      := ./bin
GO           := $(shell which go 2>/dev/null || echo /usr/local/go/bin/go)

# ─── Help ─────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ─── Development ──────────────────────────────────────────────────────────────
.PHONY: run
run: ## Run the API server (with air hot-reload if available)
	@if command -v air > /dev/null 2>&1; then \
		air; \
	else \
		$(GO) run $(MAIN_PATH)/main.go; \
	fi

.PHONY: build
build: ## Build the API binary
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)/main.go
	@echo "✅ Built: $(BIN_DIR)/$(BINARY_NAME)"

# ─── Testing ──────────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run all tests
	$(GO) test ./... -timeout 60s

.PHONY: test-race
test-race: ## Run tests with race detector
	$(GO) test -race ./... -timeout 120s

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

# ─── Code Quality ─────────────────────────────────────────────────────────────
.PHONY: lint
lint: ## Run golangci-lint
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "⚠️  golangci-lint not installed. Run: brew install golangci-lint"; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format code
	$(GO) fmt ./...

# ─── Database ─────────────────────────────────────────────────────────────────
.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	$(GO) run ./cmd/axe/main.go migrate up

.PHONY: migrate-down
migrate-down: ## Rollback last migration
	$(GO) run ./cmd/axe/main.go migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GO) run ./cmd/axe/main.go migrate status

# ─── Code Generation ──────────────────────────────────────────────────────────
.PHONY: generate
generate: ## Run go generate
	$(GO) generate ./...

# ─── Docker ───────────────────────────────────────────────────────────────────
.PHONY: docker-up
docker-up: ## Start services via Docker Compose
	docker compose up -d
	@echo "✅ Services started"

.PHONY: docker-down
docker-down: ## Stop and remove Docker containers
	docker compose down

.PHONY: docker-logs
docker-logs: ## Follow Docker Compose logs
	docker compose logs -f

# ─── Setup ────────────────────────────────────────────────────────────────────
.PHONY: setup
setup: ## Full local setup from zero
	@echo "→ Copying .env.example to .env..."
	@cp -n .env.example .env || true
	@echo "→ Starting Docker services..."
	@$(MAKE) docker-up
	@echo "→ Waiting for database..."
	@sleep 3
	@echo "→ Applying migrations..."
	@$(MAKE) migrate-up
	@echo ""
	@echo "✅ Setup complete! Run: make run"

.PHONY: tidy
tidy: ## Run go mod tidy
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html tmp/
	@echo "✅ Cleaned"
`

const tmplMainAxeGo = `// {{.Name}} CLI — Developer tooling.
//
// Usage:
//
//	go run ./cmd/axe migrate up / down / status / create <name>
//	go run ./cmd/axe version
package main

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

var version = "0.1.0"

const migrationsDir = "db/migrations"

func main() {
	root := &cobra.Command{
		Use:           "axe",
		Short:         "{{.Name}} CLI — database tooling",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(versionCmd(), migrateCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("{{.Name}} cli version %s\n", version)
		},
	}
}

// ── migrate ──────────────────────────────────────────────────────────────────

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration commands",
	}
	cmd.AddCommand(migrateCreateCmd(), migrateUpCmd(), migrateDownCmd(), migrateStatusCmd())
	return cmd
}

func migrateCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new timestamped migration file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.ReplaceAll(args[0], " ", "_"))
			ts := time.Now().Format("20060102150405")
			filename := fmt.Sprintf("%s_%s.sql", ts, name)
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

func migrateUpCmd() *cobra.Command {
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

func migrateDownCmd() *cobra.Command {
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
				` + "`" + `SELECT filename FROM schema_migrations ORDER BY applied_at DESC LIMIT 1` + "`" + `,
			).Scan(&last)
			if err == pgx.ErrNoRows {
				fmt.Println("⚠️  No applied migrations to roll back.")
				return nil
			}
			if err != nil {
				return fmt.Errorf("query last migration: %w", err)
			}
			_, err = conn.Exec(ctx,
				` + "`" + `DELETE FROM schema_migrations WHERE filename = $1` + "`" + `, last,
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

func migrateStatusCmd() *cobra.Command {
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

// ── helpers ───────────────────────────────────────────────────────────────────

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

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
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
		if idx := strings.Index(v, " #"); idx != -1 {
			v = strings.TrimSpace(v[:idx])
		}
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func ensureMigrationsTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, ` + "`" + `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	` + "`" + `)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}
	return nil
}

func appliedMigrations(ctx context.Context, conn *pgx.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, ` + "`" + `SELECT filename FROM schema_migrations` + "`" + `)
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
		` + "`" + `INSERT INTO schema_migrations (filename) VALUES ($1)` + "`" + `, filename,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", filename, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filename, err)
	}
	fmt.Println("✅")
	return nil
}
`

const tmplConfigGo = `// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config holds all application configuration.
type Config struct {
	// Server
	ServerPort  int    ` + "`" + `env:"SERVER_PORT"  env-default:"8080"` + "`" + `
	Environment string ` + "`" + `env:"ENVIRONMENT"  env-default:"development"` + "`" + `
	LogLevel    string ` + "`" + `env:"LOG_LEVEL"    env-default:"info"` + "`" + `

	// Database
	DBDriver                    string ` + "`" + `env:"DB_DRIVER"                          env-default:"postgres"` + "`" + `
	DatabaseURL                 string ` + "`" + `env:"DATABASE_URL"                       env-required:"true"` + "`" + `
	DatabaseMaxOpenConns        int    ` + "`" + `env:"DATABASE_MAX_OPEN_CONNS"            env-default:"25"` + "`" + `
	DatabaseMaxIdleConns        int    ` + "`" + `env:"DATABASE_MAX_IDLE_CONNS"            env-default:"5"` + "`" + `
	DatabaseConnMaxLifetimeMins int    ` + "`" + `env:"DATABASE_CONN_MAX_LIFETIME_MINUTES" env-default:"30"` + "`" + `

	// Redis
	RedisURL        string ` + "`" + `env:"REDIS_URL"         env-default:"redis://localhost:6379/0"` + "`" + `
	RedisMaxRetries int    ` + "`" + `env:"REDIS_MAX_RETRIES" env-default:"3"` + "`" + `

	// Auth
	JWTSecret                   string ` + "`" + `env:"JWT_SECRET"                      env-required:"true"` + "`" + `
	JWTAccessTokenExpiryMinutes int    ` + "`" + `env:"JWT_ACCESS_TOKEN_EXPIRY_MINUTES" env-default:"15"` + "`" + `
	JWTRefreshTokenExpiryDays   int    ` + "`" + `env:"JWT_REFRESH_TOKEN_EXPIRY_DAYS"   env-default:"7"` + "`" + `

	// Asynq
	AsynqConcurrency   int    ` + "`" + `env:"ASYNQ_CONCURRENCY"    env-default:"10"` + "`" + `
	AsynqQueueDefault  string ` + "`" + `env:"ASYNQ_QUEUE_DEFAULT"  env-default:"default"` + "`" + `
	AsynqQueueCritical string ` + "`" + `env:"ASYNQ_QUEUE_CRITICAL" env-default:"critical"` + "`" + `

	// Observability
	OTELEndpoint    string ` + "`" + `env:"OTEL_EXPORTER_OTLP_ENDPOINT" env-default:""` + "`" + `
	OTELServiceName string ` + "`" + `env:"OTEL_SERVICE_NAME"           env-default:"app"` + "`" + `
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := cleanenv.ReadEnv(cfg); err != nil {
		return nil, fmt.Errorf("config: load from env: %w", err)
	}
	return cfg, nil
}

// LoadFromFile reads configuration from a .env file and environment variables.
func LoadFromFile(path string) (*Config, error) {
	cfg := &Config{}
	if err := cleanenv.ReadConfig(path, cfg); err != nil {
		return nil, fmt.Errorf("config: load from file %q: %w", path, err)
	}
	return cfg, nil
}

// IsProduction reports whether the current environment is production.
func (c *Config) IsProduction() bool  { return c.Environment == "production" }

// IsDevelopment reports whether the current environment is development.
func (c *Config) IsDevelopment() bool { return c.Environment == "development" }

// AccessTokenTTL returns the access token expiry as a duration.
func (c *Config) AccessTokenTTL() time.Duration {
	return time.Duration(c.JWTAccessTokenExpiryMinutes) * time.Minute
}

// RefreshTokenTTL returns the refresh token expiry as a duration.
func (c *Config) RefreshTokenTTL() time.Duration {
	return time.Duration(c.JWTRefreshTokenExpiryDays) * 24 * time.Hour
}

// RedisAddr extracts host:port from REDIS_URL.
func (c *Config) RedisAddr() string {
	u := c.RedisURL
	u = strings.TrimPrefix(u, "redis://")
	u = strings.TrimPrefix(u, "rediss://")
	if idx := strings.LastIndex(u, "/"); idx != -1 {
		u = u[:idx]
	}
	return u
}
`

const tmplInitSQL = `-- 001_init.sql
-- Initial schema migration.
-- Generated by axe new.

-- Example: users table (remove or replace with your domain model)
CREATE TABLE IF NOT EXISTS users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      VARCHAR(255) NOT NULL UNIQUE,
    name       VARCHAR(255) NOT NULL,
    role       VARCHAR(50)  NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
`

const tmplOpenAPIYaml = `openapi: "3.0.3"
info:
  title: "{{.Name}} API"
  version: "1.0.0"
  description: "REST API for {{.Name}}"

servers:
  - url: http://localhost:8080
    description: Local development

paths:
  /health:
    get:
      summary: Health check
      operationId: healthCheck
      responses:
        "200":
          description: Service is healthy
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: ok
`

// ─────────────────────────────────────────────────────────────────────────────
// Package stubs — minimal but compilable
// ─────────────────────────────────────────────────────────────────────────────

const tmplApperror = `// Package apperror defines domain-level error types for {{.Name}}.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError is a structured application error with an HTTP status code.
type AppError struct {
	Code    int
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

// Common constructors.
func NotFound(resource string) *AppError {
	return &AppError{Code: http.StatusNotFound, Message: resource + " not found"}
}

func BadRequest(msg string) *AppError {
	return &AppError{Code: http.StatusBadRequest, Message: msg}
}

func Unauthorized(msg string) *AppError {
	return &AppError{Code: http.StatusUnauthorized, Message: msg}
}

func Forbidden(msg string) *AppError {
	return &AppError{Code: http.StatusForbidden, Message: msg}
}

func Internal(err error) *AppError {
	return &AppError{Code: http.StatusInternalServerError, Message: "internal server error", Err: err}
}

// IsNotFound returns true if err is a 404 AppError.
func IsNotFound(err error) bool {
	var ae *AppError
	return errors.As(err, &ae) && ae.Code == http.StatusNotFound
}
`

const tmplLogger = `// Package logger provides a structured slog-based logger for {{.Name}}.
package logger

import (
	"log/slog"
	"os"
)

// New returns a structured *slog.Logger configured for the given environment.
// In production it uses JSON format; otherwise human-readable text.
func New(env string) *slog.Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{Level: slog.LevelDebug}

	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
`

const tmplMetrics = `// Package metrics provides Prometheus instrumentation middleware for {{.Name}}.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware records request count and latency for each HTTP handler.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := prometheus.NewTimer(httpDuration.WithLabelValues(r.Method, r.URL.Path))
		defer timer.ObserveDuration()

		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		httpRequests.WithLabelValues(r.Method, r.URL.Path, http.StatusText(rw.status)).Inc()
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
`

const tmplTxmanager = `// Package txmanager provides a database transaction manager for {{.Name}}.
package txmanager

import (
	"context"
	"database/sql"
	"fmt"
)

// Manager wraps a *sql.DB to provide explicit transaction management.
type Manager struct {
	db *sql.DB
}

// New creates a new transaction Manager.
func New(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// WithTx executes fn inside a database transaction.
// It commits on success and rolls back on any error or panic.
func (m *Manager) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx failed: %w; rollback failed: %v", err, rbErr)
		}
		return err
	}

	return tx.Commit()
}
`

const tmplJwtauth = `// Package jwtauth provides JWT creation and validation for {{.Name}}.
package jwtauth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Service handles JWT operations.
type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// Claims represents JWT claim payload.
type Claims struct {
	UserID string ` + "`" + `json:"sub"` + "`" + `
	Role   string ` + "`" + `json:"role"` + "`" + `
	jwt.RegisteredClaims
}

// New creates a new JWT Service.
func New(secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// SignAccess signs a new access token for the given user.
func (s *Service) SignAccess(userID, role string) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a JWT token string.
func (s *Service) Verify(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
`

const tmplCache = `// Package cache provides a Redis-backed cache client for {{.Name}}.
package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps a Redis client with a key prefix.
type Client struct {
	rdb    *redis.Client
	prefix string
}

// Config holds Redis connection configuration.
type Config struct {
	Addr   string // host:port
	Prefix string // key prefix, e.g. "myapp:dev:"
}

// New creates a new Redis Client and verifies connectivity.
func New(cfg Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.Addr,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb, prefix: cfg.Prefix}, nil
}

// Redis returns the underlying *redis.Client for advanced usage.
func (c *Client) Redis() *redis.Client { return c.rdb }

// Close closes the Redis connection.
func (c *Client) Close() error { return c.rdb.Close() }

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// key prepends the configured prefix to a cache key.
func (c *Client) key(k string) string { return c.prefix + k }

// Set stores a value under key with the given TTL.
func (c *Client) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.key(key), val, ttl).Err()
}

// Get retrieves a value by key. Returns redis.Nil if not found.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	v, err := c.rdb.Get(ctx, c.key(key)).Result()
	if err != nil {
		return "", err
	}
	return v, nil
}

// Del removes one or more keys.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}
	return c.rdb.Del(ctx, prefixed...).Err()
}

// Exists reports whether keys exist.
func (c *Client) Exists(ctx context.Context, keys ...string) (bool, error) {
	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}
	n, err := c.rdb.Exists(ctx, prefixed...).Result()
	return n > 0, err
}

// IsNotFound returns true if err is a Redis nil (cache miss).
func IsNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "redis: nil")
}
`

const tmplRatelimit = `// Package ratelimit provides a Redis sliding-window rate limiter for {{.Name}}.
package ratelimit

import (
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter holds rate limiting configuration.
type Limiter struct {
	rdb *redis.Client
}

// New creates a Limiter. If rdb is nil, rate limiting is disabled.
func New(rdb *redis.Client) *Limiter {
	return &Limiter{rdb: rdb}
}

// Global returns a middleware that limits to 100 requests/minute per IP.
func (l *Limiter) Global() func(http.Handler) http.Handler {
	return l.limit(100, time.Minute)
}

// Strict returns a middleware that limits to 10 requests/minute per IP.
func (l *Limiter) Strict() func(http.Handler) http.Handler {
	return l.limit(10, time.Minute)
}

func (l *Limiter) limit(_ int, _ time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO: implement Redis sliding-window rate limiting.
			// For a reference implementation see:
			// https://github.com/axe-cute/axe/blob/main/pkg/ratelimit/ratelimit.go
			next.ServeHTTP(w, r)
		})
	}
}
`

const tmplWorker = `// Package worker provides Asynq-based background job processing for {{.Name}}.
package worker

import (
	"log/slog"

	"github.com/hibiken/asynq"
)

// Config holds Asynq worker configuration.
type Config struct {
	RedisAddr   string
	Concurrency int
	Queues      map[string]int
}

// Server wraps an Asynq server.
type Server struct {
	srv *asynq.Server
	mux *asynq.ServeMux
	log *slog.Logger
}

// New creates a new Asynq worker server.
func New(cfg Config, log *slog.Logger) *Server {
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: cfg.RedisAddr}, asynq.Config{
		Concurrency: cfg.Concurrency,
		Queues:      cfg.Queues,
	})
	return &Server{srv: srv, mux: asynq.NewServeMux(), log: log}
}

// Register registers a handler for a task type.
func (s *Server) Register(taskType string, handler asynq.HandlerFunc) {
	s.mux.HandleFunc(taskType, handler)
}

// Start begins processing background tasks.
func (s *Server) Start() error {
	s.log.Info("asynq worker starting")
	return s.srv.Run(s.mux)
}

// Shutdown gracefully stops the worker.
func (s *Server) Shutdown() {
	s.srv.Shutdown()
	s.log.Info("asynq worker stopped")
}
`

// ─────────────────────────────────────────────────────────────────────────────
// Inline template helper
// ─────────────────────────────────────────────────────────────────────────────

// renderInlineTemplate renders a simple template string with TemplateData.
// Used for per-driver DSN strings inside env files.
func renderInlineTemplate(t string, data TemplateData) string {
	r := strings.NewReplacer(
		"{{.Name}}", data.Name,
		"{{.Module}}", data.Module,
	)
	return r.Replace(t)
}
