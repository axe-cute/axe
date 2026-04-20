# 🌫️ Murky Areas
> Points that are **neither wrong nor right** — ambiguous, lacking clear definition,
> making it hard for teams to make consistent decisions during implementation.
>
> 🇻🇳 [Phiên bản tiếng Việt](vi/03_murky_areas.md)

---

### 📌 Current Status (v0.5.1)

> **All 7 murky areas have been clarified and implemented.**
> **Security clarifications (v0.5.1)**: Redis URL parsing, plugin semver, outbox SQL safety resolved.
>
> | # | Murky Area | Decision |
> |---|---|---|
> | 1 | "No Magic" definition | ✅ Decision Matrix in `architecture_contract.md` |
> | 2 | Service vs Handler | ✅ Responsibility table applied |
> | 3 | Ent + sqlc | ✅ Shared `*sql.DB` pool |
> | 4 | Config management | ✅ **Cleanenv** (`config/config.go`) |
> | 5 | Testing strategy | ✅ Multi-DB matrix CI (Postgres + MySQL + SQLite) |
> | 6 | Observability | ✅ `pkg/logger` (slog) + `pkg/metrics` (Prometheus) |
> | 7 | Auth model | ✅ JWT + RBAC (`pkg/jwtauth` + `middleware/auth.go`) |
>
> **Security clarifications in v0.5.1:**
> - ✅ Redis URL parsing → `url.Parse()` handles auth with special characters
> - ✅ Plugin semver → fail-closed on malformed versions (reject, don't skip)
> - ✅ Outbox SQL → parameterized queries only (no string interpolation)
> - ✅ JWT auth model → `typ` + `aud` claims, 32-byte minimum secret, `New()` returns error

---

## 1. "No Magic" — Not Yet Operationalizable

**Problem:**
Both reports use "no magic" / "explicit" extensively, but:
- Who decides what counts as "magic"?
- Is `go generate` magic?
- Is Ent codegen magic?
- Are struct tags (`json:"name"`) magic?

**Clarification needed:**

```
"No Magic" Decision Matrix:

✅ ALLOWED (Compile-time, inspectable, generates static code):
  - Struct tags (json, db, validate)
  - go generate + Ent + sqlc + Wire
  - Interface satisfaction (implicit, but compiler-verified)
  - Build constraints

❌ FORBIDDEN (Runtime, opaque, hides control flow):
  - reflect.ValueOf / reflect.TypeOf in runtime hot paths
  - init() functions with complex side effects
  - Global var mutation after startup
  - Monkey patching
  - Plugin system with dynamic loading
```

---

## 2. Service vs Handler Boundary — Unclear

**Problem:**
Report 1 says:
> Handler: "parse HTTP request, call service layer"
> Service: "apply business algorithms"

But doesn't define:
- Does validation belong in Handler or Service?
- Does authorization check (RBAC) belong in Middleware, Handler, or Service?
- Where does DTO conversion (HTTP request → domain struct) happen?
- Does pagination logic belong in Handler, Service, or Repository?

**Clarification needed:**

| Concern | Layer | Reason |
|---|---|---|
| Input parsing (JSON → struct) | Handler | Depends on HTTP protocol |
| Input validation (format, required) | Handler | Returns 400 before entering business |
| Business validation (email exists?) | Service | Needs DB access |
| Authorization (user has permission?) | Middleware + Service | Middleware checks token, Service checks ownership |
| DTO → Domain Entity mapping | Service | Isolates handler from domain changes |
| Pagination params | Handler | Parsed from query string |
| Pagination logic | Repository | SQL LIMIT/OFFSET |

---

## 3. Ent vs sqlc — Coexistence Not Defined

**Problem:**
Report 1 concludes: "use Ent primarily, sqlc for analytics". Report 2 agrees but provides no implementation guide.

**Specific ambiguities:**
- Do these two tools use **2 separate connection pools** or share one?
- Is migration managed by Ent Atlas or separate SQL files?
- When Ent schema changes, do sqlc queries auto-detect?
- Test setup: how to mock Ent client and sqlc queries?

**Clarification:**
```
Architecture:
  ┌──────────────────────────────────────────┐
  │           cmd/api/main.go                │
  │  db := sql.Open(...)                     │
  │  entClient := ent.NewClient(ent.Driver(db))  │
  │  queries := sqlc.New(db)                 │
  │  (same *sql.DB, 2 client wrappers)       │
  └──────────────────────────────────────────┘
```
→ **Shared single `*sql.DB` connection pool**, two client layers on top.

---

## 4. Configuration Management — Multiple Choices, No Decision

**Problem:**
Report 1 proposes "Viper or Cleanenv" — but doesn't choose.

| | Viper | Cleanenv |
|---|---|---|
| Config file support | ✅ YAML, TOML, JSON, HCL | ❌ Only `.env` and env vars |
| Struct binding | ✅ | ✅ |
| Hot reload | ✅ | ❌ |
| Dependency size | Large (cobra, etc.) | Small |
| "No config file" cloud-native | ❌ May use files | ✅ Pure env vars |

**Decision:** If targeting cloud-native/12-Factor → **Cleanenv**. If needing multi-environment config files → **Viper**. Must choose one.

---

## 5. Testing Strategy — Missing Layers

**Problem:**
Report 1 says "unit testing is easier than Rails" via httptest and mock DB. True but insufficient.

**Clarification needed:**

```
Testing Pyramid for axe:

Layer 4: E2E (optional, smoke only)
Layer 3: Integration — testcontainers-go + real PostgreSQL
Layer 2: Service unit — mock Repository (interface)
Layer 1: Handler unit — httptest + mock Service (interface)
Layer 0: Domain unit — pure functions, no mock needed
```

---

## 6. Observability — Completely Unmentioned

**Problem:**
Neither report substantially addresses:
- Logging format (JSON structured logs?)
- Metrics (Prometheus? OpenTelemetry?)
- Tracing (distributed tracing for microservices?)
- Health check endpoints

**Clarification — context-aware structured logging:**
```go
func (s *OrderService) CreateOrder(ctx context.Context, ...) error {
    logger := LoggerFromCtx(ctx). // from context, has request_id
        With("order_id", order.ID)
    logger.Info("creating order")
    ...
}
```

---

## 7. Authentication / Authorization Model — Completely Murky

**Problem:**
Report 1 mentions "JWT middleware" but doesn't define:
- JWT or Session? Why?
- RBAC or ABAC or Policy-based?
- Token refresh strategy?
- Multi-tenant support?
- Where does permission check happen (middleware vs service)?

---

## Summary

```
✅ "No Magic" definition        → Decision Matrix in architecture_contract.md
✅ Service vs Handler boundary  → Responsibility table applied
✅ Ent + sqlc coexistence       → Shared *sql.DB pool, 2 client wrappers
✅ Config management            → Cleanenv chosen (12-Factor, env vars only)
✅ Testing strategy             → Multi-DB CI matrix + testcontainers
✅ Observability                → slog (JSON) + Prometheus /metrics
✅ Auth model                   → JWT access/refresh + RBAC middleware
```

> This document preserves original content as **historical context** for architecture decisions.
