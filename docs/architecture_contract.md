# Architecture Contract — axe

> Constitution of axe framework. Every PR, AI-generated code, and technical decision MUST comply. No exceptions.

> *Compressed copy.* Original at `architecture_contract.original.md`.

---

## 1. "No Magic" Principle

Every behavior traceable at compile-time. Developer never wonders "where does this come from?"

**✅ Allowed** (compile-time, inspectable):

| Pattern | Why OK |
|---|---|
| Struct tags (`json`, `db`, `validate`) | Declarative, visible, processed by known libs |
| `go generate` + Ent/sqlc codegen | Generates static Go files |
| Implicit interface satisfaction | Compiler verifies contract |
| Build constraints (`//go:build`) | Explicit conditional compilation |
| `init()` for driver registration | Idiomatic Go, `database/sql` pattern |

**❌ Forbidden** (runtime, opaque):

| Pattern | Why banned |
|---|---|
| `reflect.ValueOf/TypeOf` in hot paths | Invisible behavior, poor perf |
| Complex `init()` side effects | Order-dependent, surprising |
| Global mutable state post-startup | Concurrency hazard |
| `plugin.Open` dynamic loading | Breaks static analysis |
| Runtime DI containers | Invisible control flow |

**`panic()` Policy:** Forbidden in request-serving code. Allowed only in: `Must*` functions, startup registration guards, CLI `template.Must()`, test helpers.

---

## 2. Layer Architecture

```
cmd/api/main.go (Orchestrator — infra setup + Leader calls)
    ├── setup/plugin.go   (Plugin Leader)
    ├── hook/hook.go      (Hook Leader)
    └── handler/router.go (Router Leader)
            ↓
    internal/handler/  (HTTP: Parse → Validate → Call → Write)
            ↓ via interface
    internal/service/  (Business logic, auth, tx, outbox)
            ↓ via interface
    internal/repository/ (Data access: Ent or sqlc)
            ↓
    DB (PostgreSQL | MySQL | SQLite)
```

**Leaders independent.** No Leader imports another. `main.go` sole orchestrator. Dependency arrow always downward. Interfaces in `domain/` for DI.

---

## 3. Layer Rules

**domain/** — Entities, value objects, interfaces only. Zero infra knowledge. Allowed: context, errors, fmt, strings, time, uuid. Forbidden: database, net/http, slog, ORM, framework, validation libs.

**handler/** — Thin HTTP adapter. 4 steps: Parse → Validate → Call service → Write response. No DB calls, no business logic, no direct repo calls.

**service/** — All business rules. Auth checks, tx coordination via `TxManager`, outbox events in tx. No HTTP concerns, no direct DB driver calls.

**repository/** — Pure data access adapter. Implements domain interfaces. No business logic, no HTTP, no cross-repo calls. Multi-repo coordination → service layer.

---

## 4. Multi-Database Strategy

**Ent vs sqlc — choose one per project.** Ent: CRUD-heavy, schema-as-code. sqlc: query-heavy, analytics. Both share `*sql.DB` via `pkg/db` adapter.

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

```
Repository  →  wraps: fmt.Errorf("operation: %w", err)
Service     →  maps to: apperror.ErrNotFound.WithMessage("...")
Handler     →  central middleware maps *AppError → HTTP status + JSON
```

Rules: Repo returns `fmt.Errorf` (never apperror directly). Service catches + maps to apperror type. Handler does NOT manually map errors. Never raw `errors.New()` at handler level.

---

## 6. Transaction Contract

**Rule 1:** >1 write → wrap in `TxManager.WithinTransaction()`.
**Rule 2:** Repos accept `context.Context`, extract tx (never start own).
**Rule 3:** Outbox events in same tx as primary write — atomic.

---

## 7. Plugin Contract

```
app.Use(plugin)   → registers (before Start)
app.Start(ctx)    → Register() each plugin, FIFO
app.Shutdown(ctx) → Shutdown() each plugin, LIFO
```

Rules: Every plugin implements `plugin.Plugin`. Register called once. Typed service locator (`Provide[T]/Resolve[T]`). Fail-fast at startup (rollback via Shutdown). Plugins must not depend on registration order.

---

## 8. WebSocket Contract

```
Connect → WSAuth (JWT) → Hub.UpgradeAuthenticated() → readPump + writePump
    → Hub.Join(client, "room") → messages flow → disconnect → tracker.Release()
```

Rules: All WS require auth. Max 5 conns/user. Non-blocking send (buffered 256). Multi-instance via `HUB_ADAPTER=redis`. Graceful shutdown waits for goroutines.

---

## 9. Observability Contract

**Logging:** slog everywhere, structured fields, JSON in prod, `request_id` in all request-scoped logs. Never `fmt.Println` or `log.Printf`.

**Metrics:** `axe_` namespace. Key metrics: `axe_http_requests_total`, `axe_http_request_duration_seconds`, `axe_ws_active_connections`, `axe_storage_*`.

**Health:** `GET /health` (liveness), `GET /ready` (readiness: DB+Redis), `GET /metrics` (Prometheus).

---

## 10. PR Checklist

```
□ go vet ./...    □ golangci-lint ./...    □ go test ./...    □ go test -race
□ No forbidden imports in domain/    □ apperror taxonomy used
□ Tx wrapped if multiple writes      □ ADR updated if arch decision changed
□ Coverage not decreased             □ New public APIs have GoDoc comments
```
