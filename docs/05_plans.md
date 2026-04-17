# 📋 Kế Hoạch Xây Dựng (Build Plans)
> Kế hoạch 3 giai đoạn để hiện thực hóa nền tảng axe —
> Go web framework "không ma thuật", production-grade, adoptable.

---

## Tầm Nhìn

**axe** là một Go web framework nội bộ (internal platform) cung cấp:
- Kiến trúc Clean Architecture baked-in
- CLI generator giúp tạo CRUD endpoint trong < 10 phút
- Zero runtime magic, full compile-time safety
- Production-grade từ ngày đầu (transaction, observability, error handling)

---

## Phase 1: Foundation (4–6 tuần)
> Mục tiêu: "Hello World" production-grade hoạt động được

### 1.1 Project Scaffold

```
axe/
├── cmd/
│   └── api/
│       └── main.go          # Composition Root
├── internal/
│   ├── domain/              # Entities + Interfaces ONLY
│   ├── handler/             # HTTP layer
│   ├── service/             # Business logic
│   └── repository/          # Data access
├── pkg/
│   ├── apperror/            # Error taxonomy
│   ├── txmanager/           # Transaction manager
│   ├── logger/              # Structured logging
│   └── validator/           # Input validation
├── db/
│   └── migrations/          # SQL migration files
├── config/
│   └── config.go            # Cleanenv struct
└── ent/
    └── schema/              # Ent schema definitions
```

### 1.2 Core Infrastructure Packages

**Priority order:**

| # | Package | Lý do ưu tiên |
|---|---|---|
| 1 | `pkg/apperror` | Mọi layer cần ngay |
| 2 | `pkg/txmanager` | Critical, không thể thiếu |
| 3 | `pkg/logger` | Request ID propagation cần từ đầu |
| 4 | `config/` | Cleanenv setup |
| 5 | Chi Router + middleware chain | HTTP layer |
| 6 | Ent schema setup | Data layer |
| 7 | sqlc setup | Read queries |
| 8 | pgx connection pool | Database foundation |

### 1.3 First Domain: User (Example)

Implement toàn bộ flow cho 1 domain — User — để validate kiến trúc:
```
POST   /api/v1/users          → CreateUser
GET    /api/v1/users/:id      → GetUser
PUT    /api/v1/users/:id      → UpdateUser
DELETE /api/v1/users/:id      → DeleteUser
GET    /api/v1/users          → ListUsers (sqlc, pagination)
```

### 1.4 Deliverables Phase 1

- [ ] Project structure hoạt động được
- [ ] Error taxonomy `pkg/apperror` hoàn chỉnh
- [ ] Transaction manager `pkg/txmanager` tested
- [ ] Full User domain (CRUD) làm reference implementation
- [ ] Health check endpoint (`/health`, `/ready`)
- [ ] Structured JSON logging với request ID
- [ ] Makefile với `make run`, `make test`, `make migrate`
- [ ] Docker Compose: app + PostgreSQL + Redis

---

## Phase 2: Developer Experience (3–4 tuần)
> Mục tiêu: Dev mới có thể tạo feature trong 1 ngày

### 2.1 axe CLI Generator

```bash
# Generate full CRUD resource
axe generate resource Post \
  --fields="title:string,content:text,author_id:uuid,published:bool" \
  --belongs-to="User"

# Output:
# ✅ internal/domain/post.go           (entity + interface)
# ✅ internal/handler/post_handler.go  (HTTP handlers)
# ✅ internal/service/post_service.go  (business logic)
# ✅ internal/repository/post_repo.go  (DB queries)
# ✅ ent/schema/post.go               (Ent schema)
# ✅ db/migrations/YYYYMMDD_create_posts.sql
# ✅ internal/handler/post_handler_test.go
# ✅ internal/service/post_service_test.go
```

### 2.2 Developer Experience Contract

Đo và enforce các SLAs sau:

| Metric | Target |
|---|---|
| Time to create CRUD endpoint | ≤ 10 phút |
| Time to run tests | ≤ 30 giây |
| Time to understand a handler | ≤ 5 phút (linear readable) |
| Onboarding time (new dev) | ≤ 1 ngày |

### 2.3 Reference Documentation

- [ ] `docs/architecture_contract.md` — Rules, decisions, rationale
- [ ] `docs/data_consistency.md` — Transaction + Outbox patterns
- [ ] `docs/dev_experience_spec.md` — Generator guide, DX SLAs
- [ ] `docs/adr/` — Architecture Decision Records (ADRs)
- [ ] Working Postman/Bruno collection

---

## Phase 3: Production Hardening (3–4 tuần)
> Mục tiêu: Ready để deploy real workload

### 3.1 Observability Stack

```go
// Structured logging (slog với JSON output)
// Metrics (Prometheus /metrics endpoint)
// Distributed tracing (OpenTelemetry)
// Health checks (/health, /ready, /live)
```

### 3.2 Caching Layer

```
Redis integration:
  - Cache-aside pattern trong Service layer
  - Cache invalidation strategy per domain
  - Cache miss → DB fallback
  - TTL configurable per resource
```

### 3.3 Authentication Module

```
JWT authentication:
  - Access token (15 phút)
  - Refresh token (7 ngày, stored in DB)
  - RBAC với permission table
  - Middleware: ExtractToken → ValidateToken → InjectUser
```

### 3.4 Background Jobs

```
Asynq setup:
  - Outbox poller (Outbox → Queue publish)
  - Retry policy: exponential backoff
  - Dead letter queue
  - Admin UI: Asynqmon
```

### 3.5 Deployment

```
Docker:
  - Multi-stage build (builder + minimal final image)
  - < 20MB final image size

Kubernetes (optional):
  - Deployment + Service + ConfigMap
  - HPA (Horizontal Pod Autoscaler)
  - Readiness/Liveness probes

CI/CD (GitHub Actions):
  - go test ./...
  - go vet + staticcheck
  - Docker build + push
  - Migration run on deploy
```

---

## Phase 4: Plugin Ecosystem (3–4 tuần)
> Mục tiêu: Plugins dễ dùng, plug-and-play

### 4.1 Plugin System (Sprint 19) ✅

```go
// Plugin interface
type Plugin interface {
    Name() string
    Register(ctx context.Context, app *App) error
    Shutdown(ctx context.Context) error
}

// Typed service locator
plugin.Provide[MyService](app, "my-service", svc)
svc := plugin.MustResolve[MyService](app, "my-service")
```

### 4.2 Storage Plugin Integration (Sprint 20) 🔄

**Hai cách sử dụng:**

```bash
# Cách 1: Include khi tạo project
axe new blog-api --with-storage

# Cách 2: Thêm vào project có sẵn
axe plugin add storage
```

**Tự động tạo:**
- `pkg/storage/` — Store interface, FSStore, HTTP handler, Prometheus metrics
- Config fields trong `config/config.go`
- Env vars trong `.env.example`
- Route wiring trong `cmd/api/main.go`

**Thiết kế:**
- Standalone package (không phụ thuộc framework `plugin.App`)
- Direct chi handler mount → đơn giản hơn cho end users
- POSIX filesystem → hoạt động giống nhau trên local & JuiceFS

### 4.3 Email Plugin (Sprint 21) 🟡

```
pkg/plugin/email/:
  - SendGrid + SMTP backends
  - Asynq queue integration (async send)
  - Development mode: log to console
  - Template support (html/template)
```

### 4.4 Multi-Tenancy (Sprint 21) 🟡

```
Middleware approach:
  - Tenant ID from JWT claims / subdomain / header
  - Row-level security via Ent runtime hooks
  - Per-tenant rate limiting
```

### 4.5 Plugin Registry CLI (Sprint 22) 🟡

```bash
axe plugin list              # Show available plugins
axe plugin add storage       # Add storage to project
axe plugin add email         # Add email to project
axe plugin remove storage    # Remove plugin
```

### 4.6 Deliverables Phase 4

- [x] Plugin system (`plugin.Plugin` interface + typed service locator)
- [x] Storage plugin (FSStore, HTTP handler, Prometheus metrics)
- [ ] Storage easy integration (`--with-storage` + `axe plugin add storage`)
- [ ] Email plugin
- [ ] Multi-tenancy middleware
- [ ] Plugin Registry CLI

---

## Milestone Timeline

```
Week 1-2:   pkg/apperror + pkg/txmanager + config  [Phase 1]
Week 3-4:   Chi setup + User domain CRUD           [Phase 1]
Week 5-6:   Docker Compose + Makefile + tests       [Phase 1]
Week 7-8:   axe CLI generator v1                    [Phase 2]
Week 9-10:  Documentation + ADRs + DX polish        [Phase 2]
Week 11-12: Observability + Auth + Cache             [Phase 3]
Week 13-14: Background jobs + Deployment pipeline    [Phase 3]
Week 15-16: Plugin system + Storage plugin           [Phase 4]
Week 17-18: Storage integration + Email plugin       [Phase 4]
Week 19-20: Multi-tenancy + Plugin Registry CLI      [Phase 4]
```

---

## Rủi Ro và Mitigation


| Rủi ro | Khả năng | Mitigation |
|---|---|---|
| Ent codegen chậm với schema lớn | Medium | Pre-generate trong CI, cache artifact |
| Boilerplate quá nhiều → team bỏ | High | axe CLI generator là critical path |
| Transaction manager phức tạp | Medium | Reference implementation + tests rõ ràng |
| Dev quen Rails không adopt | High | Pair programming session + DX metric tracking |
| pgx migration from database/sql | Low | pgx tương thích ngược `database/sql` interface |

---

## Definition of Done (cho mỗi Domain)

```markdown
□ Entity defined trong internal/domain/
□ Interface defined (Repository + Service)
□ Service implemented với TxManager khi cần
□ Repository implemented (Ent cho writes, sqlc cho reads)
□ Handler implemented (Chi router, validation, error mapping)
□ Unit tests: Handler (httptest), Service (mock repo)
□ Integration test (testcontainers-go)
□ Migration file SQL
□ Postman/Bruno test cases
□ ADR nếu có architectural decision mới
```
