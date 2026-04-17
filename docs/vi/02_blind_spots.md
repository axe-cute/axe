# 🕳️ Điểm Mù (Blind Spots)
> 🇬🇧 [English version](../02_blind_spots.md)
> Những vấn đề mà cả hai báo cáo **không nhìn thấy** — không phải sai, mà là **chưa nghĩ đến**.

---

## 1. Không có Transaction Model — Lỗ hổng Critical Nhất

**Vấn đề:**
Báo cáo 1 mô tả Repository Pattern rất rõ, nhưng **không hề nhắc đến transaction boundary**.

**Ví dụ thực tế vỡ hệ thống:**
```
Create Order:
  → insert order          ← repo call 1
  → insert order_items    ← repo call 2
  → update inventory      ← repo call 3
```
Nếu `update inventory` fail sau khi 2 cái trên đã commit → **data inconsistency vĩnh viễn**.

**Fix bắt buộc — Unit of Work Pattern:**
```go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// Inject TxManager vào Service, KHÔNG vào Repository
type OrderService struct {
    tx       TxManager
    orderRepo OrderRepository
    itemRepo  ItemRepository
    stockRepo StockRepository
}

func (s *OrderService) CreateOrder(ctx context.Context, input CreateOrderInput) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        // Tất cả 3 repo calls dùng chung transaction từ ctx
        ...
    })
}
```

**Tại sao là blind spot:**
Báo cáo 1 hiểu DI rất tốt nhưng transaction là một dạng **cross-cutting concern** không thuộc về một layer cụ thể → dễ bị bỏ sót.

---

## 2. Không có Outbox Pattern — Consistency Khi Có Queue

**Vấn đề:**
Báo cáo 1 đề xuất dùng Asynq/Watermill cho background jobs. Nhưng không nói:
```
DB write SUCCESS
Queue publish FAIL
→ Job không bao giờ chạy
→ Không ai biết
```

Hoặc ngược lại:
```
Queue publish SUCCESS
DB write FAIL (rollback)
→ Job chạy trên dữ liệu không tồn tại
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
- Write DB + outbox event trong **cùng 1 transaction**
- Background poller đọc outbox → publish queue → mark processed
- At-least-once delivery đảm bảo không mất event

**Tại sao là blind spot:**
Outbox thường được nhớ đến khi hệ thống đã có lỗi production. Với team Go mới, rất dễ bị bỏ qua.

---

## 3. Không Define Domain Boundary Strict

**Vấn đề:**
Báo cáo 1 nói `internal/domain/` là "lõi bất biến" nhưng không có rule cụ thể về:
- Domain có được import `uuid` package không?
- Domain có được dùng `time.Time` không?
- Domain có được import validation library không?

**Hệ quả khi không define:**
```go
// Ai đó thêm vào domain/user.go:
import "github.com/go-playground/validator/v10"  // ← PHẠM LUẬT
import "github.com/google/uuid"                  // ← OK hay không?
import "go.uber.org/zap"                         // ← PHẠM LUẬT nghiêm trọng
```

Khi AI agent generate code, nó sẽ import lung tung vào domain → **phá kiến trúc từ từ**.

**Fix — Domain Allowed Dependencies List:**
```markdown
## Domain Layer — Allowed Imports Only:
✅ Standard library: `time`, `strings`, `errors`, `fmt`, `context`
✅ Value types: `github.com/google/uuid` (chỉ type, không gọi generator)
❌ Logging packages (zap, slog)
❌ Validation frameworks
❌ Any infra packages (database/sql, redis, http)
❌ Any framework packages
```

---

## 4. CQRS Light Chưa Được Define

**Vấn đề:**
Báo cáo 1 đề xuất dùng **cả Ent và sqlc** nhưng không có rule khi nào dùng cái nào.

**Hệ quả:**
- Dev dùng Ent cho tất cả → dashboard analytics query chạy chậm
- Dev dùng sqlc cho tất cả → mutation logic phức tạp, khó maintain

**Fix — CQRS Light Decision:**
```markdown
## Write Model → Ent
- Mutations (Insert, Update, Delete)
- Entity relationships
- Schema migrations

## Read Model → sqlc
- Dashboard queries
- Reports / Analytics
- Search với nhiều join
- Pagination phức tạp
```

Đây không phải full CQRS (separate DB), chỉ là **query model separation** — nhẹ và thực tế.

---

## 5. Không Có Error Taxonomy

**Vấn đề:**
Báo cáo 1 nói "explicit error handling – if err != nil" nhưng không define:
- Error codes là gì?
- HTTP status mapping như thế nào?
- Ai quyết định lỗi nào là 400 vs 500?

**Hệ quả:**
```go
// Handler A:
return c.JSON(400, map[string]string{"error": "user not found"})

// Handler B:
return c.JSON(404, map[string]string{"message": "User does not exist"})

// Handler C:
return c.JSON(200, map[string]string{"status": "error", "detail": "not found"})
```
→ Frontend không thể xử lý consistent.

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

## 6. Failure Strategy Hoàn Toàn Vắng Mặt

**Vấn đề:**
Cả hai báo cáo không hề nhắc đến:
- DB down → xử lý thế nào?
- Redis down → fallback về không có cache?
- Queue down → reject request hay degrade gracefully?
- External API timeout → retry? circuit breaker?

**Fix — Failure Mode Matrix (bắt buộc define):**

| Dependency | Down Behavior | Strategy |
|---|---|---|
| PostgreSQL | 503 + alert | Health check endpoint |
| Redis | Degrade (no cache) | Feature flag |
| Queue (Asynq) | Log + retry in-process | Dead letter queue |
| External API | Timeout 5s + fallback | Circuit breaker |

---

## 7. Zero Developer Adoption Strategy

**Vấn đề (từ báo cáo 2):**
Toàn bộ báo cáo 1 tối ưu cho **correctness**, không tối ưu cho **adoption**.

**Điểm mù cụ thể:**
- Không có generator / scaffolding tool
- Không có template CRUD endpoint mẫu
- Không có "Hello World in 5 minutes" guide
- Không đo **Time-to-First-Feature** cho dev mới

**Hệ quả thực tế:**
Dev mới join team → mất 2–3 ngày chỉ để hiểu boilerplate → frustration → shortcuts → phá kiến trúc.

**Fix:**
```
axe generate resource User --fields="name:string,email:string,age:int"
```
→ Tự động tạo: domain entity, service interface, repository, handler, migration file.

---

## Tóm tắt Điểm Mù

```
🕳️ Transaction model          → không có, sẽ gây data corruption
🕳️ Outbox pattern             → không có, sẽ mất events production
🕳️ Domain boundary strict     → chưa define, AI sẽ phá dần
🕳️ CQRS light (Ent vs sqlc)   → không có rule, dev tự đoán
🕳️ Error taxonomy             → không có, API response không consistent
🕳️ Failure strategy           → không có, production sẽ vỡ
🕳️ Developer adoption         → không có, kiến trúc đẹp nhưng không dùng được
```
