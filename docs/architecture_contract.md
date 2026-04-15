# 📜 Architecture Contract
> Văn bản "hiến pháp" của axe platform.
> Mọi PR, mọi AI-generated code, mọi quyết định kỹ thuật
> đều phải tuân thủ document này.

---

## Định Nghĩa "No Magic"

```markdown
✅ ALLOWED MAGIC (Compile-time, inspectable, generates static code):
  - Struct tags (json:"...", db:"...", validate:"...")
  - go generate + Ent codegen + sqlc codegen + Wire codegen
  - Implicit interface satisfaction (compiler-verified)
  - Build constraints //go:build

❌ FORBIDDEN MAGIC (Runtime, opaque, hides control flow):
  - reflect.ValueOf / reflect.TypeOf trong runtime hot path
  - init() functions với side effects phức tạp
  - Global mutable state sau startup
  - Dynamic plugin loading
  - Runtime dependency injection
```

---

## Layer Rules

### internal/domain/ — Allowed Imports
```go
// ✅ ONLY:
import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
    "github.com/google/uuid"  // type only
)
// ❌ NEVER: database, logging, framework, validation libs
```

### internal/handler/ — Responsibilities
```
✅ Parse HTTP request (JSON, query params, path params)
✅ Validate input format (required fields, types)
✅ Call service layer via interface
✅ Write HTTP response (status code + JSON)
❌ Database calls
❌ Business logic
❌ Direct repository calls
```

### internal/service/ — Responsibilities
```
✅ Business rules
✅ Authorization checks
✅ Transaction coordination (TxManager)
✅ Outbox event appending
✅ Calling repository interfaces
❌ HTTP concerns (headers, status codes)
❌ Direct database driver calls
```

### internal/repository/ — Responsibilities
```
✅ Database read/write via Ent or sqlc
✅ Implement interfaces defined in domain/
❌ Business logic
❌ HTTP concerns
❌ Calling other repositories (use service for coordination)
```

---

## Ent vs sqlc Usage Rules

```
WRITE operations → Ent (always)
READ operations:
  - Simple entity fetch by ID → Ent (for consistency)
  - Complex queries (JOIN, aggregation, full-text) → sqlc
  - Pagination with cursor → sqlc
  - Analytics / Reports → sqlc
```

---

## Error Handling Contract

```go
// Repository returns wrapped errors:
return fmt.Errorf("create order: %w", err)

// Service returns apperror types:
if ent.IsNotFound(err) {
    return apperror.ErrNotFound.WithMessage("order not found").WithCause(err)
}

// Handler maps to HTTP:
// → handled by central error middleware, not inline
```

---

## Transaction Contract

```
Rule: Nếu một service method thực hiện > 1 write operation,
      nó PHẢI wrap trong TxManager.WithinTransaction()

Rule: Repository methods PHẢI accept context.Context
      và extract transaction từ context (không tự mở tx)

Rule: Outbox event PHẢI được append trong cùng transaction
      với DB write chính
```

---

## PR Checklist

Mọi PR vào main phải pass:
```
□ go vet ./...
□ staticcheck ./...
□ go test ./... (all green)
□ No forbidden imports in domain/
□ Error taxonomy used (no raw errors at handler level)
□ Transaction wrapped if multiple writes
□ ADR updated if architectural decision changed
□ Test coverage không giảm so với trước PR
```
