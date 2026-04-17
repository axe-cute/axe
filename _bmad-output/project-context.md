# Project Context — axe

> File này được inject vào mọi AI agent session.
> Mọi code generate phải tuân thủ các rules dưới đây.
> Không được overwrite hay skip phần nào trong file này.

---

## Project Overview

**axe** là một Go web framework nội bộ (internal platform):
- Clean Architecture baked-in, zero runtime magic
- CLI generator (`axe generate resource`) tạo CRUD endpoint < 10 phút
- Production-grade từ ngày đầu: transactions, observability, error handling

**Status**: Phase 2 (Plugin Ecosystem) — Sprint 21

---

## Tech Stack

| Layer | Technology | Version |
|---|---|---|
| Language | Go | 1.22+ |
| HTTP Router | Chi | v5 |
| ORM (writes) | Ent | latest |
| Query builder (reads) | sqlc | v2 |
| Database driver | pgx | v5 |
| Config | Cleanenv | latest |
| Background jobs | Asynq | latest |
| Logging | slog (stdlib) | Go 1.21+ |
| Tracing | OpenTelemetry | latest |
| Cache | Redis (go-redis) | v9 |
| Test containers | testcontainers-go | latest |
| Code generation | go generate (Ent + sqlc) | latest |
| Database driver | pgx (PostgreSQL) | v5 |
| Database driver | go-sql-driver (MySQL) | latest |
| Database driver | modernc.org/sqlite (CGO-free) | latest |
| WebSocket | nhooyr.io/websocket | latest |

**Go module**: `github.com/axe-cute/axe`

---

## Folder Structure

```
axe/
├── cmd/
│   └── api/
│       └── main.go          # Composition Root (Wire)
├── internal/
│   ├── domain/              # Entities + Interfaces ONLY
│   ├── handler/             # HTTP layer (Chi handlers)
│   ├── service/             # Business logic
│   └── repository/          # Data access (Ent + sqlc)
├── pkg/
│   ├── apperror/            # Error taxonomy
│   ├── txmanager/           # Transaction manager
│   ├── logger/              # Structured logging (slog)
│   └── validator/           # Input validation
├── ent/
│   └── schema/              # Ent schema definitions
├── db/
│   ├── migrations/          # SQL migration files
│   └── queries/             # sqlc SQL queries
├── config/
│   └── config.go            # Cleanenv struct
├── _bmad-output/            # BMAD artifacts (không commit code vào đây)
└── docs/                    # Architecture docs
```

---

## Layer Rules (STRICT — không được vi phạm)

### internal/domain/ — Pure Domain
```go
// ✅ CHỈ import:
import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
    "github.com/google/uuid"  // type only
)
// ❌ KHÔNG BAO GIỜ: database, logging, framework, HTTP, validation libs
```
**Trách nhiệm**: Entity definitions + Repository interfaces + Service interfaces

### internal/handler/ — HTTP Layer
```
✅ Parse HTTP request (JSON, query params, path params)
✅ Validate input format (required fields, type checks)
✅ Call service layer via interface
✅ Write HTTP response (status code + JSON body)
❌ KHÔNG: Database calls, business logic, direct repository calls
```

### internal/service/ — Business Logic
```
✅ Business rules và validations (email tồn tại?)
✅ Authorization checks (ownership)
✅ Transaction coordination (TxManager.WithinTransaction)
✅ Outbox event appending (trong cùng transaction)
✅ Calling repository interfaces
❌ KHÔNG: HTTP concerns (headers, status codes), direct DB driver calls
```

### internal/repository/ — Data Access
```
✅ Database read/write via Ent (writes) hoặc sqlc (complex reads)
✅ Implement interfaces defined in internal/domain/
❌ KHÔNG: Business logic, HTTP concerns, calling other repositories
```

---

## "No Magic" Decision Matrix

```
✅ ALLOWED (Compile-time, inspectable):
  - Struct tags (json, db, validate)
  - go generate + Ent codegen + sqlc codegen + Wire codegen
  - Implicit interface satisfaction (compiler-verified)
  - Build constraints //go:build

❌ FORBIDDEN (Runtime, opaque):
  - reflect.ValueOf / reflect.TypeOf trong hot path
  - init() với side effects phức tạp
  - Global mutable state sau startup
  - Dynamic plugin loading
  - Runtime dependency injection
```

---

## Ent vs sqlc — Usage Rules

```
WRITE operations     → Ent (always)
READ - simple by ID  → Ent (consistency)
READ - JOIN/aggregate → sqlc
READ - pagination    → sqlc (LIMIT/OFFSET)
READ - analytics     → sqlc
```

**Connection**: Cả Ent và sqlc dùng chung 1 `*sql.DB` connection pool:
```go
db, _ := sql.Open("pgx", cfg.DatabaseURL)
entClient := ent.NewClient(ent.Driver(entsql.OpenDB("pgx", db)))
queries := sqlc.New(db)
```

---

## Error Taxonomy (pkg/apperror)

AI **PHẢI** dùng taxonomy này, không được tự tạo error format:

| Tình huống | Error type |
|---|---|
| Record not found | `apperror.ErrNotFound` |
| Invalid input format | `apperror.ErrInvalidInput` |
| JWT expired/missing | `apperror.ErrUnauthorized` |
| User lacks permission | `apperror.ErrForbidden` |
| DB/external failure | `apperror.ErrInternal` |
| Business rule violated | `apperror.ErrConflict` |

```go
// ✅ Đúng
return apperror.ErrNotFound.WithMessage("user not found").WithCause(err)

// ❌ Sai — tuyệt đối không làm
return errors.New("not found")
return c.JSON(400, map[string]string{"error": "user not found"})
```

---

## Transaction Contract

```
RULE: Service method với > 1 write operation PHẢI dùng TxManager.WithinTransaction()

RULE: Repository methods PHẢI accept context.Context
      và extract transaction từ context (không tự mở tx)

RULE: Outbox event PHẢI append trong cùng transaction với DB write chính
```

```go
// Pattern chuẩn:
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        order, err := s.orderRepo.Create(ctx, ...)
        if err != nil { return err }
        return s.outboxRepo.Append(ctx, OrderPlacedEvent{OrderID: order.ID})
    })
}
```

---

## Outbox Pattern — Khi nào dùng

```
Trigger AI phải suggest Outbox (không direct call):
  "Sau khi tạo user, gửi email"         → Outbox
  "Sau khi payment success, notify"     → Outbox
  "Sau khi update, trigger analytics"   → Outbox
```

---

## Interface-First Design

AI luôn phải:
1. Define interface trong `internal/domain/` TRƯỚC
2. Implement trong `internal/repository/` hoặc `internal/service/`
3. Wire trong `cmd/api/main.go`

---

## Testing Pyramid

```
Layer 3: Integration — testcontainers-go + real PostgreSQL
Layer 2: Service unit — mock Repository interface (testify/mock)
Layer 1: Handler unit — httptest + mock Service interface
Layer 0: Domain unit — pure functions, no mock
```

AI generate code PHẢI generate test cùng lúc (không tách riêng).

---

## Logger Pattern (Context-Aware)

```go
// ✅ Đúng: logger từ context (có request_id)
func (s *OrderService) CreateOrder(ctx context.Context, ...) error {
    logger := logger.FromCtx(ctx).With("order_id", order.ID)
    logger.Info("creating order")
}

// ❌ Sai: global logger
slog.Info("creating order") // mất request_id
```

---

## PR Checklist (AI tự apply trước khi hoàn thành)

```
□ Code ở đúng layer?
□ Interface define trong domain/ trước implement?
□ Transaction wrap nếu > 1 write?
□ Error dùng apperror taxonomy?
□ Domain layer không import infra packages?
□ Test viết cùng production code?
□ SQL có explicit columns, không SELECT *?
□ Outbox nếu có side effect?
□ Context propagate qua tất cả function calls?
□ Logger inject qua context?
□ go vet ./... pass?
□ go test ./... pass?
```

---

## Reference Implementation

`internal/domain/user.go`, `internal/handler/user_handler.go`,
`internal/service/user_service.go`, `internal/repository/user_repo.go`

→ Khi có doubt, đọc User domain làm mẫu.
