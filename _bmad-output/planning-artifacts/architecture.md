# Architecture — axe

> *Compressed copy.* Original at `architecture.original.md`.

**Status**: Accepted | **Date**: 2026-04-16 (v2) | **Supersedes**: `docs/architecture_contract.md` + `docs/data_consistency.md`

---

## Tech Stack

| Layer | Choice | Notes |
|-------|--------|-------|
| Language | Go 1.22+, no CGO | Generics, slog, <20MB image |
| HTTP | Chi v5 | Rejected: Gin, Echo, bare net/http |
| ORM | Ent (writes) | Schema-as-code, compile-time safe |
| Query | sqlc (reads) | Type-safe Go from SQL, zero reflection |
| DB pool | Shared `*sql.DB` | Both Ent+sqlc share 1 pool |
| DB adapters | `pkg/db/adapter.go` | PostgreSQL (pgx v5), MySQL (go-sql-driver), SQLite (modernc.org, CGO-free) |
| Cache/PubSub | Redis (go-redis v9) | Cache + pubsub + rate limiter + WS adapter |
| Config | Cleanenv | Env-var only. Rejected: Viper |
| Jobs | Asynq + Outbox | Redis-backed queue, atomic DB+event |
| Observability | slog + OpenTelemetry + Prometheus | JSON logs, tracing, `/metrics` |
| DI | Manual wiring | Rejected: Wire (overhead unjustified) |
| Testing | testify + testcontainers-go + httptest | |

---

## Plugin System — 7 Strategies

**S1 — Correctness Gates (ADR-011):** 6-layer model. `Dependent` interface → `DependsOn() []string` + `validateDAG()`. Shared resource pool only (`app.DB/Cache/Hub`).

**S2 — Parallel Startup (ADR-012):** Kahn's topo-sort → wave-based concurrent `Register()`. LIFO rollback per wave. Target <3s at 50+ plugins.

**S3 — UI Extension (ADR-013):** `Contributor` (nav panel) + `Configurable` (JSON Schema settings form). `AllPlugins()` on App. `axe_admin_settings` table persists config.

**S4 — Event Bus (ADR-014):** `pkg/plugin/events/Bus`. 3 modes: Sync | Async (fire-forget + error hook) | Redis (multi-instance). Standard topics: `storage.uploaded`, `user.registered`, etc.

**S5 — Observability (ADR-015):** `obs.NewCounter()` enforces `axe_{plugin}_{metric}_{unit}`. `HealthChecker` aggregated by `/ready`. Pre-tagged logger.

**S6 — DX:** `axe plugin new/validate`. `plugintest.MockApp` (in-memory SQLite). Guide: `docs/plugin-guide.md`.

**S7 — Versioning (ADR-016):** `const AxeVersion` + `Versioned` interface (`MinAxeVersion()`). Checked before `validateDAG()`. Semver rules.

**Monorepo (ADR-009):** Official plugins in `pkg/plugin/`. Separate submodule only when dep bloat real.

---

## Folder Structure

```
axe/
├── cmd/api/main.go              # Orchestrator — calls Leaders, no logic
├── internal/
│   ├── boot/plugin.go           # 🔌 Plugin Leader
│   ├── handler/
│   │   ├── router.go            # 🗺  Router Leader
│   │   ├── hook/{hook,stripe}.go # 🎣 Hook Leader + business hooks
│   │   ├── auth_handler.go
│   │   ├── user_handler.go
│   │   └── middleware/
│   ├── domain/                  # Entities + Value Objects (zero deps)
│   ├── service/                 # Business rules
│   └── repository/              # Ent/sqlc data access
├── pkg/
│   ├── plugin/{plugin,events,payment/stripe,...}
│   ├── ws/                      # WebSocket Hub
│   ├── jwtauth/ | logger/ | metrics/ | apperror/
├── config/config.go             # Cleanenv env-var only
├── ent/schema/                  # Ent schemas
├── db/{migrations,queries}      # SQL files
└── _bmad-output/                # BMAD artifacts
```

**Read tree:** `internal/boot/` = what starts | `handler/router.go` = where on HTTP | `handler/hook/` = what happens on events | `cmd/api/main.go` = conductor only.

---

## Composition Root — Leader Pattern

`main.go` = Orchestrator. Holds infra (DB, Cache, Hub), distributes to independent Leaders. **Leaders NEVER import each other.**

```
ZERO CROSS-COUPLING: Router ↛ Plugin, Plugin ↛ Hook, Hook ↛ Router
```

Adding plugin (`axe plugin add stripe`):
- `internal/boot/plugin.go` → `app.Use(stripeExt)`
- `internal/handler/hook/hook.go` → `RegisterStripeHooks(bus)`
- `internal/handler/hook/stripe.go` → new file
- `config/config.go` → env vars
- **`cmd/api/main.go` unchanged** ✅

---

## Key Patterns (code ref)

### Transaction Manager
```go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
// Repo extracts tx: db := extractTxOrDB(ctx, r.db)
```

### Error Taxonomy
```go
var (
    ErrNotFound     = &AppError{Code: "NOT_FOUND", HTTPStatus: 404}
    ErrInvalidInput = &AppError{Code: "INVALID_INPUT", HTTPStatus: 400}
    ErrUnauthorized = &AppError{Code: "UNAUTHORIZED", HTTPStatus: 401}
    ErrForbidden    = &AppError{Code: "FORBIDDEN", HTTPStatus: 403}
    ErrInternal     = &AppError{Code: "INTERNAL_ERROR", HTTPStatus: 500}
    ErrConflict     = &AppError{Code: "CONFLICT", HTTPStatus: 409}
)
// Usage: apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
```

### Outbox
```sql
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate TEXT NOT NULL, event_type TEXT NOT NULL,
    payload JSONB NOT NULL, created_at TIMESTAMPTZ DEFAULT NOW(),
    processed_at TIMESTAMPTZ, retries INT DEFAULT 0
);
```
Poller: goroutine polls 1s → Asynq → marks processed.

### Ent Schema
```go
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

### sqlc: explicit columns, `:one/:many/:exec`, no `SELECT *`.

### Testing: Integration (testcontainers) → Service unit (mock repo) → Handler unit (httptest) → Domain unit (pure). Target ≥80%.

---

## ADR Log

| ADR | Decision |
|-----|----------|
| 001 | Chi over Gin/Echo |
| 002 | Cleanenv over Viper |
| 003 | Ent (writes) + sqlc (reads) shared pool |
| 004 | Manual wiring over Wire |
| 005 | Outbox pattern for side effects |
| 006 | Pluggable DB adapter (PG+MySQL+SQLite) |
| 007 | modernc.org/sqlite (CGO-free) |
| 008 | nhooyr.io/websocket over gorilla |
| 009 | Plugins monorepo, split when needed |
| 010 | FSStore POSIX over S3 SDK |
| 011 | 6-Layer plugin correctness gates |
| 012 | Wave-based parallel startup |
| 013 | Plugin UI Extension (`Contributor`+`Configurable`) |
| 014 | Event Bus (Sync/Async/Redis) |
| 015 | Plugin Observability (`obs.NewCounter`, `HealthChecker`) |
| 016 | Plugin Versioning (`AxeVersion`+`Versioned`) |
| 017 | Leader Pattern (3 independent Leaders, zero cross-coupling) |
