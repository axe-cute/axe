# 🤖 Required AI Capabilities
> Skills an AI must have to work effectively in the axe project
> without breaking the "no magic" architecture.
>
> 🇻🇳 [Phiên bản tiếng Việt](vi/06_ai_skills.md)

---

## Why AI Skills Matter Especially With axe

axe has a **stricter-than-usual architecture**:
- Clear layer boundaries that must not be violated
- Strict import rules (especially in `internal/domain/`)
- Interface-first design: AI must understand interfaces before implementing
- Transaction boundaries must be recognized by AI
- Error taxonomy is mandatory — AI must not create new error formats

**If AI lacks these skills:** it will generate syntactically correct code that breaks architecture.

---

## Skill 1: Layer-Aware Code Generation

AI must know which layer code is being written in and apply corresponding rules.

```
Prompt: "Write a function to list a user's orders"

AI MUST NOT:
  → in domain/: call database
  → in handler/: call repository.List()
  → in service/: parse HTTP request
  → in repository/: contain business rules

AI MUST:
  → handler/: parse params, call service.ListOrders(ctx, userID, pagination)
  → service/: validate ownership, call repo.ListByUserID(ctx, userID)
  → repository/: SQL/DB calls only
```

---

## Skill 2: Interface-First Thinking

Before writing implementation, AI must extract interface in `domain/`.

```
Step 1: Define interface in domain/
  type OrderRepository interface {
      Create(ctx, order) error
      FindByID(ctx, id) (*Order, error)
      ListByUserID(ctx, userID, pagination) ([]*Order, error)
  }

Step 2: Implement in repository/
  type postgresOrderRepo struct { db *ent.Client }

Step 3: Wire in main.go
  repo := repository.NewPostgresOrderRepo(entClient)
  svc := service.NewOrderService(repo, txMgr)
  handler := handler.NewOrderHandler(svc)
```

**Common AI mistake:** Writing implementation directly → injecting concrete type into service → not testable.

---

## Skill 3: Transaction Boundary Recognition

AI must recognize when an operation needs a transaction and wrap correctly.

**Detection triggers:**
```
"create ... and update ..."
"insert ... if successful then insert ..."
"process payment and create order"
"batch operation"
"multi-table write"
```

**Expected output:**
```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        order, err := s.orderRepo.Create(ctx, ...)
        if err != nil { return err }

        if err := s.inventoryRepo.Deduct(ctx, ...); err != nil {
            return err // auto rollback
        }

        return s.outboxRepo.Append(ctx, OrderPlacedEvent{OrderID: order.ID})
    })
}
```

---

## Skill 4: Error Taxonomy Compliance

AI must not create custom error responses. Must use `pkg/apperror` taxonomy.

| Situation | Error Type |
|---|---|
| Record not found | `apperror.ErrNotFound` |
| Invalid input format | `apperror.ErrInvalidInput` |
| JWT expired/missing | `apperror.ErrUnauthorized` |
| User lacks permission | `apperror.ErrForbidden` |
| DB/external failure | `apperror.ErrInternal` |
| Business rule violated | `apperror.ErrConflict` |

```go
// ❌ Custom error format
return c.JSON(400, map[string]string{"error": "user not found"})

// ✅ apperror taxonomy
return apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
```

---

## Skill 5: Import Discipline (Domain Layer Guard)

AI must refuse to import infra packages into `internal/domain/`.

```go
// ✅ ALLOWED in domain/:
import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
    "github.com/google/uuid"  // type definition only
)

// ❌ FORBIDDEN in domain/:
import (
    "database/sql"
    "github.com/jackc/pgx/v5"      // infra
    "go.uber.org/zap"               // logging
    "github.com/chi-router/..."     // framework
    "entgo.io/ent"                  // ORM
)
```

---

## Skill 6: Outbox Pattern Awareness

When side effects follow DB writes (send email, notify, trigger job), AI must suggest Outbox instead of direct calls.

```go
// ❌ Direct call after DB write (inconsistency risk)
if err := repo.CreateOrder(ctx, order); err != nil {
    return err
}
emailService.SendConfirmation(order.UserEmail) // may fail!

// ✅ Outbox pattern
return tx.WithinTransaction(ctx, func(ctx context.Context) error {
    if err := repo.CreateOrder(ctx, order); err != nil {
        return err
    }
    return outboxRepo.Append(ctx, events.OrderCreated{OrderID: order.ID})
})
```

---

## Skill 7: SQL Quality Awareness

AI writing sqlc queries must ensure: indexes are hinted, no N+1, proper pagination, no `SELECT *`.

```sql
-- ❌ SELECT * (over-fetching)
SELECT * FROM orders WHERE user_id = $1;

-- ✅ Explicit columns
SELECT id, status, total_price, created_at FROM orders WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
```

---

## Skill 8: Test Generation Pattern

Every code generation must include corresponding tests:

```
Handler test: httptest + mock Service interface → 200, 400, 401, 404
Service test: mock Repository interface → happy path, validation fail, repo error
Repository test: testcontainers-go (real PostgreSQL) → insert, query, constraints
```

---

## Skill 9: ADR Awareness

When proposing architectural changes, AI must suggest writing an ADR (Architecture Decision Record).

**Triggers:** new dependency, error handling change, auth approach change, new layer, transaction pattern change.

---

## Skill 10: Code Review Checklist

AI must self-apply this checklist when generating or reviewing code:

```
□ Code in the correct layer?
□ Interface defined in domain/?
□ Transaction wrapped when needed?
□ Errors use apperror taxonomy?
□ Domain layer free of infra imports?
□ Tests written alongside production code?
□ SQL has explicit columns, pagination, indexes?
□ Outbox pattern if side effects exist?
□ Context propagated through all function calls?
□ Logger injected via context, not global?
```

---

## AI Skills Matrix

| Skill | Priority | Implementation Difficulty |
|---|---|---|
| Layer-aware generation | 🔴 Critical | Medium |
| Interface-first thinking | 🔴 Critical | Medium |
| Transaction boundary recognition | 🔴 Critical | High |
| Error taxonomy compliance | 🔴 Critical | Low |
| Import discipline | 🟠 High | Low |
| Outbox pattern awareness | 🟠 High | Medium |
| SQL quality awareness | 🟡 Medium | Medium |
| Test generation pattern | 🟡 Medium | Low |
| ADR awareness | 🟢 Low | Low |
| Code review checklist | 🟡 Medium | Low |
