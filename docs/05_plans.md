# 📋 Build Plans
> 4-phase plan to build the axe platform —
> a Go web framework with "no magic", production-grade, adoptable.
>
> 🇻🇳 [Phiên bản tiếng Việt](vi/05_plans.md)

---

## Vision

**axe** is an internal Go web framework platform providing:
- Clean Architecture baked-in
- CLI generator for CRUD endpoints in < 10 minutes
- Zero runtime magic, full compile-time safety
- Production-grade from day one (transactions, observability, error handling)

---

## Phase 1: Foundation (4–6 weeks)
> Goal: Production-grade "Hello World" that works

### 1.1 Core Infrastructure Packages

| # | Package | Priority Reason |
|---|---|---|
| 1 | `pkg/apperror` | Every layer needs it immediately |
| 2 | `pkg/txmanager` | Critical, cannot be missing |
| 3 | `pkg/logger` | Request ID propagation needed from start |
| 4 | `config/` | Cleanenv setup |
| 5 | Chi Router + middleware chain | HTTP layer |
| 6 | Ent schema setup | Data layer |
| 7 | sqlc setup | Read queries |
| 8 | pgx connection pool | Database foundation |

### 1.2 First Domain: User (Reference Implementation)

Full flow for one domain — User — to validate architecture:
```
POST   /api/v1/users          → CreateUser
GET    /api/v1/users/:id      → GetUser
PUT    /api/v1/users/:id      → UpdateUser
DELETE /api/v1/users/:id      → DeleteUser
GET    /api/v1/users          → ListUsers (sqlc, pagination)
```

### 1.3 Phase 1 Deliverables

- [x] Working project structure
- [x] Error taxonomy `pkg/apperror`
- [x] Transaction manager `pkg/txmanager`
- [x] Full User domain (CRUD) as reference implementation
- [x] Health check endpoints (`/health`, `/ready`)
- [x] Structured JSON logging with request ID
- [x] Makefile with `make run`, `make test`, `make migrate`
- [x] Docker Compose: app + PostgreSQL + Redis

---

## Phase 2: Developer Experience (3–4 weeks)
> Goal: New dev can create a feature in one day

### 2.1 axe CLI Generator

```bash
axe generate resource Post \
  --fields="title:string,content:text,author_id:uuid,published:bool" \
  --belongs-to="User"

# Generates:
# ✅ internal/domain/post.go           (entity + interface)
# ✅ internal/handler/post_handler.go  (HTTP handlers)
# ✅ internal/service/post_service.go  (business logic)
# ✅ internal/repository/post_repo.go  (DB queries)
# ✅ ent/schema/post.go               (Ent schema)
# ✅ db/migrations/YYYYMMDD_create_posts.sql
# ✅ internal/handler/post_handler_test.go
# ✅ internal/service/post_service_test.go
```

### 2.2 Developer Experience SLAs

| Metric | Target |
|---|---|
| Time to create CRUD endpoint | ≤ 10 minutes |
| Time to run tests | ≤ 30 seconds |
| Time to understand a handler | ≤ 5 minutes (linear readable) |
| Onboarding time (new dev) | ≤ 1 day |

### 2.3 Phase 2 Deliverables

- [x] `docs/architecture_contract.md` — Rules, decisions, rationale
- [x] `docs/data_consistency.md` — Transaction + Outbox patterns
- [x] `docs/dev_experience_spec.md` — Generator guide, DX SLAs
- [x] `docs/adr/` — Architecture Decision Records
- [x] Working Postman collection

---

## Phase 3: Production Hardening (3–4 weeks)
> Goal: Ready to deploy real workloads

### 3.1 Observability

- Structured logging (slog with JSON output)
- Prometheus `/metrics` endpoint
- OpenTelemetry-ready tracing
- Health checks (`/health`, `/ready`)

### 3.2 Caching Layer

- Redis cache-aside pattern in Service layer
- Cache invalidation strategy per domain
- Cache miss → DB fallback
- TTL configurable per resource

### 3.3 Authentication Module

- JWT access tokens (15 min) + refresh tokens (7 days)
- RBAC with permission table
- Middleware: ExtractToken → ValidateToken → InjectUser

### 3.4 Background Jobs

- Asynq: Outbox poller → Queue publish
- Retry policy: exponential backoff
- Dead letter queue + Asynqmon dashboard

### 3.5 Deployment

- Multi-stage Docker build (< 20MB final image)
- GitHub Actions CI (test, vet, lint, Docker build)
- Migration auto-run on deploy

---

## Phase 4: Plugin Ecosystem (3–4 weeks)
> Goal: Plug-and-play extensibility

### 4.1 Plugin System (Sprint 19) ✅

```go
type Plugin interface {
    Name() string
    Register(ctx context.Context, app *App) error
    Shutdown(ctx context.Context) error
}

plugin.Provide[MyService](app, "my-service", svc)
svc := plugin.MustResolve[MyService](app, "my-service")
```

### 4.2 Storage Plugin Integration (Sprints 19–21) ✅

```bash
axe new blog-api                    # Create project first
axe plugin add storage              # Add storage plugin to existing project
```

Auto-creates: `pkg/storage/` (Store, FSStore, Handler, metrics), config fields, env vars, route wiring.
Security defaults: JWT auth on POST/DELETE, path traversal protection, CORS middleware.

**Sprint 21 hardening (stories 9.3–9.5)**:
- `f.Sync()` in `Upload()` — flushes FUSE buffers before returning (prevents silent data loss)
- `FSStore.HealthCheck()` — write→read→delete sentinel cycle (detects stale JuiceFS mounts)
- `wrapFSError()` — translates `ENOTCONN`, `EIO`, `EROFS`, `ENOSPC`, `EACCES` into human-readable errors
- `docs/guides/juicefs-storage.md` — step-by-step JuiceFS integration guide (new)
- `docs/adr/010-fsstore-posix-over-s3.md` — documents POSIX vs S3 SDK decision

### 4.3 Upcoming Plugins

| Plugin | Sprint | Status |
|---|---|---|
| Email (SendGrid/SMTP) | 22 | 🟡 Planned |
| Multi-tenancy middleware | 23 | 🟡 Planned |
| Plugin Registry CLI | 24 | 🟡 Planned |

---

## Milestone Timeline

```
Week 1-2:   pkg/apperror + pkg/txmanager + config     [Phase 1]  ✅
Week 3-4:   Chi setup + User domain CRUD              [Phase 1]  ✅
Week 5-6:   Docker Compose + Makefile + tests          [Phase 1]  ✅
Week 7-8:   axe CLI generator v1                       [Phase 2]  ✅
Week 9-10:  Documentation + ADRs + DX polish           [Phase 2]  ✅
Week 11-12: Observability + Auth + Cache               [Phase 3]  ✅
Week 13-14: Background jobs + Deployment pipeline      [Phase 3]  ✅
Week 15-16: Plugin system + Storage plugin             [Phase 4]  ✅
Week 17-18: Storage hardening + JuiceFS guide + ADR    [Phase 4]  ✅
Week 19-20: Email plugin + Test coverage               [Phase 4]  🔄
Week 21-22: Multi-tenancy + Plugin Registry CLI        [Phase 4]  🟡
```

---

## Risks & Mitigation

| Risk | Likelihood | Mitigation |
|---|---|---|
| Ent codegen slow with large schemas | Medium | Pre-generate in CI, cache artifacts |
| Too much boilerplate → team abandons | High | axe CLI generator is critical path |
| Transaction manager complexity | Medium | Reference implementation + clear tests |
| Rails-experienced devs resist adoption | High | Pair programming + DX metric tracking |
| pgx migration from database/sql | Low | pgx is backwards-compatible with `database/sql` interface |

---

## Definition of Done (per Domain)

```
□ Entity defined in internal/domain/
□ Interface defined (Repository + Service)
□ Service implemented with TxManager when needed
□ Repository implemented (Ent for writes, sqlc for reads)
□ Handler implemented (Chi router, validation, error mapping)
□ Unit tests: Handler (httptest), Service (mock repo)
□ Integration test (testcontainers-go)
□ SQL migration file
□ Postman/Bruno test cases
□ ADR if new architectural decision
```
