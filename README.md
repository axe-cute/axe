<div align="center">

# 🪓 axe

### Ship production Go APIs in minutes, not months.

Clean Architecture · Zero Runtime Magic · Multi-DB · Real-time WebSocket · File Storage

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-22c55e?style=for-the-badge)](LICENSE)
[![Release](https://img.shields.io/badge/v1.0.0--rc.1-stable-6366f1?style=for-the-badge)](https://github.com/axe-cute/axe/releases)

</div>

---

<br/>

## ⚡ 60 Seconds to Your First API

```bash
go install github.com/axe-cute/axe/cmd/axe@latest

axe new blog-api
cd blog-api && make setup && make run

# Generate a full CRUD resource (10 files across all layers)
axe generate resource Post --fields="title:string,body:text,published:bool"
```

```bash
curl http://localhost:8080/api/v1/posts/        # List
curl -X POST http://localhost:8080/api/v1/posts/ -d '{"title":"Hello"}' -H "Content-Type: application/json"
```

> **That's it.** Domain entity, HTTP handler, service, repository, Ent schema, migration, sqlc queries, and tests — all generated and wired.

<br/>

---

## 🏗️ What You Get

<table>
<tr><td>

**🔀 HTTP & Routing**
- Chi v5 radix-tree router
- Structured middleware stack
- Faster than Gin & Echo ([benchmarks](#-performance))

</td><td>

**🔐 Auth & Security**
- JWT access/refresh tokens
- Redis token blocklist
- Role-based middleware

</td><td>

**🗄️ Multi-Database**
- PostgreSQL (pgx v5)
- MySQL 8+
- SQLite (no CGO)

</td></tr>
<tr><td>

**⚡ Real-time**
- WebSocket Hub/Client/Room
- Redis Pub/Sub for scaling
- JWT auth on WS connections

</td><td>

**📦 File Storage**
- POSIX filesystem (zero SDK)
- Local dev & JuiceFS prod
- Upload/serve/delete API

</td><td>

**🔧 Background Jobs**
- Asynq worker (Redis queues)
- Outbox pattern (atomic events)
- Dashboard at `:8081`

</td></tr>
<tr><td>

**📊 Observability**
- Prometheus `/metrics`
- Structured slog (JSON in prod)
- OpenTelemetry-ready

</td><td>

**🧩 Plugin System**
- `plugin.Plugin` interface
- Typed service locator
- FIFO register / LIFO shutdown

</td><td>

**🛡️ Production Defaults**
- Rate limiting (Redis sliding window)
- Error taxonomy (typed errors)
- Transaction manager

</td></tr>
</table>

<br/>

---

## 🪓 The CLI

### `axe new` — Project Scaffolding

```bash
axe new blog-api                                # Postgres + worker + cache (default)
axe new shop --db=mysql                         # MySQL backend
axe new lite --db=sqlite --no-worker --no-cache # Minimal, zero Docker
axe new media --with-storage                    # Includes file upload endpoints
axe new                                         # Interactive wizard
```

### `axe generate resource` — Code Generator

One command → 10 files across all Clean Architecture layers:

```bash
axe generate resource Post    --fields="title:string,body:text,published:bool"
axe generate resource Comment --fields="body:text" --belongs-to=Post
axe generate resource Order   --fields="amount:float,status:string" --with-auth
axe generate resource Setting --fields="key:string,value:text" --admin-only
axe generate resource Chat    --fields="message:text" --with-ws
```

**Field types**: `string` · `text` · `int` · `float` · `bool` · `uuid` · `time`

> ⚠️ Names like `Config`, `Client`, `Query`, `Tx` are reserved by Ent.
> axe will catch this and suggest alternatives like `AppConfig` or `Setting`.

### `axe plugin add` — Extend Existing Projects

```bash
axe plugin add storage    # Injects pkg/storage/, config, routes, env vars
```

Auto-wires everything — no manual setup required.

### `axe migrate` — Database Migrations

```bash
axe migrate up       # Apply pending
axe migrate down     # Rollback last
axe migrate status   # Current state
```

<br/>

---

## 📐 Architecture

Clean Architecture enforced by Go's import system — **the compiler catches layer violations**.

```
                    cmd/api/main.go                         ← Composition Root
                         │
              ┌──────────▼──────────┐
              │  internal/handler/  │                       ← HTTP (Chi)
              │  middleware: JWT,   │                         Parse → Validate → Call Service
              │  RBAC, rate limit   │
              └──────────┬──────────┘
                         │ via interface
              ┌──────────▼──────────┐
              │  internal/service/  │                       ← Business Logic
              │  rules, tx, outbox  │                         Authorization, Transactions
              └──────────┬──────────┘
                         │ via interface
              ┌──────────▼──────────┐
              │ internal/repository │                       ← Data Access
              │ Ent (writes)        │                         Ent ORM + sqlc reads
              │ sqlc (complex reads)│
              └──────────┬──────────┘
                         │
              ┌──────────▼──────────┐
              │  PostgreSQL / MySQL │                       ← Pluggable via pkg/db
              │  / SQLite           │
              └─────────────────────┘
```

**Rules**: `domain/` has zero infra imports · `handler/` never touches DB · `service/` owns business rules · `repository/` owns data access

<br/>

---

## 📦 File Storage

POSIX filesystem storage — zero SDK dependencies. Works on local directories and JuiceFS mounts.

```bash
# Enable at creation or add later
axe new myapp --with-storage
axe plugin add storage

# Upload
curl -X POST http://localhost:8080/upload -F "file=@photo.png"
# → {"key":"2026/04/16/uuid.png","url":"/upload/...","size":12345,"content_type":"image/png"}

# Download / Delete
curl http://localhost:8080/upload/2026/04/16/uuid.png -o photo.png
curl -X DELETE http://localhost:8080/upload/2026/04/16/uuid.png  # → 204
```

<details>
<summary>Configuration & Metrics</summary>

```env
STORAGE_BACKEND=local          # local | juicefs
STORAGE_MOUNT_PATH=./uploads
STORAGE_MAX_FILE_SIZE=10485760 # 10MB
STORAGE_URL_PREFIX=/upload
```

Prometheus metrics: `axe_storage_upload_bytes_total` · `axe_storage_operations_total{op,status}` · `axe_storage_upload_errors_total{reason}`

</details>

<br/>

---

## 🔌 WebSocket

Hub/Client/Room pattern with JWT auth. Single-instance (memory) or multi-instance (Redis Pub/Sub).

```bash
websocat "ws://localhost:8080/ws/chats/?token=<jwt>"
```

```bash
# Generate a resource with WebSocket room
axe generate resource Chat --fields="message:text" --with-ws
```

<details>
<summary>Configuration</summary>

```env
HUB_ADAPTER=memory   # memory | redis
```

</details>

<br/>

---

## 🚀 Performance

Benchmarked against the most popular Go frameworks · Apple M1 · Go 1.25 · 5 runs median

| Scenario | 🪓 axe (Chi) | Gin | Echo | Fiber |
|---|:---:|:---:|:---:|:---:|
| **Static JSON** | **583 ns** 🏆 | 704 ns | 792 ns | 4,158 ns |
| **URL Params** | **731 ns** 🏆 | 763 ns | 760 ns | 4,381 ns |
| **Middleware Stack** | **1,014 ns** 🏆 | 1,961 ns | 1,980 ns | 7,458 ns |
| **JSON Body Parse** | 2,909 ns | 2,914 ns | 2,883 ns | 10,992 ns |
| **50-Route Match** | 1,443 ns | 747 ns | 626 ns | 4,269 ns |

> **axe wins 3/5 scenarios** outright. Middleware stack is **2× faster** than Gin and Echo.
> JSON parsing is a tie (dominated by `encoding/json`, not the router).
> Multi-route matching is ≤1µs difference — negligible vs real DB/network latency.
>
> [→ Full benchmark source & raw data](benchmarks/)

<br/>

---

## 🧩 Plugin System

Extend axe without modifying core code:

```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "my-plugin" }
func (p *MyPlugin) Register(ctx context.Context, app *plugin.App) error {
    plugin.Provide[MyService](app, "my-service", svc)
    return nil
}
func (p *MyPlugin) Shutdown(ctx context.Context) error { return nil }

// Usage
app.Use(&MyPlugin{})
svc := plugin.MustResolve[MyService](app, "my-service")
```

Lifecycle: FIFO registration · LIFO shutdown · automatic rollback on failure.

<br/>

---

## 🛠️ Developer Experience

### Rails-like Route Listing

In development, hitting a non-existent route shows a **categorized route page**:

```
Routing Error — No route matches [GET] "/api/v1/nope"

── API ─────────────────────────
GET     /api/v1/posts/
POST    /api/v1/posts/
GET     /api/v1/posts/{id}

── WebSocket ───────────────────
GET     /ws/chats/

── System ──────────────────────
GET     /health
GET     /metrics
```

### Endpoints Out of the Box

| Endpoint | Purpose |
|---|---|
| `/health` | Liveness probe |
| `/ready` | Readiness (DB + Redis) |
| `/metrics` | Prometheus scrape |
| `/debug/routes` | Full route table |
| `/upload` | File storage (when enabled) |

<br/>

---

## 📖 Quick Reference

<details>
<summary><strong>Make Commands</strong></summary>

```bash
make run                     # Hot-reload dev server (build errors shown inline)
make build                   # Binary → ./bin/axe
make test                    # Unit tests (< 30s)
make test-race               # Race detector
make test-scaffold           # Generator integration tests (verify axe generate compiles)
make test-scaffold-fast      # Fast version (full-workflow only)
make test-integration        # Postgres (Docker)
make test-integration-mysql  # MySQL (Docker)
make test-integration-sqlite # SQLite
make lint                    # golangci-lint
make generate                # Ent + sqlc codegen
make migrate-up              # Apply migrations
make docker-up               # Postgres + Redis + Asynqmon
make setup                   # Full zero-to-running
```

</details>

<details>
<summary><strong>Project Structure</strong></summary>

```
axe/
├── cmd/api/main.go              # Composition Root
├── cmd/axe/                     # CLI: new / generate / migrate / plugin
├── internal/
│   ├── domain/                  # Entities + Interfaces (stdlib only)
│   ├── handler/                 # HTTP layer + middleware
│   ├── service/                 # Business logic + transactions
│   └── repository/              # Data access (Ent + sqlc)
├── pkg/
│   ├── cache/                   # Redis client
│   ├── db/{postgres,mysql,sqlite}  # DB adapters
│   ├── jwtauth/                 # JWT tokens
│   ├── metrics/                 # Prometheus
│   ├── outbox/                  # Event outbox → Asynq
│   ├── plugin/                  # Plugin system + typed service locator
│   ├── ratelimit/               # Rate limiter
│   ├── storage/                 # File storage (local + JuiceFS)
│   ├── worker/                  # Background jobs
│   ├── ws/                      # WebSocket hub
│   └── devroutes/               # Dev route listing
├── ent/schema/                  # ORM schemas
├── db/{schema.sql,queries}/     # sqlc schema + queries
├── config/config.go             # Env-based config
├── tests/scaffold/              # Generator integration tests (axe generate / plugin add)
└── benchmarks/                  # Framework benchmarks
```

</details>

<details>
<summary><strong>Environment Variables</strong></summary>

```env
SERVER_PORT=8080
ENVIRONMENT=development           # development | staging | production
DB_DRIVER=postgres                # postgres | mysql | sqlite3
DATABASE_URL=postgres://axe:axe@localhost:5432/axe_dev?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=your-256-bit-secret
HUB_ADAPTER=memory                # memory | redis
STORAGE_BACKEND=local             # local | juicefs
STORAGE_MOUNT_PATH=./uploads
```

See [`.env.example`](.env.example) for the full reference.

</details>

<details>
<summary><strong>Docker Services</strong></summary>

| Service | Port | Purpose |
|---|---|---|
| PostgreSQL 16 | `5432` | Primary database |
| Redis 7 | `6379` | Cache, rate limiter, queues, WS pub/sub |
| Asynqmon | `8081` | Background job dashboard |

```bash
make docker-up    # Start all
make docker-down  # Stop all
```

</details>

<br/>

---

## 🎯 Example Projects

Full production APIs built with `axe new` + `axe generate resource` + custom business logic:

| Project | Domain | Business Logic |
|---|---|---|
| [🛒 E-Commerce](examples/ecommerce/) | Product, Order, Review | PlaceOrder (stock validation + inventory deduction), order status machine, rating validation |
| [📖 Webtoon](examples/webtoon/) | Series, Episode, Bookmark | Genre whitelist, view tracking, bookmark toggle (one-click add/remove) |

```bash
cd examples/ecommerce && docker-compose up -d && make run
```

See also: [standalone package examples](examples/) (apperror, txmanager, plugin).

---

## 📚 Documentation

| Guide | Description |
|---|---|
| [Incremental Adoption](docs/guides/incremental-adoption.md) | Use axe packages in your **existing** Go project — no full migration needed |
| [JuiceFS Storage](docs/guides/juicefs-storage.md) | Production storage with JuiceFS FUSE mount |
| [Architecture Contract](docs/architecture_contract.md) | Layer rules, import boundaries, error taxonomy |
| [CHANGELOG](CHANGELOG.md) | Version history, breaking changes, upgrade notes |

---

## 🤝 Contributing

```bash
git clone https://github.com/axe-cute/axe && cd axe && make setup && make run
```

See [`docs/architecture_contract.md`](docs/architecture_contract.md) for layer rules.

---

<div align="center">

**MIT License** · Built with ❤️ and Go

</div>
