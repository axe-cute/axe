# 🌫️ Điều Mờ (Murky Areas)
> Những điểm **không sai, không đúng** — mơ hồ, thiếu định nghĩa rõ ràng,
> khiến team khó ra quyết định nhất quán khi build thực tế.

---

## 1. "Không Ma Thuật" — Định Nghĩa Chưa Đủ Operationalizable

**Vấn đề:**
Cả hai báo cáo đều dùng cụm "no magic" / "tường minh" rất nhiều, nhưng:
- Ai quyết định cái gì là "magic"?
- `go generate` có phải magic không?
- Ent codegen có phải magic không?
- Struct tags (`json:"name"`) có phải magic không?

**Tại sao mờ:**
Không có **test/checklist** để verify "no magic". Khi review PR, 2 engineer có thể tranh luận mãi về việc một đoạn code có "magic" hay không.

**Làm rõ cần thiết:**

```markdown
## "No Magic" Decision Matrix:

✅ ALLOWED (Compile-time, inspectable, generates static code):
  - Struct tags (json, db, validate)
  - go generate + Ent + sqlc + Wire
  - Interface satisfaction (implicit, but compiler-verified)
  - Build constraints

❌ FORBIDDEN (Runtime, opaque, hides control flow):
  - reflect.ValueOf / reflect.TypeOf trong runtime hot path
  - init() functions với side effects phức tạp
  - Global var mutation sau startup
  - Monkey patching (impossible in Go, nhưng cần nêu rõ)
  - Plugin system với dynamic loading
```

---

## 2. Ranh Giới Giữa Service và Handler — Chưa Rõ

**Vấn đề:**
Báo cáo 1 nói:
> Handler: "phân tích yêu cầu HTTP, gọi tầng service"
> Service: "áp dụng các thuật toán kinh doanh"

Nhưng không define:
- Validation thuộc về Handler hay Service?
- Authorization check (RBAC) thuộc về Middleware, Handler, hay Service?
- DTO conversion (HTTP request → domain struct) xảy ra ở đâu?
- Pagination logic thuộc về Handler, Service, hay Repository?

**Hệ quả thực tế:**
```
Dev A: đặt email validation ở Handler
Dev B: đặt email validation ở Service
Dev C: đặt validation ở domain entity
→ 3 cách, không nhất quán, conflict khi merge
```

**Làm rõ cần thiết:**

| Concern | Layer | Lý do |
|---|---|---|
| Input parsing (JSON → struct) | Handler | Phụ thuộc HTTP protocol |
| Input validation (format, required) | Handler | Trả về 400 trước khi vào business |
| Business validation (email đã tồn tại?) | Service | Cần DB access |
| Authorization (user có quyền không?) | Middleware + Service | Middleware check token, Service check ownership |
| DTO → Domain Entity mapping | Service | Cách ly handler khỏi domain changes |
| Pagination params | Handler | Parse từ query string |
| Pagination logic | Repository | SQL LIMIT/OFFSET |

---

## 3. Ent vs sqlc — Coexistence Chưa Được Define

**Vấn đề:**
Báo cáo 1 kết luận: "dùng Ent chính, sqlc cho analytics". Báo cáo 2 đồng ý nhưng không có implementation guide.

**Mờ cụ thể:**
- 2 tool này dùng **2 connection pool riêng** hay dùng chung?
- Migration quản lý bởi Ent Atlas hay bằng file SQL riêng?
- Khi Ent schema thay đổi, sqlc queries có tự detect không?
- Test setup: mock Ent client và sqlc queries như thế nào?

**Làm rõ cần thiết:**
```
Architecture:
  ┌─────────────────────────────────────────┐
  │           cmd/api/main.go               │
  │  db := sql.Open(...)                    │
  │  entClient := ent.NewClient(ent.Driver(db)) │
  │  queries := sqlc.New(db)               │
  │  (cùng 1 *sql.DB, 2 client wrappers)  │
  └─────────────────────────────────────────┘
```
→ **Dùng chung 1 `*sql.DB` connection pool**, 2 client layer trên đó.

---

## 4. Configuration Management — Nhiều Lựa Chọn Nhưng Không Quyết Định

**Vấn đề:**
Báo cáo 1 đề xuất "Viper hoặc Cleanenv" — nhưng không chọn.

**Sự khác biệt quan trọng:**
| | Viper | Cleanenv |
|---|---|---|
| Config file support | ✅ YAML, TOML, JSON, HCL | ❌ Chỉ `.env` và env vars |
| Struct binding | ✅ | ✅ |
| Hot reload | ✅ | ❌ |
| Dependency size | Lớn (cobra, etc.) | Nhỏ |
| "No config file" cloud-native | ❌ Có thể dùng file | ✅ Pure env vars |

**Làm rõ cần thiết:**
Nếu target là cloud-native/12-Factor → **Cleanenv**.
Nếu cần multi-environment config files → **Viper**.
Phải chọn 1, không để team tự quyết định.

---

## 5. Testing Strategy — Thiếu Nhiều Tầng

**Vấn đề:**
Báo cáo 1 nói "unit test dễ hơn Rails" qua httptest và mock DB. Đúng nhưng chưa đủ.

**Mờ cụ thể:**
- Integration test (với real DB) viết như thế nào? testcontainers-go?
- E2E test có không? Nếu có, dùng gì?
- Test data setup/teardown strategy là gì?
- Coverage target là bao nhiêu %?
- Contract testing với external APIs?

**Làm rõ cần thiết:**

```markdown
## Testing Pyramid for axe:

Layer 4: E2E (optional, smoke only)
Layer 3: Integration — testcontainers-go + real PostgreSQL
Layer 2: Service unit — mock Repository (interface)
Layer 1: Handler unit — httptest + mock Service (interface)
Layer 0: Domain unit — pure functions, no mock needed
```

---

## 6. Observability — Hoàn Toàn Không Được Nhắc Đến

**Vấn đề:**
Cả 2 báo cáo gần như không đề cập:
- Logging format (JSON structured logs?)
- Metrics (Prometheus? OpenTelemetry?)
- Tracing (distributed tracing khi có microservices?)
- Health check endpoints

**Mờ cụ thể:**
- Logger inject vào mọi layer hay dùng `slog` global?
- Request ID propagation qua `context.Context` như thế nào?
- Log level per environment?

**Làm rõ cần thiết:**
```go
// Context-aware structured logging pattern
func (s *OrderService) CreateOrder(ctx context.Context, ...) error {
    logger := LoggerFromCtx(ctx). // lấy từ context, có request_id
        With("order_id", order.ID)

    logger.Info("creating order")
    ...
}
```

---

## 7. Authentication / Authorization Model — Mờ Hoàn Toàn

**Vấn đề:**
Báo cáo 1 nhắc đến "middleware JWT" nhưng không define:
- JWT hay Session? Tại sao?
- RBAC hay ABAC hay Policy-based?
- Token refresh strategy?
- Multi-tenant support?
- Permission check xảy ra ở đâu (middleware vs service)?

---

## Tóm tắt Điều Mờ

```
🌫️ "No Magic" definition        → cần decision matrix cụ thể
🌫️ Service vs Handler boundary  → cần responsibility table
🌫️ Ent + sqlc coexistence       → cần shared connection pool guide
🌫️ Config management            → Viper hay Cleanenv phải chọn 1
🌫️ Testing strategy             → thiếu pyramid, thiếu integration test
🌫️ Observability                → hoàn toàn chưa define
🌫️ Auth model                   → JWT / RBAC strategy không rõ
```
