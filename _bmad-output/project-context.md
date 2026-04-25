# Project Context — axe

> Injected into every AI agent session. All generated code MUST comply. No overwrite/skip.

> *Compressed copy.* Original at `project-context.original.md`.

---

## Overview

**axe** — Go web framework (internal platform). Clean Architecture baked-in, zero runtime magic. CLI generator (`axe generate resource`) → CRUD endpoint <10 min. Production-grade day one: transactions, observability, error handling.

**Status**: Phase 2 (Plugin Ecosystem) — Sprint 34+

---

## Tech Stack

| Layer | Tech | Version |
|---|---|---|
| Language | Go | 1.25+ |
| HTTP | Chi | v5 |
| ORM | Ent | latest |
| Query (alt) | sqlc | v2 |
| DB | pgx (PG), go-sql-driver (MySQL), modernc.org/sqlite (CGO-free) | v5/latest |
| Config | Cleanenv | latest |
| Jobs | Asynq | latest |
| Logging | slog (stdlib) | 1.21+ |
| Tracing | OpenTelemetry | latest |
| Cache | Redis (go-redis) | v9 |
| Test | testcontainers-go | latest |
| Codegen | go generate (Ent or sqlc) | latest |
| WebSocket | nhooyr.io/websocket | latest |

**Module**: `github.com/axe-cute/axe`

---

## Folder Structure

```
axe/                            # FRAMEWORK repo
├── cmd/api/main.go             # Composition Root
├── internal/
│   ├── domain/                 # Entities + Interfaces ONLY
│   ├── handler/                # HTTP layer (Chi)
│   ├── service/                # Business logic
│   └── repository/             # Data access (Ent or sqlc — pick one)
├── pkg/                        # Framework libs (imported by generated projects)
│   ├── apperror/ | txmanager/ | logger/ | plugin/
├── ent/schema/                 # Ent schemas
├── db/{migrations,queries}     # SQL files
├── config/config.go            # Cleanenv struct
└── _bmad-output/               # BMAD artifacts

# SCAFFOLD (axe new) → internal/infra/ instead of pkg/:
my-app/internal/infra/{apperror,jwtauth,logger,cache,ws}
```

---

## Layer Rules (STRICT)

**domain/** — Pure. Only imports: context, errors, fmt, strings, time, uuid. NEVER: database, logging, framework, HTTP. Contains: entities, repo interfaces, service interfaces.

**handler/** — Thin HTTP adapter. Parse → Validate → Call service interface → Write response. NEVER: DB calls, business logic, direct repo calls.

**service/** — Business rules, auth checks, tx coordination (`TxManager.WithinTransaction`), outbox event append. NEVER: HTTP concerns, direct DB driver calls.

**repository/** — Data access via Ent or sqlc. Implements domain interfaces. NEVER: business logic, HTTP, calling other repos.

---

## "No Magic" Matrix

✅ Struct tags, go generate (Ent/sqlc/Wire), implicit interface satisfaction, build constraints.

❌ reflect in hot path, complex init() side effects, global mutable state post-startup, dynamic plugin loading, runtime DI.

---

## Ent vs sqlc — Choose One Per Project

**Ent** (recommended): full ORM, schema-as-code, CRUD-heavy. **sqlc**: SQL-first, complex JOINs/analytics. ⚠️ Never both in same project.

---

## Error Taxonomy (pkg/apperror) — AI MUST use

| Situation | Error |
|---|---|
| Not found | `ErrNotFound` (404) |
| Bad input | `ErrInvalidInput` (400) |
| JWT expired | `ErrUnauthorized` (401) |
| No permission | `ErrForbidden` (403) |
| DB/external fail | `ErrInternal` (500) |
| Business rule violated | `ErrConflict` (409) |

```go
// ✅ apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
// ❌ errors.New("not found")  |  c.JSON(400, map[string]string{...})
```

---

## Transaction Contract

- >1 write → MUST `TxManager.WithinTransaction()`
- Repos accept `context.Context`, extract tx from context (never self-open)
- Outbox event MUST be in same tx as primary write

```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        order, err := s.orderRepo.Create(ctx, ...)
        if err != nil { return err }
        return s.outboxRepo.Append(ctx, OrderPlacedEvent{OrderID: order.ID})
    })
}
```

---

## Outbox — When to use

Side effects after DB write → always Outbox: send email after user create, notify after payment, trigger analytics after update.

---

## Interface-First: define in `domain/` → implement in `repository/`/`service/` → wire in `main.go`.

## Testing: Integration (testcontainers) → Service unit (mock repo) → Handler unit (httptest) → Domain (pure). AI MUST generate tests alongside code.

## Logger: `logger.FromCtx(ctx).With("key", val)` — never global `slog.Info()`.

---

## API Safety (audit v3)

- Pagination: upper bound 100
- Create DTO: no server fields (views, created_at)
- Rate limiter: RemoteAddr default (no X-Forwarded-For trust)
- Event Bus Publish(): returns sync handler errors
- Framework: no domain logic

---

## PR Checklist

```
□ Correct layer? □ Interface in domain/ first? □ Tx wrap if >1 write?
□ apperror taxonomy? □ Domain no infra imports? □ Tests with code?
□ Explicit SQL columns? □ Outbox for side effects? □ Context propagated?
□ Logger via context? □ Pagination capped? □ No server fields in Create DTO?
□ go vet pass? □ go test pass?
```

**Reference**: User domain (`internal/domain/user.go` → `handler/` → `service/` → `repository/`).
