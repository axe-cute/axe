# Architecture — axe Go Web Framework

**Status**: Accepted  
**Date**: 2026-04-15  
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
- **PostgreSQL** (pgx v5 driver) — production-grade, jsonb, advisory locks
- **Redis** (go-redis v9) — cache + pubsub

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
- **Wire** — compile-time DI, no runtime reflect
- **go generate** orchestrates: Ent codegen + sqlc generate + Wire

### Testing
- **testify** — assertions + mocking
- **testcontainers-go** — real PostgreSQL in Docker for integration tests
- **httptest** — handler unit tests

---

## Folder Structure

```
axe/
├── cmd/api/main.go          # Composition Root
├── internal/
│   ├── domain/              # Entities + Interfaces
│   ├── handler/             # HTTP (Chi)
│   ├── service/             # Business logic
│   └── repository/          # DB access (Ent + sqlc)
├── pkg/
│   ├── apperror/            # Error taxonomy
│   ├── txmanager/           # Transaction manager
│   ├── logger/              # slog wrapper
│   └── validator/           # go-playground/validator
├── ent/schema/              # Ent schema files
├── db/
│   ├── migrations/          # Atlas/raw SQL migrations
│   └── queries/             # sqlc .sql files
├── config/config.go         # Cleanenv config
└── _bmad-output/            # BMAD workflow artifacts
```

---

## Layer Architecture

```
┌──────────────────────────────────────────┐
│              cmd/api/main.go             │  ← Composition Root
│    (Wire: wires everything together)     │
└────────────────┬─────────────────────────┘
                 │
┌────────────────▼─────────────────────────┐
│           internal/handler/              │  ← HTTP layer (Chi)
│   • Parse request                        │
│   • Validate format                      │
│   • Call service interface               │
│   • Write HTTP response                  │
└────────────────┬─────────────────────────┘
                 │ via interface
┌────────────────▼─────────────────────────┐
│           internal/service/              │  ← Business logic
│   • Business rules                       │
│   • Authorization                        │
│   • Transaction coordination             │
│   • Outbox event appending               │
└────────────────┬─────────────────────────┘
                 │ via interface
┌────────────────▼─────────────────────────┐
│         internal/repository/             │  ← Data access
│   • Ent (writes)                         │
│   • sqlc (complex reads)                 │
│   • Extract tx from context              │
└────────────────┬─────────────────────────┘
                 │
┌────────────────▼─────────────────────────┐
│   PostgreSQL (pgx/sql.DB shared pool)    │
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
| ADR-004 | Wire for DI (compile-time) | 2026-04-15 |
| ADR-005 | Outbox pattern for side effects | 2026-04-15 |
