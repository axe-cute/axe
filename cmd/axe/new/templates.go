package new

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"
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
	"{{.Module}}/pkg/logger"
	"{{.Module}}/pkg/ws"`

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

	// ── WebSocket Hub ─────────────────────────────────────────────────────────
	// Shared across all WebSocket handlers. Start the event loop before the server.
	wsHub := ws.NewHub(ws.WithLogger(log))
	wsTracker := ws.NewUserConnTracker()
	_ = wsTracker

	// ── REST router (chi with full middleware stack) ───────────────────────────
	// NOTE: chimiddleware.Compress wraps http.ResponseWriter and strips
	// http.Hijacker, which nhooyr.io/websocket requires for the WS upgrade.
	// Keep Compress ONLY on the REST router; never add it to the WS router.
	restRouter := chi.NewRouter()
	restRouter.Use(chimiddleware.Recoverer)
	restRouter.Use(chimiddleware.RequestID)
	restRouter.Use(chimiddleware.Logger)
	restRouter.Use(metrics.Middleware)
	restRouter.Use(chimiddleware.Compress(5))

	restRouter.Get("/health", healthHandler)
	restRouter.Handle("/metrics", metrics.Handler())

	// TODO: Mount your REST handlers here after running: axe generate resource <Name>
	// Example:
	//   postSvc  := service.NewPostService(postRepo)
	//   postHandler := handler.NewPostHandler(postSvc)
	//   restRouter.Mount("/api/v1/posts", postHandler.Routes())

	// ── WebSocket router (bare chi — NO response-wrapping middleware) ──────────
	// Wrapping middleware (Logger, Compress, Recoverer) all break http.Hijacker.
	// Only add non-wrapping middleware here (e.g. ws.WSAuth).
	wsRouter := chi.NewRouter()
	wsRouter.Use(chimiddleware.RequestID) // safe: does not wrap ResponseWriter

	// TODO: Mount your WebSocket handlers here after running: axe generate resource <Name> --with-ws
	// Example:
	//   postWSHandler := handler.NewPostWSHandler(wsHub, wsTracker)
	//   wsRouter.Mount("/ws/posts", postWSHandler.Routes())

	// ── Top-level mux: routes /ws/* to wsRouter, everything else to restRouter ─
	mux := http.NewServeMux()
	mux.Handle("/ws/", wsRouter)
	mux.Handle("/", restRouter)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%%d", cfg.ServerPort),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,  // MUST be 0 to support WebSocket connections
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	bgCtx, cancelBg := context.WithCancel(context.Background())

	// Start Hub event loop
	go wsHub.Run(bgCtx)
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
	wsHub.Shutdown()
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


//go:embed tmpl/cmd_axe_main.go.tmpl
var cmdAxeMainTmpl string

// tmplMainAxeGo renders the cmd/axe/main.go for the generated project using
// the embedded template file (tmpl/cmd_axe_main.go.tmpl).
// Custom delimiters [[ ]] are used so inner {{ }} Go template markers in the
// const resource template strings are NOT processed by the outer engine.
func tmplMainAxeGo(data TemplateData) string {
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"title": func(s string) string {
			if s == "" { return s }
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"snake": func(s string) string {
			var b strings.Builder
			for i, r := range s {
				if r >= 'A' && r <= 'Z' && i > 0 { b.WriteByte('_') }
				if r >= 'A' && r <= 'Z' { b.WriteByte(byte(r + 32)) } else { b.WriteRune(r) }
			}
			return b.String()
		},
		"bt": func() string { return "`" },
	}
	t := template.Must(template.New("cmd_axe_main").
		Funcs(funcMap).
		Delims("[[", "]]").
		Parse(cmdAxeMainTmpl))
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		panic("tmplMainAxeGo: " + err.Error())
	}
	return buf.String()
}


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

const tmplApperror = `// Package apperror defines domain-level error types and sentinel errors.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError is a structured application error with an HTTP status code.
type AppError struct {
	HTTPStatus int
	Code       string
	Message    string
	Cause      error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Cause }

// WithMessage returns a copy of the error with a new message.
func (e *AppError) WithMessage(msg string) *AppError {
	clone := *e
	clone.Message = msg
	return &clone
}

// WithCause returns a copy of the error with a wrapped cause.
func (e *AppError) WithCause(err error) *AppError {
	clone := *e
	clone.Cause = err
	return &clone
}

// Sentinel errors — use these in handlers and services.
var (
	ErrNotFound     = &AppError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: "resource not found"}
	ErrInvalidInput = &AppError{HTTPStatus: http.StatusBadRequest, Code: "INVALID_INPUT", Message: "invalid input"}
	ErrUnauthorized = &AppError{HTTPStatus: http.StatusUnauthorized, Code: "UNAUTHORIZED", Message: "unauthorized"}
	ErrForbidden    = &AppError{HTTPStatus: http.StatusForbidden, Code: "FORBIDDEN", Message: "forbidden"}
	ErrConflict     = &AppError{HTTPStatus: http.StatusConflict, Code: "CONFLICT", Message: "conflict"}
	ErrInternal     = &AppError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "internal server error"}
)

// IsNotFound reports whether err is a 404 AppError.
func IsNotFound(err error) bool {
	var ae *AppError
	return errors.As(err, &ae) && ae.HTTPStatus == http.StatusNotFound
}
`

const tmplLogger = `// Package logger provides a context-aware structured slog logger.
package logger

import (
	"context"
	"log/slog"
	"os"
)

type contextKey string

const (
	loggerKey    contextKey = "logger"
	requestIDKey contextKey = "request_id"
)

// New returns a *slog.Logger for the given environment (production=JSON, else text).
func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var h slog.Handler
	if env == "production" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// WithLogger stores a logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromCtx retrieves the logger from ctx, falling back to slog.Default().
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithRequestID stores a request ID and adds it to the logger in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	l := FromCtx(ctx).With("request_id", id)
	ctx = context.WithValue(ctx, requestIDKey, id)
	return WithLogger(ctx, l)
}

// WithFields returns a ctx with additional slog attributes attached to the logger.
func WithFields(ctx context.Context, args ...any) context.Context {
	return WithLogger(ctx, FromCtx(ctx).With(args...))
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

const tmplJwtauth = `// Package jwtauth provides JWT token generation and validation.
package jwtauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims extends jwt.RegisteredClaims with application-specific fields.
type Claims struct {
	UserID string ` + "`" + `json:"uid"` + "`" + `
	Role   string ` + "`" + `json:"role"` + "`" + `
	jwt.RegisteredClaims
}

// JTI returns the JWT ID claim (used as blocklist key for revocation).
func (c *Claims) JTI() string { return c.RegisteredClaims.ID }

// RemainingTTL returns how long until the token expires.
func (c *Claims) RemainingTTL() time.Duration {
	if c.ExpiresAt == nil { return 0 }
	if ttl := time.Until(c.ExpiresAt.Time); ttl > 0 { return ttl }
	return 0
}

// Service handles token generation and validation.
type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

// New creates a new JWT Service.
func New(secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "axe",
	}
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string ` + "`" + `json:"access_token"` + "`" + `
	RefreshToken string ` + "`" + `json:"refresh_token"` + "`" + `
	ExpiresIn    int64  ` + "`" + `json:"expires_in"` + "`" + `
}

// GenerateTokenPair mints a fresh access + refresh token pair.
func (s *Service) GenerateTokenPair(userID uuid.UUID, role string) (*TokenPair, error) {
	now := time.Now()
	accessClaims := Claims{
		UserID: userID.String(), Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID: uuid.New().String(), Issuer: s.issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil { return nil, fmt.Errorf("jwtauth: sign access: %w", err) }

	refreshClaims := Claims{
		UserID: userID.String(), Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID: uuid.New().String(), Issuer: s.issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.secret)
	if err != nil { return nil, fmt.Errorf("jwtauth: sign refresh: %w", err) }

	return &TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresIn: int64(s.accessTTL.Seconds())}, nil
}

// Validate parses and validates a token string, returning its Claims.
func (s *Service) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) { return nil, ErrTokenExpired }
		return nil, ErrTokenInvalid
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid { return nil, ErrTokenInvalid }
	return claims, nil
}

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
	ErrTokenRevoked = errors.New("token revoked")
)
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

// ─────────────────────────────────────────────────────────────────────────────
// internal/domain/pagination.go
// ─────────────────────────────────────────────────────────────────────────────

const tmplDomainPagination = `package domain

import "errors"

// Pagination holds offset-based pagination parameters.
type Pagination struct {
	Limit  int
	Offset int
}

// DefaultPagination returns sensible defaults (20 items, offset 0).
func DefaultPagination() Pagination { return Pagination{Limit: 20, Offset: 0} }

// Validate checks that pagination values are within allowed bounds.
func (p Pagination) Validate() error {
	if p.Limit < 1 || p.Limit > 100 {
		return errors.New("limit must be between 1 and 100")
	}
	if p.Offset < 0 {
		return errors.New("offset must be non-negative")
	}
	return nil
}
`

// ─────────────────────────────────────────────────────────────────────────────
// internal/handler/middleware/middleware.go
// ─────────────────────────────────────────────────────────────────────────────

const tmplMiddleware = `// Package middleware provides HTTP middleware (WriteError, WriteJSON, Logger).
package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"{{.Module}}/pkg/apperror"
	"{{.Module}}/pkg/logger"
	"github.com/google/uuid"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(logger.WithRequestID(r.Context(), id)))
	})
}

type rw struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrap(w http.ResponseWriter) *rw { return &rw{ResponseWriter: w} }
func (r *rw) Status() int {
	if r.status == 0 {
		return 200
	}
	return r.status
}
func (r *rw) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := wrap(w)
		defer func() {
			logger.FromCtx(r.Context()).Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.Status()),
				slog.Duration("latency", time.Since(start)),
			)
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.FromCtx(r.Context()).Error("panic",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				writeError(w, apperror.ErrInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type errorResponse struct {
	Code    string ` + "`" + `json:"code"` + "`" + `
	Message string ` + "`" + `json:"message"` + "`" + `
}

func WriteError(w http.ResponseWriter, err error) {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		writeError(w, appErr)
		return
	}
	writeError(w, apperror.ErrInternal)
}

func writeError(w http.ResponseWriter, appErr *apperror.AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.HTTPStatus)
	_ = json.NewEncoder(w).Encode(errorResponse{Code: appErr.Code, Message: appErr.Message})
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
`

// ─────────────────────────────────────────────────────────────────────────────
// internal/handler/middleware/auth.go
// ─────────────────────────────────────────────────────────────────────────────

const tmplMiddlewareAuth = `package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"{{.Module}}/pkg/apperror"
	"{{.Module}}/pkg/jwtauth"
	"{{.Module}}/pkg/logger"
)

type Blocklist interface {
	BlockToken(ctx context.Context, jti string, ttl time.Duration) error
	IsTokenBlocked(ctx context.Context, jti string) (bool, error)
}

type contextKey string

const claimsKey contextKey = "jwt_claims"

func ClaimsFromCtx(ctx context.Context) *jwtauth.Claims {
	v, _ := ctx.Value(claimsKey).(*jwtauth.Claims)
	return v
}

func JWTAuth(svc *jwtauth.Service, blocklist Blocklist) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromCtx(r.Context())
			token := extractBearerToken(r)
			if token == "" {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("missing authorization header"))
				return
			}
			claims, err := svc.Validate(token)
			if err != nil {
				if err == jwtauth.ErrTokenExpired {
					log.Info("token expired", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token expired"))
				} else {
					log.Warn("invalid token", "ip", r.RemoteAddr)
					WriteError(w, apperror.ErrUnauthorized.WithMessage("invalid token"))
				}
				return
			}
			if blocklist != nil && claims.JTI() != "" {
				if blocked, err := blocklist.IsTokenBlocked(r.Context(), claims.JTI()); err == nil && blocked {
					WriteError(w, apperror.ErrUnauthorized.WithMessage("token revoked"))
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
		})
	}
}

func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromCtx(r.Context())
			if claims == nil {
				WriteError(w, apperror.ErrUnauthorized.WithMessage("authentication required"))
				return
			}
			if !hasRole(claims.Role, role) {
				WriteError(w, apperror.ErrForbidden.WithMessage("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type LoginResponse struct {
	*jwtauth.TokenPair
	UserID string ` + "`" + `json:"user_id"` + "`" + `
	Role   string ` + "`" + `json:"role"` + "`" + `
}

func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func hasRole(userRole, required string) bool {
	if required == "admin" {
		return userRole == "admin"
	}
	return userRole == "user" || userRole == "admin"
}
`
