# 🤖 AI Skills Cần Thiết (Required AI Capabilities)
> Những kỹ năng AI phải có để làm việc hiệu quả trong project axe
> mà không phá vỡ kiến trúc "no magic".
>
> 🇬🇧 [English version](../06_ai_skills.md)

---

## Tại Sao AI Skills Quan Trọng Đặc Biệt Với axe

axe có kiến trúc **stricte hơn bình thường**:
- Layer boundaries rõ ràng và không được vi phạm
- Import rules nghiêm ngặt (đặc biệt trong `internal/domain/`)
- Interface-first design: AI phải hiểu interface trước khi implement
- Transaction boundaries phải được AI nhận biết
- Error taxonomy bắt buộc — AI không được tự tạo error format mới

**Nếu AI không có những skills này:** nó sẽ generate code đúng về syntax nhưng phá kiến trúc.

---

## Skill 1: Layer-Aware Code Generation

**Mô tả:**
AI phải biết code đang được viết ở layer nào và áp dụng rules tương ứng.

**Kiểm tra:**
```
Prompt: "Viết function lấy danh sách orders của user"

AI KHÔNG được viết:
  → trong domain/: gọi database
  → trong handler/: gọi repository.List()
  → trong service/: parse HTTP request
  → trong repository/: chứa business rule

AI PHẢI:
  → handler/: parse params, gọi service.ListOrders(ctx, userID, pagination)
  → service/: validate ownership, gọi repo.ListByUserID(ctx, userID)
  → repository/: chỉ SQL/DB call
```

**Training signal:**
Provide layer-specific constraint files mà AI đọc trước khi generate.

---

## Skill 2: Interface-First Thinking

**Mô tả:**
Trước khi viết implementation, AI phải extract interface trong `domain/`.

**Pattern AI phải follow:**

```
Step 1: Define interface trong domain/
  type OrderRepository interface {
      Create(ctx, order) error
      FindByID(ctx, id) (*Order, error)
      ListByUserID(ctx, userID, pagination) ([]*Order, error)
  }

Step 2: Implement trong repository/
  type postgresOrderRepo struct { db *ent.Client }
  func (r *postgresOrderRepo) Create(ctx, order) error { ... }

Step 3: Wire trong main.go
  repo := repository.NewPostgresOrderRepo(entClient)
  svc := service.NewOrderService(repo, txMgr)
  handler := handler.NewOrderHandler(svc)
```

**Lỗi AI thường mắc:**
Viết implementation trực tiếp → inject concrete type vào service → không testable.

---

## Skill 3: Transaction Boundary Recognition

**Mô tả:**
AI phải tự nhận biết khi nào một operation cần transaction và wrap đúng cách.

**Pattern nhận biết:**
```
Trigger words → cần transaction:
  "create ... và update ..."
  "insert ... nếu thành công thì insert ..."
  "xử lý payment và tạo order"
  "batch operation"
  "multi-table write"
```

**Output AI phải generate khi detect:**
```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        // Tất cả operations dùng ctx này sẽ trong cùng transaction
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

**Mô tả:**
AI không được tự tạo error responses. Phải dùng `pkg/apperror` taxonomy.

**AI phải biết map:**

| Situation | Error Type |
|---|---|
| Record not found | `apperror.ErrNotFound` |
| Invalid input format | `apperror.ErrInvalidInput` |
| JWT expired/missing | `apperror.ErrUnauthorized` |
| User lacks permission | `apperror.ErrForbidden` |
| DB/external failure | `apperror.ErrInternal` |
| Business rule violated | `apperror.ErrConflict` |

> **Lưu ý**: Trong source code thực tế, struct dùng field `HTTPStatus` (không phải `Status`).
> Xem `pkg/apperror/apperror.go` để biết chi tiết.

**AI không được viết:**
```go
// ❌ Custom error format
return c.JSON(400, map[string]string{"error": "user not found"})

// ❌ Raw errors
return errors.New("not found")
```

**AI phải viết:**
```go
// ✅ apperror taxonomy
return apperror.ErrNotFound.WithMessage("user not found").WithCause(err)
```

---

## Skill 5: Import Discipline (Domain Layer Guard)

**Mô tả:**
AI phải từ chối import infra packages vào `internal/domain/`.

**Domain import whitelist AI phải enforce:**
```go
// ✅ ALLOWED in domain/:
import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
    "github.com/google/uuid"  // chỉ type definition
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

**AI phải detect và cảnh báo** nếu user muốn thêm forbidden import vào domain layer.

---

## Skill 6: Outbox Pattern Awareness

**Mô tả:**
Khi có side effects sau DB write (gửi email, notify, trigger job), AI phải suggest Outbox pattern thay vì direct call.

**Pattern AI phải recognize:**
```
"Sau khi tạo user, gửi welcome email" → Outbox pattern
"Sau khi payment success, notify order" → Outbox pattern
"Sau khi update order, trigger analytics" → Outbox pattern
```

**AI không được generate:**
```go
// ❌ Direct call sau DB write (inconsistency risk)
if err := repo.CreateOrder(ctx, order); err != nil {
    return err
}
emailService.SendConfirmation(order.UserEmail) // có thể fail!
```

**AI phải generate:**
```go
// ✅ Outbox pattern
return tx.WithinTransaction(ctx, func(ctx context.Context) error {
    if err := repo.CreateOrder(ctx, order); err != nil {
        return err
    }
    // Lưu event cùng transaction → atomic
    return outboxRepo.Append(ctx, events.OrderCreated{OrderID: order.ID})
})
```

---

## Skill 7: SQL Quality Awareness (khi dùng sqlc)

**Mô tả:**
AI viết SQL queries cho sqlc phải đảm bảo:
- Có index hint khi cần
- Không gây N+1
- Pagination đúng (`LIMIT $1 OFFSET $2`)
- Không dùng `SELECT *` trong production queries

**AI phải flag:**
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

**Mô tả:**
Mỗi khi generate code, AI phải generate test cùng lúc theo đúng pattern.

**Testing rules:**
```
Handler test:
  → httptest.NewRecorder() + httptest.NewRequest()
  → Mock Service bằng interface (không cần DB)
  → Test: 200 OK, 400 Bad Input, 401 Unauthorized, 404 Not Found

Service test:
  → Mock Repository bằng interface
  → Test: happy path, validation fail, repo error

Repository test:
  → testcontainers-go (real PostgreSQL in Docker)
  → Test: insert + query + constraint violations
```

---

## Skill 9: Architecture Decision Record (ADR) Awareness

**Mô tả:**
Khi AI đề xuất thay đổi architectural decision, phải đề xuất viết ADR.

**Trigger cho ADR:**
- Thêm dependency mới
- Thay đổi error handling strategy
- Thay đổi authentication approach
- Thêm layer mới
- Thay đổi transaction pattern

**ADR template AI nên output:**
```markdown
# ADR-XXX: [Decision Title]
Date: YYYY-MM-DD
Status: Proposed | Accepted | Deprecated

## Context
[Tại sao cần ra quyết định này?]

## Decision
[Quyết định là gì?]

## Consequences
✅ [Lợi ích]
❌ [Đánh đổi]
```

---

## Skill 10: Code Review Checklist

**Mô tả:**
AI phải tự apply checklist khi review hoặc generate code:

```markdown
## Pre-commit checklist AI tự check:
□ Code ở đúng layer không?
□ Interface được define trong domain/ chưa?
□ Transaction cần thiết đã được wrap chưa?
□ Error dùng apperror taxonomy chưa?
□ Domain layer không import infra packages?
□ Test được viết cùng production code?
□ SQL có explicit columns, pagination, index?
□ Outbox pattern nếu có side effect?
□ Context được propagate qua tất cả function calls?
□ Logger inject qua context, không phải global?
```

---

## Tóm tắt AI Skills Matrix

| Skill | Priority | Khó implement |
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
