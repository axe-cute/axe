# 📜 Architecture Contract

> The constitution of the axe framework.
> Every pull request, every AI-generated code, and every technical decision
> **must** comply with this document. No exceptions.

---

## Table of Contents

1. [The "No Magic" Principle](#1-the-no-magic-principle)
2. [Layer Architecture](#2-layer-architecture)
3. [Layer Rules (in detail)](#3-layer-rules-in-detail)
4. [Multi-Database Strategy](#4-multi-database-strategy)
5. [Error Handling Contract](#5-error-handling-contract)
6. [Transaction Contract](#6-transaction-contract)
7. [Plugin Contract](#7-plugin-contract)
8. [WebSocket Contract](#8-websocket-contract)
9. [Observability Contract](#9-observability-contract)
10. [PR Checklist](#10-pr-checklist)

---

## 1. The "No Magic" Principle

The single most important rule in axe: **every behavior must be traceable
at compile-time**. A developer reading the code should never wonder
"where does this come from?" or "when does this run?".

### ✅ Allowed (compile-time, inspectable, generates static code)

| Pattern | Why it's OK |
|---|---|
| Struct tags (`json:"..."`, `db:"..."`, `validate:"..."`) | Declarative, visible in source, processed by known libraries |
| `go generate` + Ent/sqlc/Wire codegen | Generates **static Go files** that you can read and debug |
| Implicit interface satisfaction | The compiler verifies the contract — no runtime surprise |
| Build constraints (`//go:build integration`) | Conditional compilation, explicit in source |
| `init()` for driver registration (`database/sql` style) | Idiomatic Go pattern, side-effect is adding to a registry |

### ❌ Forbidden (runtime, opaque, hides control flow)

| Pattern | Why it's banned |
|---|---|
| `reflect.ValueOf` / `reflect.TypeOf` in hot paths | Invisible behavior, hard to debug, poor performance |
| `init()` with complex side effects | Order-dependent, hard to test, surprising |
| Global mutable state after startup | Concurrency hazard, makes testing non-deterministic |
| Dynamic plugin loading (`plugin.Open`) | Breaks static analysis, version coupling |
| Runtime dependency injection (containers) | Control flow becomes invisible; use Wire instead |

### `panic()` Policy

`panic()` is **forbidden in request-serving code paths** (`pkg/`, `internal/handler/`, `internal/service/`, `internal/repository/`).

**Allowed only in:**
| Context | Example | Rationale |
|---|---|---|
| `Must*` functions | `MustResolve[T]()`, `MustFromCtx()` | Go convention — caller explicitly opts into panic |
| Startup-time registration guards | `db.Register()`, `plugin.Provide[T]()` | Mirrors `database/sql.Register()` — fail-fast before any request |
| CLI template parsing | `template.Must()` | Startup-only, no user request involved |
| Test helpers | `t.Fatal()` equivalent | Tests, not production |

---

## 2. Layer Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                      cmd/api/main.go                          │
│            Orchestrator (infra setup + Leader calls)           │
│  Creates: config, DB, cache, JWT, WS Hub │ then delegates:    │
└──────────────┬─────────────────┬─────────────────┬────────────┘
               │                 │                 │
    ┌──────────▼───────┐ ┌──────▼────────┐ ┌──────▼────────────┐
    │ setup/plugin.go  │ │ hook/hook.go  │ │ handler/router.go │
    │  Plugin Leader   │ │  Hook Leader  │ │  Router Leader    │
    │  RegisterPlugins │ │  RegisterAll  │ │  Controllers{}    │
    └──────────────────┘ └───────────────┘ └───────┬───────────┘
         🔌 plugins           🎣 events            │
                                        ┌──────────▼──────────────┐
                                        │    internal/handler/    │  HTTP Layer
                                        │ Parse → Validate → Call │
                                        └───────────┬─────────────┘
                                                    │ via interface
                                        ┌───────────▼─────────────┐
                                        │    internal/service/    │  Business Logic
                                        │ Rules, auth, tx, outbox │
                                        └───────────┬─────────────┘
                                                    │ via interface
                                        ┌───────────▼─────────────┐
                                        │   internal/repository/  │  Data Access
                                        │   Ent or sqlc           │
                                        └───────────┬─────────────┘
                                                    │
                                        ┌───────────▼─────────────┐
                                        │ PostgreSQL/MySQL/SQLite │
                                        └─────────────────────────┘
```

**Leaders are independent.** No Leader imports another. `main.go` is the sole
orchestrator that connects them. This prevents composition root bloat as the
plugin ecosystem grows.

**The dependency arrow always points downward.** A lower layer must never
import or reference a higher layer. Interfaces are defined in `domain/`
(or in the consuming layer) so that the dependency is inverted.

---

## 3. Layer Rules (in detail)

### 3.1 `internal/domain/` — The Core

The domain layer contains **entities, value objects, and interfaces only**.
It has zero knowledge of databases, HTTP, or any infrastructure.

**Allowed imports:**
```go
import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
    "github.com/google/uuid"  // type definition only
)
```

**Forbidden imports:** anything from `database/`, `net/http`, `log/slog`,
any ORM, any framework, any validation library.

**What goes here:**
- Entity structs (e.g. `User`, `Post`) with business methods
- Repository interfaces (e.g. `UserRepository`)
- Service interfaces (if needed for mocking)
- Domain errors (e.g. `ErrInsufficientBalance`)
- Value objects and enums

**What does NOT go here:**
- Database queries, SQL, Ent client calls
- HTTP request/response types
- Logging
- Configuration

### 3.2 `internal/handler/` — HTTP Layer

The handler layer is the **thin adapter** between HTTP and the service layer.
Each handler method follows the same 4-step pattern:

```
1. Parse    → Extract data from HTTP request (JSON body, path params, query params)
2. Validate → Check input format (required fields, types, ranges)
3. Call     → Invoke a service method via its interface
4. Write    → Return an HTTP response (status code + JSON body)
```

| ✅ Allowed | ❌ Forbidden |
|---|---|
| Parse request body/params | Direct database calls |
| Input format validation | Business logic |
| Call service interface | Direct repository calls |
| Write HTTP response | Transaction management |
| Set response headers | Outbox event creation |

### 3.3 `internal/service/` — Business Logic

The service layer contains **all business rules**. This is where the real
work happens. Services coordinate between repositories, enforce invariants,
and manage transactions.

| ✅ Allowed | ❌ Forbidden |
|---|---|
| Business rules and invariants | HTTP concerns (headers, status codes, request parsing) |
| Authorization checks | Direct database driver calls (`sql.DB.Query(...)`) |
| Transaction coordination via `TxManager` | Importing `net/http` |
| Calling repository interfaces | Returning HTTP status codes |
| Appending outbox events (within tx) | |
| Logging business-relevant information | |

### 3.4 `internal/repository/` — Data Access

The repository layer is a **pure data access adapter**. It implements the
interfaces defined in `domain/` and talks to the database.

| ✅ Allowed | ❌ Forbidden |
|---|---|
| Database read/write via Ent or sqlc | Business logic (conditions, calculations) |
| Implement domain interfaces | HTTP concerns |
| Extract transaction from context | Calling other repositories directly |
| Map DB entities to domain entities | Creating outbox events |

**Important:** If an operation requires coordination between multiple
repositories, that coordination belongs in the **service layer**, not here.

---

## 4. Multi-Database Strategy

### Ent vs sqlc — Choose One Per Project

Axe supports both Ent and sqlc, but **each project chooses one**:

| Option | Best for | Strengths |
|---|---|---|
| **Ent** (recommended) | CRUD-heavy REST APIs | Schema-as-code, compile-time safety, auto migrations, relations |
| **sqlc** | Query-heavy / analytics apps | Hand-written SQL, full control, type-safe output, complex JOINs |

> ⚠️ Do not use both Ent and sqlc in the same project.
> Choose the tool that fits your use case.

Both share the **same `*sql.DB` connection pool**, managed by
the pluggable `pkg/db` adapter.

### Pluggable DB Adapters

The `pkg/db` package provides an adapter interface. Drivers register
themselves via `init()` (same pattern as `database/sql`):

```go
import (
    _ "github.com/axe-cute/axe/pkg/db/postgres"  // registers "postgres"
    _ "github.com/axe-cute/axe/pkg/db/mysql"      // registers "mysql"
    _ "github.com/axe-cute/axe/pkg/db/sqlite"     // registers "sqlite3"
)

sqlDB, entDialect, err := db.Open(cfg.DBDriver, db.AdapterConfig{...})
```

---

## 5. Error Handling Contract

Errors flow upward through the layers, gaining context at each level:

```
Repository  →  wraps with context:  fmt.Errorf("create order: %w", err)
     ↓
Service     →  maps to app error:   apperror.ErrNotFound.WithMessage("order not found")
     ↓
Handler     →  central middleware maps *AppError → HTTP status + JSON
```

### Rules

1. **Repository** — returns `fmt.Errorf("operation: %w", err)`.
   Never returns `apperror` types directly.

2. **Service** — catches repository errors and maps them to the appropriate
   `apperror` type (`ErrNotFound`, `ErrConflict`, `ErrForbidden`, etc.)

3. **Handler** — does NOT manually map errors to HTTP status codes.
   The central error middleware (`middleware.Recoverer`) inspects
   `*apperror.AppError` and writes the correct status + JSON.

4. **Never use raw `errors.New()` at the handler level.** Always use the
   `apperror` taxonomy so that error responses are consistent.

---

## 6. Transaction Contract

### Rule 1: Multiple writes require a transaction

If a service method performs **more than one write operation**, it **must**
be wrapped in `TxManager.WithinTransaction()`:

```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        order, err := s.orderRepo.Create(ctx, ...)
        if err != nil { return err }

        // The outbox event is in the SAME transaction as the order insert.
        return s.outboxRepo.Append(ctx, outbox.Event{...})
    })
}
```

### Rule 2: Repositories accept `context.Context` and extract the tx

Repositories must **never** start their own transactions. Instead, they
extract the transaction from context (falling back to the connection pool
if no transaction is active):

```go
func (r *orderRepo) Create(ctx context.Context, order domain.Order) error {
    db := extractTxOrDB(ctx, r.db)  // tx if present, otherwise pool
    // ...
}
```

### Rule 3: Outbox events are appended within the same transaction

The outbox event and the primary DB write must be **atomic**. If either
fails, both are rolled back.

---

## 7. Plugin Contract

Plugins extend axe without modifying core code. The lifecycle is:

```
app.Use(plugin)     →  registers (before Start)
app.Start(ctx)      →  calls Register() on each plugin, FIFO order
app.Shutdown(ctx)   →  calls Shutdown() on each plugin, LIFO order
```

### Rules

1. **Every plugin implements `plugin.Plugin`** — `Name()`, `Register()`, `Shutdown()`.
2. **Register is called once** — plugins must not register routes or services outside of `Register()`.
3. **Typed service locator** — use `plugin.Provide[T]()` / `plugin.Resolve[T]()` for cross-plugin communication.
4. **Fail-fast at startup** — if a plugin fails in `Register()`, all previously registered plugins are rolled back via `Shutdown()`.
5. **Plugins must not depend on registration order** — use the service locator to resolve optional dependencies.

---

## 8. WebSocket Contract

### Connection lifecycle

```
Client connects → WSAuth middleware (JWT validation + conn limit)
    → Hub.UpgradeAuthenticated() → nhooyr.io/websocket upgrade
    → Client readPump + writePump goroutines start
    → Hub.Join(client, "room-name")
    → client.OnMessage(handler)
    → ... messages flow ...
    → client disconnects → readPump exits → tracker.Release()
```

### Rules

1. **All WS connections require authentication** — `WSAuth` middleware validates JWT (header or `?token=` query param).
2. **Per-user connection limits are enforced** — default max 5 connections per user.
3. **Messages are non-blocking** — `client.Send()` writes to a buffered channel (256); slow clients are dropped.
4. **Multi-instance broadcasting** — set `HUB_ADAPTER=redis` for cross-instance broadcast via Redis Pub/Sub.
5. **Graceful shutdown** — `Hub.Shutdown()` closes all connections and waits for goroutines to exit.

---

## 9. Observability Contract

### Logging

- Use `log/slog` (stdlib) everywhere — never `fmt.Println` or `log.Printf`.
- Structured fields: `slog.Info("msg", "key", "value")`.
- In production, logs are JSON; in development, human-readable text.
- Include `request_id` in all request-scoped logs.

### Metrics

All Prometheus metrics use the `axe_` namespace:

| Metric | Type | Description |
|---|---|---|
| `axe_http_requests_total{method,path,status}` | Counter | Total HTTP requests |
| `axe_http_request_duration_seconds{method,path}` | Histogram | Request latency |
| `axe_ws_active_connections` | Gauge | Current WebSocket connections |
| `axe_ws_messages_total{direction}` | Counter | WS messages sent/received |
| `axe_storage_upload_bytes_total` | Counter | Total bytes uploaded |
| `axe_storage_operations_total{operation,status}` | Counter | Storage operations |

### Health endpoints

- `GET /health` — liveness probe (always returns 200 if the process is running)
- `GET /ready` — readiness probe (checks DB + Redis connectivity)
- `GET /metrics` — Prometheus scrape endpoint

---

## 10. PR Checklist

Every pull request to `main` must pass the following checks:

```
□ go vet ./...              — no issues
□ golangci-lint run ./...   — no warnings
□ go test ./...             — all green
□ go test -race ./...       — no data races
□ No forbidden imports in internal/domain/
□ apperror taxonomy used — no raw errors at handler level
□ Transactions wrapped if multiple writes in one service call
□ ADR updated if an architectural decision was changed
□ Test coverage did not decrease compared to the previous commit
□ New public APIs are documented with GoDoc comments
```

---

*Last updated: 2026-04-16*
