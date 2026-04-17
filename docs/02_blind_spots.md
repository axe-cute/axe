# 🕳️ Blind Spots
> Issues that both reports **didn't see** — not wrong, but **not thought of**.
>
> 🇻🇳 [Phiên bản tiếng Việt](vi/02_blind_spots.md)

---

## 1. No Transaction Model — Most Critical Gap

**Problem:**
Report 1 describes the Repository Pattern well, but **never mentions transaction boundaries**.

**Real-world breakage example:**
```
Create Order:
  → insert order          ← repo call 1
  → insert order_items    ← repo call 2
  → update inventory      ← repo call 3
```
If `update inventory` fails after the other two committed → **permanent data inconsistency**.

**Mandatory fix — Unit of Work Pattern:**
```go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Inject TxManager into Service, NOT Repository
type OrderService struct {
    tx        TxManager
    orderRepo OrderRepository
    itemRepo  ItemRepository
    stockRepo StockRepository
}

func (s *OrderService) CreateOrder(ctx context.Context, input CreateOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        // All 3 repo calls share the same transaction from ctx
        ...
    })
}
```

**Why blind spot:** Report 1 understands DI well, but transactions are a **cross-cutting concern** that doesn't belong to a specific layer → easy to overlook.

---

## 2. No Outbox Pattern — Consistency With Queues

**Problem:**
Report 1 proposes using Asynq/Watermill for background jobs. But doesn't address:
```
DB write SUCCESS → Queue publish FAIL → Job never runs → Nobody knows
```
Or the reverse:
```
Queue publish SUCCESS → DB write FAIL (rollback) → Job runs on non-existent data
```

**Fix — Transactional Outbox:**
```sql
CREATE TABLE outbox_events (
    id          UUID PRIMARY KEY,
    aggregate   TEXT,
    event_type  TEXT,
    payload     JSONB,
    created_at  TIMESTAMPTZ,
    processed   BOOLEAN DEFAULT FALSE
);
```
- Write DB + outbox event in **the same transaction**
- Background poller reads outbox → publishes to queue → marks processed
- At-least-once delivery guarantees no lost events

---

## 3. No Strict Domain Boundary

**Problem:**
Report 1 says `internal/domain/` is the "immutable core" but has no specific rules about:
- Can domain import `uuid` package?
- Can domain use `time.Time`?
- Can domain import validation libraries?

**Consequence when undefined:**
```go
import "github.com/go-playground/validator/v10"  // ← VIOLATION
import "github.com/google/uuid"                  // ← OK or not?
import "go.uber.org/zap"                         // ← SEVERE VIOLATION
```

When AI agents generate code, they'll import randomly into domain → **gradually break architecture**.

**Fix — Domain Allowed Dependencies:**
```
✅ Standard library: time, strings, errors, fmt, context
✅ Value types: github.com/google/uuid (type only, not generator)
❌ Logging packages (zap, slog)
❌ Validation frameworks
❌ Any infra packages (database/sql, redis, http)
❌ Any framework packages
```

---

## 4. CQRS Light Not Defined

**Problem:**
Report 1 proposes using **both Ent and sqlc** but has no rule for when to use which.

**Fix — CQRS Light Decision:**
```
Write Model → Ent: Mutations, relationships, schema migrations
Read Model  → sqlc: Dashboard queries, reports, analytics, complex joins, pagination
```

This is not full CQRS (separate DB), just **query model separation** — lightweight and practical.

---

## 5. No Error Taxonomy

**Problem:**
Report 1 says "explicit error handling – if err != nil" but doesn't define error codes, HTTP status mapping, or who decides what's 400 vs 500.

**Fix — Error Taxonomy:**
```go
type AppError struct {
    Code    string // "USER_NOT_FOUND", "INVALID_INPUT", "INTERNAL"
    Message string
    Status  int    // HTTP status
    Cause   error  // wrapped original
}

var (
    ErrNotFound     = &AppError{Code: "NOT_FOUND",      Status: 404}
    ErrUnauthorized = &AppError{Code: "UNAUTHORIZED",   Status: 401}
    ErrForbidden    = &AppError{Code: "FORBIDDEN",      Status: 403}
    ErrInvalidInput = &AppError{Code: "INVALID_INPUT",  Status: 400}
    ErrInternal     = &AppError{Code: "INTERNAL_ERROR", Status: 500}
)
```

---

## 6. Failure Strategy Completely Absent

Neither report mentions: DB down? Redis down? Queue down? External API timeout?

**Fix — Failure Mode Matrix:**

| Dependency | Down Behavior | Strategy |
|---|---|---|
| PostgreSQL | 503 + alert | Health check endpoint |
| Redis | Degrade (no cache) | Feature flag |
| Queue (Asynq) | Log + retry in-process | Dead letter queue |
| External API | Timeout 5s + fallback | Circuit breaker |

---

## 7. Zero Developer Adoption Strategy

**Problem (from Report 2):**
All of Report 1 optimizes for **correctness**, not **adoption**.

**Specific blind spots:**
- No generator / scaffolding tool
- No CRUD endpoint template
- No "Hello World in 5 minutes" guide
- No **Time-to-First-Feature** measurement for new devs

**Fix:**
```
axe generate resource User --fields="name:string,email:string,age:int"
```
→ Auto-generates: domain entity, service interface, repository, handler, migration file.

---

## Summary

```
🕳️ Transaction model          → missing, will cause data corruption
🕳️ Outbox pattern             → missing, will lose production events
🕳️ Domain boundary strict     → undefined, AI will gradually break it
🕳️ CQRS light (Ent vs sqlc)  → no rule, devs will guess
🕳️ Error taxonomy             → missing, API responses inconsistent
🕳️ Failure strategy           → missing, production will break
🕳️ Developer adoption         → missing, beautiful architecture nobody can use
```
