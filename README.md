# 🪓 axe

> Go web framework — Clean Architecture, zero runtime magic, production-grade from day one.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## Philosophy

- **No runtime magic** — every behavior is traceable at compile-time
- **Clean Architecture baked-in** — layer violations caught by the compiler
- **Production-grade from day one** — transactions, structured logging, error taxonomy, rate limiting
- **DX-first** — new developer ships a feature on day one
- **Plugin-friendly** — extend without touching core (storage, email, payments…)

---

## Features

| Category | What you get |
|---|---|
| **HTTP** | Chi v5 router, structured middleware (logging, recovery, request ID, CORS) |
| **ORM + Query** | Ent (writes) + sqlc (complex reads), shared `*sql.DB` pool |
| **Multi-DB** | PostgreSQL (pgx v5), MySQL, SQLite — pluggable adapter interface |
| **Auth** | JWT access/refresh tokens, token blocklist via Redis, role-based middleware |
| **Cache** | Redis client with key-prefix isolation |
| **Rate Limiting** | Redis sliding-window limiter (global + strict per-route) |
| **Background Jobs** | Asynq (Redis-backed queues, Asynqmon dashboard at `:8081`) |
| **Outbox Pattern** | Atomic DB write + event dispatch, background poller → Asynq |
| **WebSocket** | Hub/Client/Room abstraction, Redis Pub/Sub adapter for multi-instance, JWT auth, per-user connection limits |
| **File Storage** | FSStore plugin — local dev & JuiceFS production (POSIX, zero SDK deps), POST/GET/DELETE `/upload` endpoints |
| **Plugin System** | `plugin.Plugin` interface, typed service locator, FIFO register / LIFO shutdown |
| **Observability** | Prometheus metrics (`/metrics`), structured slog logging (JSON in prod), OpenTelemetry-ready |
| **Code Generator** | `axe generate resource` — 10 files across all layers in one command |
| **Project Scaffold** | `axe new` — full project from scratch, `< 5 min` to first API call |
| **API Docs** | OpenAPI 3.0 spec, Swagger UI (`/docs`), Redoc (`/docs/redoc`) |
| **CI** | GitHub Actions: multi-DB matrix (Postgres + MySQL + SQLite), lint, race, coverage |

---

## Quick Start (< 2 minutes)

**Prerequisites**: Go 1.22+, Docker

```bash
# Clone
git clone https://github.com/axe-cute/axe && cd axe

# One-command setup (copies .env, starts Postgres + Redis, migrates, seeds)
make setup

# Run (hot-reload with air if installed)
make run
# → http://localhost:8080
```

Check health:
```bash
curl http://localhost:8080/health
# {"status":"ok","service":"axe"}
```

### Scaffold a New Project

```bash
# Build the CLI
go build -o bin/axe ./cmd/axe

# Create a new project (postgres, with worker + cache)
./bin/axe new blog-api --module=github.com/acme/blog-api

# Or lightweight (sqlite, no worker, no cache)
./bin/axe new lite --db=sqlite --no-worker --no-cache --yes

cd blog-api && make setup && make run
```

---

## Development Commands

```bash
# ── Run ──────────────────────────────────────────────
make run                     # API server (air hot-reload if available)
make build                   # Build binary to ./bin/axe

# ── Test ─────────────────────────────────────────────
make test                    # All unit tests (< 30s)
make test-race               # With race detector
make test-coverage           # HTML coverage report
make test-integration        # PostgreSQL integration (Docker)
make test-integration-mysql  # MySQL integration (Docker)
make test-integration-sqlite # SQLite integration (no Docker)
make test-ws                 # WebSocket hub unit tests
make test-ws-integration     # WebSocket Redis integration (Docker)
make test-plugin             # Plugin + storage unit tests

# ── Quality ──────────────────────────────────────────
make lint                    # golangci-lint
make vet                     # go vet
make fmt                     # gofmt + goimports

# ── Codegen ──────────────────────────────────────────
make generate                # All generators (Ent + sqlc + Wire)
make generate-ent            # Ent ORM only
make generate-sqlc           # sqlc only

# ── Database ─────────────────────────────────────────
make migrate-up              # Apply pending migrations
make migrate-down            # Rollback last migration
make migrate-status          # Show migration status
make seed                    # Load test/seed data

# ── Docker ───────────────────────────────────────────
make docker-up               # Start PostgreSQL + Redis + Asynqmon
make docker-down             # Stop services
make docker-logs             # Follow compose logs

# ── Misc ─────────────────────────────────────────────
make setup                   # Full local setup from zero
make tidy                    # go mod tidy
make clean                   # Remove build artifacts
```

---

## Project Structure

```
axe/
├── cmd/
│   ├── api/main.go                    # Composition Root
│   └── axe/                           # CLI tool
│       ├── main.go                    #   axe new / generate / migrate
│       ├── new/                       #   Project scaffolding
│       ├── generate/                  #   Resource code generator
│       └── migrate/                   #   DB migration runner
├── internal/
│   ├── domain/                        # Entities + Interfaces ONLY (no infra imports)
│   ├── handler/                       # HTTP layer (Chi)
│   │   └── middleware/                # JWT auth, RBAC, logger, recovery, request ID
│   ├── service/                       # Business logic, transactions, outbox
│   └── repository/                    # Data access (Ent writes, sqlc reads)
├── pkg/
│   ├── apperror/                      # Error taxonomy (NotFound, Forbidden, Conflict…)
│   ├── cache/                         # Redis cache client
│   ├── db/                            # Pluggable DB adapter interface
│   │   ├── postgres/                  #   PostgreSQL (pgx v5)
│   │   ├── mysql/                     #   MySQL 8+
│   │   └── sqlite/                    #   SQLite (pure Go, no CGO)
│   ├── jwtauth/                       # JWT issue / verify / refresh
│   ├── logger/                        # Structured slog wrapper
│   ├── metrics/                       # Prometheus middleware + /metrics handler
│   ├── outbox/                        # Outbox event poller → Asynq
│   ├── plugin/                        # Plugin system (interface + typed service locator)
│   │   └── storage/                   # File storage plugin (FSStore)
│   ├── ratelimit/                     # Redis sliding-window rate limiter
│   ├── txmanager/                     # Transaction manager (context-injected tx)
│   ├── validator/                     # Input validation
│   ├── worker/                        # Asynq background worker
│   └── ws/                            # WebSocket hub, client, room, auth
│       ├── hub.go                     #   Hub with Room-based broadcasting
│       ├── client.go                  #   Per-connection client
│       ├── auth.go                    #   WSAuth middleware (JWT header + query)
│       ├── adapter.go                 #   Adapter interface (Memory / Redis)
│       ├── redis_adapter.go           #   Redis Pub/Sub for multi-instance
│       └── metrics.go                 #   Prometheus counters
├── ent/schema/                        # Ent ORM schema definitions
├── db/
│   ├── migrations/                    # SQL migrations
│   └── queries/                       # sqlc SQL queries
├── config/config.go                   # Cleanenv configuration (env vars)
├── docs/
│   ├── openapi.yaml                   # OpenAPI 3.0 spec
│   ├── architecture_contract.md       # Layer rules
│   ├── data_consistency.md            # Transaction + outbox patterns
│   └── adr/                           # Architecture Decision Records
├── .github/workflows/                 # CI: multi-DB matrix, lint, race
├── docker-compose.yml                 # PostgreSQL + Redis + Asynqmon
├── Makefile                           # All development commands
└── .env.example                       # Environment variable reference
```

---

## Architecture

See [`docs/architecture_contract.md`](docs/architecture_contract.md) for the full contract.

```
┌───────────────────────────────────────────────┐
│               cmd/api/main.go                 │  ← Composition Root
│  (wires: config, DB, cache, JWT, WS, plugins) │
└────────────────────┬──────────────────────────┘
                     │
┌────────────────────▼──────────────────────────┐
│             internal/handler/                 │  ← HTTP layer (Chi)
│   • Parse request, validate, call service     │
│   • Middleware: JWT, RBAC, rate limit, metrics│
└────────────────────┬──────────────────────────┘
                     │ via interface
┌────────────────────▼──────────────────────────┐
│             internal/service/                 │  ← Business logic
│   • Rules, authorization, TxManager, outbox   │
└────────────────────┬──────────────────────────┘
                     │ via interface
┌────────────────────▼──────────────────────────┐
│           internal/repository/                │  ← Data access
│   • Ent (writes), sqlc (complex reads)        │
└────────────────────┬──────────────────────────┘
                     │
┌────────────────────▼──────────────────────────┐
│      PostgreSQL / MySQL / SQLite              │
│          (pluggable via pkg/db)               │
└───────────────────────────────────────────────┘
```

Key rules:
- `internal/domain/` — only stdlib imports, no infrastructure
- `internal/handler/` — parse request → validate → call service → write response
- `internal/service/` — business rules, transactions, outbox events
- `internal/repository/` — DB access only (Ent writes, sqlc reads)

---

## axe CLI

### `axe new` — Project Scaffolding

```bash
axe new blog-api                                           # Defaults: Postgres + worker + cache
axe new shop --db=mysql --module=github.com/acme/shop      # MySQL
axe new lite --db=sqlite --no-worker --no-cache --yes      # Minimal, no Docker needed
```

### `axe generate resource` — Code Generator

```bash
# Full CRUD (10 files: domain, handler, service, repo, schema, migration, queries, tests)
axe generate resource Post \
  --fields="title:string,body:text,published:bool,views:int"

# With relationship
axe generate resource Comment \
  --fields="body:text,score:int" \
  --belongs-to=Post

# With JWT authentication
axe generate resource Order \
  --fields="amount:float,status:string" \
  --with-auth

# Admin-only (implies --with-auth)
axe generate resource Config \
  --fields="key:string,value:text" \
  --admin-only

# With WebSocket room (scaffolds pkg/ws if missing)
axe generate resource Chat \
  --fields="message:text" \
  --with-ws
```

**Supported field types**: `string`, `text`, `int`, `float`, `bool`, `uuid`, `time`

### `axe migrate` — Migration Runner

```bash
axe migrate up       # Apply all pending migrations
axe migrate down     # Rollback last migration
axe migrate status   # Show current state
```

---

## WebSocket

Real-time support with Hub/Client/Room pattern. Supports single-instance (memory) and multi-instance (Redis Pub/Sub) deployments.

```bash
# Connect (JWT via query param for browser clients)
websocat "ws://localhost:8080/ws?token=<jwt>"

# Or via header
websocat -H="Authorization: Bearer <jwt>" ws://localhost:8080/ws
```

Configuration:
```env
HUB_ADAPTER=memory   # or "redis" for multi-instance
```

---

## File Storage Plugin

Zero-dependency file storage via POSIX filesystem. Works identically on local dev directories and JuiceFS mount points (no SDK needed).

```bash
# Upload
curl -X POST http://localhost:8080/upload -F "file=@photo.png"
# → {"key":"2026/04/16/uuid.png","url":"/upload/2026/04/16/uuid.png","size":12345,"content_type":"image/png"}

# Download
curl http://localhost:8080/upload/2026/04/16/uuid.png -o photo.png

# Delete
curl -X DELETE http://localhost:8080/upload/2026/04/16/uuid.png
# → 204 No Content
```

Configuration:
```env
STORAGE_BACKEND=local               # "local" (dev) or "juicefs" (production)
STORAGE_MOUNT_PATH=./uploads        # or /mnt/jfs/uploads
STORAGE_MAX_FILE_SIZE=10485760      # 10MB
STORAGE_URL_PREFIX=/upload
```

Prometheus metrics:
- `axe_storage_upload_bytes_total` — total bytes uploaded
- `axe_storage_operations_total{operation,status}` — ops by type and result
- `axe_storage_upload_errors_total{reason}` — errors by cause

---

## Plugin System

Extend axe without modifying core code:

```go
// Create a plugin
type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "my-plugin" }
func (p *MyPlugin) Register(ctx context.Context, app *plugin.App) error {
    // Access: app.Router, app.DB, app.Cache, app.Hub, app.Logger
    plugin.Provide[MyService](app, "my-service", svc)
    return nil
}
func (p *MyPlugin) Shutdown(ctx context.Context) error { return nil }

// Register in main.go
app.Use(&MyPlugin{})
app.Start(ctx)

// Other plugins resolve dependencies:
svc := plugin.MustResolve[MyService](app, "my-service")
```

Lifecycle: FIFO registration, LIFO shutdown, automatic rollback on failure.

---

## Observability

| Endpoint | Description |
|---|---|
| `GET /health` | Liveness probe |
| `GET /ready` | Readiness probe (DB + Redis) |
| `GET /metrics` | Prometheus scrape endpoint |
| `GET /docs` | Swagger UI |
| `GET /docs/redoc` | Redoc |
| `GET /openapi.yaml` | OpenAPI 3.0 spec |

---

## Environment Configuration

See [`.env.example`](.env.example) for the full reference. Key variables:

```env
# Server
SERVER_PORT=8080
ENVIRONMENT=development               # development | staging | production

# Database (pluggable: postgres | mysql | sqlite3)
DB_DRIVER=postgres
DATABASE_URL=postgres://axe:axe@localhost:5432/axe_dev?sslmode=disable

# Redis (cache + rate limiter + worker + WS pub/sub)
REDIS_URL=redis://localhost:6379/0

# Auth
JWT_SECRET=your-256-bit-secret

# WebSocket
HUB_ADAPTER=memory                     # memory | redis

# Storage
STORAGE_BACKEND=local                  # local | juicefs
STORAGE_MOUNT_PATH=./uploads
```

---

## Reference Implementation

The `User` domain is the **canonical reference** for all other domains:
- [`internal/domain/user.go`](internal/domain/user.go)
- [`internal/handler/user_handler.go`](internal/handler/user_handler.go)
- [`internal/service/user_service.go`](internal/service/user_service.go)
- [`internal/repository/user_repo.go`](internal/repository/user_repo.go)

When in doubt → read User domain.

---

## Onboarding (1 day)

**Morning** (4h):
1. Read [`docs/architecture_contract.md`](docs/architecture_contract.md) → 30 min
2. `make setup && make run` → 5 min
3. Read User domain code end-to-end → 90 min
4. Run and read User tests → 30 min

**Afternoon** (4h):
1. `axe generate resource YourDomain --fields="..." --with-auth`
2. Customize generated code
3. Write 1 business rule in service layer
4. Submit PR

---

## Docker Services

```bash
make docker-up    # Starts:
```

| Service | Port | Purpose |
|---|---|---|
| PostgreSQL 16 | `5432` | Primary database |
| Redis 7 | `6379` | Cache, rate limiter, worker queues, WS pub/sub |
| Asynqmon | `8081` | Background job dashboard |

---

## License

MIT
