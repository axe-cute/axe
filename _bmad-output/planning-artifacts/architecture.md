# Architecture — axe Go Web Framework

**Status**: Accepted  
**Date**: 2026-04-16 (v2 — Phase 2 updates)  
**Supersedes**: `docs/architecture_contract.md` + `docs/data_consistency.md`

---

## Tech Stack Decisions

### Language & Runtime
- **Go 1.22+** — generics support, stdlib slog, performance
- **No CGO** — pure Go build, < 20MB Docker image

### HTTP
- **Chi v5** — lightweight, idiomatic, interface-based middleware
- *Rejected*: Gin (too magical), Echo (similar reasons), net/http bare (too verbose for team scale)

### ORM & Query
- **Ent** (writes) — schema-as-code, compile-time safe, Atlas migration
- **sqlc** (complex reads) — generates type-safe Go from SQL, zero reflection
- **Shared `*sql.DB`**: cả hai dùng chung 1 connection pool
- *Rejected*: GORM (runtime reflection, magic), sqlx (manual scanning)

### Database
- **Pluggable adapter interface** (`pkg/db/adapter.go`)
- **PostgreSQL** (pgx v5) — production default, jsonb, advisory locks
- **MySQL** (go-sql-driver/mysql) — legacy system support
- **SQLite** (modernc.org/sqlite, pure Go, CGO-free) — dev/test, no Docker needed
- **Redis** (go-redis v9) — cache + pubsub + rate limiter + WS adapter

### Config
- **Cleanenv** — cloud-native, env-var only, struct binding, validation
- *Rejected*: Viper (overkill, file-based config conflicts với 12-Factor)

### Background Jobs
- **Asynq** — Redis-backed, reliable queue, Asynqmon UI
- **Outbox pattern** — DB write + event atomic, poller publishes to Asynq

### Observability
- **slog** (stdlib) — structured JSON logging, context-aware
- **OpenTelemetry** — distributed tracing
- **Prometheus** — metrics endpoint `/metrics`

### DI & Codegen
- **Manual wiring** trong `cmd/api/main.go` — explicit, traceable, no codegen overhead
- **go generate** orchestrates: Ent codegen + sqlc generate
- *Rejected*: Wire (overhead chưa justified ở scale hiện tại, manual wiring đủ explicit)

### Plugin System — 7 Ecosystem Strategies

#### Strategy 1 — Correctness Gates (ADR-011)
- **6-Layer quality model**: Interface contract → Duplicate detection → Dependency declaration + Cycle detection (Kahn's) → Fail-fast config → Typed service keys → Shared resource pool
- **`Dependent` interface**: plugins khai báo `DependsOn() []string`, `validateDAG()` kiểm tra trước bất kỳ `Register()` nào
- **Shared resource pool**: plugins Chỉ dùng `app.DB`, `app.Cache`, `app.Hub` — cấm tự tạo connections (100 plugins × 10 conns = DB crash)

#### Strategy 2 — Performance at Scale (ADR-012)
- **Wave-based parallel startup**: Kahn's output groups plugins by depth, each wave runs concurrently via goroutines. Rollback LIFO per wave. Target: < 3s at 50+ plugins.

#### Strategy 3 — UI Extension Model (ADR-013)
- **`Contributor` interface**: any plugin can contribute a nav panel to admin UI without importing admin package
- **`Configurable` interface**: extends Contributor with auto-rendered settings form (JSON Schema) + `ErrInvalidConfig` typed error
- **`AllPlugins() []Plugin`** method on `App`: needed for admin to iterate plugins without service locator hack
- **`axe_admin_settings` table**: persists nav visibility + config per plugin (uses shared `app.DB`)
- **Plugin Manager page**: users toggle which plugins appear in sidebar, edit config at runtime

#### Strategy 4 — Event Bus (ADR-014)
- **`pkg/plugin/events/Bus`**: plugins communicate via topic events — no direct import needed
- **3 delivery modes**: Sync (same goroutine) | Async (goroutine, fire-and-forget with error hook) | Redis (multi-instance fan-out)
- **Standard topics**: `storage.uploaded`, `user.registered`, `job.failed`, etc.
- **`app.Events`** field exposed to all plugins in `Register()`

#### Strategy 5 — Observability Contract (ADR-015)
- **Metrics helpers**: `obs.NewCounter(pluginName, metric, unit)` enforces `axe_{plugin}_{metric}_{unit}` naming
- **`HealthChecker` interface**: optional, `GET /ready` aggregates all plugin health checks
- **Pre-tagged logger**: `app.Logger.With("plugin", p.Name())` — convention documented in authoring guide

#### Strategy 6 — Developer Experience
- **`axe plugin new {name}`**: scaffolds plugin with plugin.go, config.go, metrics.go, tests
- **`axe plugin validate`**: checks 6 quality layers + version compatibility locally
- **`plugintest.MockApp`**: `plugintest.NewMockApp()` for plugin unit tests (in-memory SQLite, no Docker)
- **Plugin Authoring Guide**: `docs/plugin-guide.md` documents all strategies with examples

#### Strategy 7 — Versioning & Compatibility (ADR-016)
- **`const AxeVersion`**: bumped every release, checked at `App.Start()` before `validateDAG()`
- **`Versioned` interface**: `MinAxeVersion() string` — optional, plugins declare minimum axe version
- **Semver rules**: MAJOR break = incompatible; MINOR/PATCH = backward compatible
- **`axe plugin validate`**: also checks version compatibility before push

#### Monorepo Rule (ADR-009)
- All official plugins in `pkg/plugin/`. Heavy SDK plugins (AI, Kafka, Stripe) → separate Go submodule when dependency bloat is real, not speculative.

### Testing
- **testify** — assertions + mocking
- **testcontainers-go** — real PostgreSQL in Docker for integration tests
- **httptest** — handler unit tests

---

## Folder Structure

```
axe/
├── cmd/
│   └── api/
│       └── main.go              # Orchestrator — gọi Leaders, không chứa logic
│
├── internal/
│   ├── boot/
│   │   └── plugin.go            # 🔌 Plugin Leader — Load & Wire tất cả Plugins
│   ├── handler/
│   │   ├── router.go            # 🗺  Router Leader — Định nghĩa tất cả REST routes
│   │   ├── hook/
│   │   │   ├── hook.go          # 🎣 Hook Leader — Subscribe tất cả Domain Events
│   │   │   └── stripe.go        # Business logic cho Stripe webhooks
│   │   ├── auth_handler.go
│   │   ├── user_handler.go
│   │   └── middleware/
│   ├── domain/                  # Entities + Value Objects (zero deps)
│   ├── service/                 # Business rules, use-cases
│   └── repository/              # Ent (writes) + sqlc (reads)
│
├── pkg/
│   ├── plugin/                  # Plugin engine + official plugins
│   │   ├── plugin.go            # App, Plugin interface, lifecycle
│   │   ├── events/              # Event Bus (pub/sub)
│   │   └── payment/stripe/      # Stripe plugin implementation
│   ├── ws/                      # WebSocket Hub
│   ├── jwtauth/
│   ├── logger/
│   ├── metrics/
│   └── apperror/
│
├── config/config.go             # Cleanenv — env-var only
├── ent/schema/                  # Ent schema files
├── db/
│   ├── migrations/
│   └── queries/                 # sqlc .sql files
└── _bmad-output/                # BMAD workflow artifacts
```

> 💡 **Quy tắc đọc cây thư mục:**
> - `internal/boot/`  → **Cái gì** được khởi động
> - `internal/handler/router.go` → **Ở đâu** trên HTTP
> - `internal/handler/hook/` → **Khi nào** có event thì làm gì
> - `cmd/api/main.go` → Chỉ là **Nhạc Trưởng**, không có logic thực tế

---

## Composition Root & Leader Pattern

`main.go` là **Nhạc Trưởng (Orchestrator)** — nó nắm giữ Infrastructure (DB, Cache, Hub) và phân phối chúng cho các **Leader** độc lập. Leaders **không được phép** import lẫn nhau.

```
┌─────────────────────────────────────────────────────────────────────┐
│                       cmd/api/main.go                               │
│                   「 Orchestrator — Nhạc Trưởng 」                    │
│                                                                     │
│   cfg, sqlDB, entClient, cache, wsHub  ← Infrastructure Setup      │
│                                                                     │
│   controllers := build Controllers (DI)                             │
│                         │                                           │
│         ┌───────────────┼───────────────────────┐                  │
│         ▼               ▼                       ▼                  │
│  ┌─────────────┐ ┌──────────────┐  ┌─────────────────────────┐    │
│  │ 🗺  Router  │ │ 🔌 Plugin   │  │      🎣 Hook             │    │
│  │   Leader   │ │   Leader    │  │       Leader             │    │
│  │            │ │             │  │                          │    │
│  │ router.go  │ │  boot/      │  │  handler/hook/hook.go    │    │
│  │            │ │  plugin.go  │  │                          │    │
│  │ Nhận:      │ │             │  │  Nhận: events.Bus        │    │
│  │ chi.Router │ │ Nhận:       │  │                          │    │
│  │ Controllers│ │ plugin.App  │  │  Biết về:                │    │
│  │            │ │ config      │  │  TOPIC NAMES only        │    │
│  │ Biết về:   │ │             │  │  (string constants)      │    │
│  │ Routes     │ │ Biết về:    │  │                          │    │
│  │ only       │ │ Plugin init │  │  KHÔNG biết:             │    │
│  │            │ │ only        │  │  Plugins, Routes         │    │
│  │ KHÔNG biết:│ │             │  │                          │    │
│  │ Plugins    │ │ KHÔNG biết: │  │ ┌──────────────────────┐ │    │
│  │ Hooks      │ │ Routes      │  │ │ hook/stripe.go       │ │    │
│  └─────────────┘ │ Hooks      │  │ │ hook/storage.go      │ │    │
│                  └──────────────┘  │ └──────────────────────┘ │    │
│                                    └─────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘

ZERO CROSS-COUPLING: Router ↛ Plugin, Plugin ↛ Hook, Hook ↛ Router
```

### Thêm Plugin mới → main.go KHÔNG thay đổi

Khi chạy `axe plugin add stripe`:

| File được sửa | Nội dung được inject |
|---|---|
| `internal/boot/plugin.go` | Init + `app.Use(stripeExt)` |
| `internal/handler/hook/hook.go` | `RegisterStripeHooks(bus)` |
| `internal/handler/hook/stripe.go` | File mới — business logic hooks |
| `config/config.go` | `StripeSecretKey`, `StripeWebhookSecret` |
| `.env` | Env var templates |
| `cmd/api/main.go` | **Không thay đổi** ✅ |

---

## Layer Architecture

```
┌──────────────────────────────────────────┐
│              cmd/api/main.go             │  ← Orchestrator (Nhạc trưởng)
│  Calls: Router Leader, Plugin Leader,    │
│         Hook Leader — no logic here      │
└───────┬────────────────┬─────────────────┘
        │                │
        ▼                ▼
┌───────────────┐  ┌─────────────────────────────────┐
│ Router Leader │  │         Plugin Leader            │
│ router.go     │  │         boot/plugin.go           │
│               │  │                                  │
│  chi.Router + │  │  event: ───► Hook Leader         │
│  Controllers  │  │              hook/hook.go        │
└──────┬────────┘  └──────┬──────────────────────────┘
       │                  │
       ▼                  ▼
┌──────────────────────────────────────────┐
│           internal/handler/              │  ← HTTP layer (Chi)
│   • Parse request                        │
│   • Validate format                      │
│   • Call service interface               │
│   • Write HTTP response                  │
└──────────────────┬───────────────────────┘
                   │ via interface
┌──────────────────▼───────────────────────┐
│           internal/service/              │  ← Business logic
│   • Business rules                       │
│   • Authorization                        │
│   • Transaction coordination             │
└──────────────────┬───────────────────────┘
                   │ via interface
┌──────────────────▼───────────────────────┐
│         internal/repository/             │  ← Data access
│   • Ent (writes)                         │
│   • sqlc (complex reads)                 │
└──────────────────┬───────────────────────┘
                   │
┌──────────────────▼───────────────────────┐
│  DB (pluggable: PostgreSQL|MySQL|SQLite)  │
└──────────────────────────────────────────┘
```

---

## Transaction Manager Design

```go
// pkg/txmanager/txmanager.go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type pgxTxManager struct{ db *sql.DB }

func (tm *pgxTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
    tx, err := tm.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }
    ctx = injectTx(ctx, tx)
    if err := fn(ctx); err != nil {
        _ = tx.Rollback()
        return err
    }
    return tx.Commit()
}

// Repository extracts:
func (r *postgresUserRepo) Create(ctx context.Context, user domain.User) error {
    db := extractTxOrDB(ctx, r.db) // tx nếu có, fallback pool
    // ...
}
```

---

## Error Taxonomy Design

```go
// pkg/apperror/apperror.go
type AppError struct {
    Code    string
    Message string
    Cause   error
    HTTPStatus int
}

var (
    ErrNotFound     = &AppError{Code: "NOT_FOUND", HTTPStatus: 404}
    ErrInvalidInput = &AppError{Code: "INVALID_INPUT", HTTPStatus: 400}
    ErrUnauthorized = &AppError{Code: "UNAUTHORIZED", HTTPStatus: 401}
    ErrForbidden    = &AppError{Code: "FORBIDDEN", HTTPStatus: 403}
    ErrInternal     = &AppError{Code: "INTERNAL_ERROR", HTTPStatus: 500}
    ErrConflict     = &AppError{Code: "CONFLICT", HTTPStatus: 409}
)

func (e *AppError) WithMessage(msg string) *AppError {...}
func (e *AppError) WithCause(err error) *AppError {...}
```

Central error handler middleware maps `*AppError` → JSON response.

---

## Outbox Pattern Design

```sql
-- db/migrations/001_create_outbox.sql
CREATE TABLE outbox_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate    TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    retries      INT         NOT NULL DEFAULT 0
);
CREATE INDEX idx_outbox_unprocessed ON outbox_events(created_at)
    WHERE processed_at IS NULL;
```

Poller: background goroutine polls mỗi 1s, publishes to Asynq, marks processed.

---

## Ent Schema Convention

```go
// ent/schema/user.go
type User struct{ ent.Schema }

func (User) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
        field.String("email").Unique().NotEmpty(),
        field.String("name").NotEmpty(),
        field.Time("created_at").Default(time.Now).Immutable(),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}
```

---

## sqlc Convention

```sql
-- db/queries/user.sql

-- name: ListUsersByCreatedAt :many
SELECT id, email, name, created_at FROM users
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;
```

- Không `SELECT *` trong production queries
- Explicit columns luôn
- Named queries với `:one`, `:many`, `:exec`

---

## Testing Strategy

```
Layer 3: Integration
  → testcontainers-go + real PostgreSQL
  → Test: full flow từ HTTP → DB

Layer 2: Service unit
  → mock Repository via interface (testify/mock)
  → Test: happy path, business rules, error cases

Layer 1: Handler unit
  → httptest.NewRecorder() + httptest.NewRequest()
  → mock Service via interface
  → Test: 200, 400, 401, 404, 409

Layer 0: Domain unit
  → pure functions, zero dependency
  → Test: validation logic, entity methods
```

Coverage target: ≥ 80% cho handler + service.

---

## ADR Log

| ADR | Decision | Date |
|---|---|---|
| ADR-001 | Chi over Gin/Echo | 2026-04-15 |
| ADR-002 | Cleanenv over Viper | 2026-04-15 |
| ADR-003 | Ent (writes) + sqlc (reads) shared pool | 2026-04-15 |
| ADR-004 | Manual wiring over Wire (explicit DI, no codegen overhead) | 2026-04-16 |
| ADR-005 | Outbox pattern for side effects | 2026-04-15 |
| ADR-006 | Pluggable DB adapter interface (PostgreSQL + MySQL + SQLite) | 2026-04-15 |
| ADR-007 | modernc.org/sqlite over mattn/go-sqlite3 (CGO-free, aligns with No-CGO rule) | 2026-04-15 |
| ADR-008 | nhooyr.io/websocket over gorilla/websocket (actively maintained, lighter) | 2026-04-16 |
| ADR-009 | Plugins in monorepo (pkg/plugin/*) — ship fast, split later when needed | 2026-04-16 |
| ADR-010 | FSStore POSIX over S3 SDK — zero external deps, works with JuiceFS | 2026-04-16 |
| ADR-011 | 6-Layer plugin consistency model (correctness gates): interface contract, duplicate detection, `Dependent`+Kahn's cycle detection, fail-fast config in `New()`, typed service keys, shared resource pool | 2026-04-17 |
| ADR-012 | Wave-based parallel startup: Kahn's topological sort groups plugins by depth, each wave concurrently via goroutines, LIFO rollback per wave, target < 3s at 50+ plugins | 2026-04-17 |
| ADR-013 | Plugin UI Extension Model: `Contributor` interface (nav panel) + `Configurable` (settings form, JSON Schema, `ErrInvalidConfig`) + `AllPlugins() []Plugin` on App + `axe_admin_settings` table | 2026-04-17 |
| ADR-014 | Plugin Event Bus (`pkg/plugin/events`): decoupled pub/sub, 3 delivery modes (Sync/Async/Redis), standard topic constants, `OnError` hook for async error handling | 2026-04-17 |
| ADR-015 | Plugin Observability Contract: `obs.NewCounter()` enforces `axe_{plugin}_{metric}_{unit}` naming, `HealthChecker` aggregated by `/ready`, pre-tagged logger convention | 2026-04-17 |
| ADR-016 | Plugin Versioning: `const AxeVersion` + `Versioned` interface (`MinAxeVersion() string`), checked before `validateDAG()`, semver MAJOR=breaking change | 2026-04-17 |
| ADR-017 | Leader Pattern: `main.go` là pure Orchestrator, 3 Leader files độc lập zero cross-coupling: `internal/boot/plugin.go` (Plugin Leader), `internal/handler/router.go` (Router Leader), `internal/handler/hook/hook.go` (Hook Leader). CLI `axe plugin add` inject vào Leaders thay vì `main.go`. | 2026-04-18 |

