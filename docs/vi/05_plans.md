# 📋 Kế Hoạch Xây Dựng (Build Plans)
> Kế hoạch 4 giai đoạn để hiện thực hóa nền tảng axe —
> Go web framework "không ma thuật", production-grade, adoptable.
>
> 🇬🇧 [English version](../05_plans.md)

---

## Tầm Nhìn

**axe** là một Go web framework cung cấp:
- Kiến trúc Clean Architecture baked-in
- CLI generator giúp tạo CRUD endpoint trong < 10 phút
- Zero runtime magic, full compile-time safety
- Production-grade từ ngày đầu (transaction, observability, error handling)
- Plugin system mở rộng (storage, email, payments…)

---

## Phase 1: Foundation (4–6 tuần) ✅
> Mục tiêu: "Hello World" production-grade hoạt động được

### Deliverables

- [x] Project structure (`cmd/api`, `internal/`, `pkg/`, `ent/`, `config/`)
- [x] Error taxonomy `pkg/apperror` (NotFound, Forbidden, Conflict…)
- [x] Transaction manager `pkg/txmanager` (Unit of Work)
- [x] Full User domain (CRUD) làm reference implementation
- [x] Health check endpoints (`/health`, `/ready`)
- [x] Structured JSON logging (slog) với request ID
- [x] Makefile (`make run`, `make test`, `make migrate`, `make setup`)
- [x] Docker Compose: PostgreSQL + Redis + Asynqmon

---

## Phase 2: Developer Experience (3–4 tuần) ✅
> Mục tiêu: Dev mới có thể tạo feature trong 1 ngày

### axe CLI

```bash
# Scaffold dự án mới
axe new blog-api --module=github.com/you/blog-api
axe new lite --db=sqlite --no-worker --no-cache
axe new media --with-storage

# Generate CRUD resource (10 files)
axe generate resource Post --fields="title:string,body:text,published:bool"
axe generate resource Comment --fields="body:text" --belongs-to=Post
axe generate resource Order --fields="amount:float" --with-auth

# Thêm plugin vào dự án có sẵn
axe plugin add storage

# Migration
axe migrate up / down / status
```

### DX SLAs

| Metric | Target | Thực tế |
|---|---|---|
| Tạo CRUD endpoint | ≤ 10 phút | ✅ ~2 phút |
| Chạy tests | ≤ 30 giây | ✅ ~15 giây |
| Hiểu 1 handler | ≤ 5 phút | ✅ Linear readable |
| Onboarding dev mới | ≤ 1 ngày | ✅ |

### Documentation

- [x] `docs/architecture_contract.md` — Rules, decisions, rationale
- [x] `docs/data_consistency.md` — Transaction + Outbox patterns
- [x] `docs/dev_experience_spec.md` — Generator guide, DX SLAs
- [x] `docs/adr/` — Architecture Decision Records
- [x] Postman collection

---

## Phase 3: Production Hardening (3–4 tuần) ✅
> Mục tiêu: Ready để deploy real workload

### Đã hoàn thành

- [x] **Observability**: Prometheus `/metrics`, slog JSON, OpenTelemetry-ready
- [x] **Cache**: Redis cache-aside pattern (`pkg/cache`)
- [x] **Auth**: JWT access/refresh + Redis blocklist + RBAC middleware (`pkg/jwtauth`)
- [x] **Rate Limiting**: Redis sliding-window (`pkg/ratelimit`)
- [x] **Background Jobs**: Asynq + Outbox poller (`pkg/worker`, `pkg/outbox`)
- [x] **Multi-DB**: PostgreSQL (pgx v5), MySQL, SQLite (`pkg/db/`)
- [x] **WebSocket**: Hub/Client/Room + Redis Pub/Sub adapter (`pkg/ws/`)
- [x] **Deployment**: Multi-stage Docker, GitHub Actions CI

---

## Phase 4: Plugin Ecosystem (3–4 tuần) 🔄
> Mục tiêu: Plugins dễ dùng, plug-and-play

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

### 4.2 Storage Plugin (Sprint 19–21) ✅

- FSStore (POSIX filesystem — local + JuiceFS)
- HTTP handler (upload/serve/delete)
- Prometheus metrics
- `axe new --with-storage` + `axe plugin add storage`
- **Sprint 21 hardening**: `f.Sync()` flush FUSE buffers, `FSStore.HealthCheck()` write→read→delete sentinel, `wrapFSError()` map `ENOTCONN/EROFS/ENOSPC` → human-readable errors
- **Tài liệu**: `docs/guides/juicefs-storage.md` — hướng dẫn tích hợp JuiceFS (mới)

### 4.3 Upcoming

| Plugin | Sprint | Status |
|---|---|---|
| Email (SendGrid/SMTP) | 22 | 🟡 Planned |
| Multi-tenancy middleware | 23 | 🟡 Planned |
| Plugin Registry CLI | 24 | 🟡 Planned |

---

## Performance (Benchmarks)

So sánh với các framework phổ biến — Apple M1, Go 1.25:

| Scenario | axe (Chi) | Gin | Echo | Fiber |
|---|---|---|---|---|
| Static JSON | **583 ns** 🏆 | 704 ns | 792 ns | 4,158 ns |
| URL Params | **731 ns** 🏆 | 763 ns | 760 ns | 4,381 ns |
| Middleware | **1,014 ns** 🏆 | 1,961 ns | 1,980 ns | 7,458 ns |
| JSON Parse | 2,909 ns | 2,914 ns | 2,883 ns | 10,992 ns |
| Multi-Route | 1,443 ns | 747 ns | 626 ns | 4,269 ns |

→ [Chi tiết benchmarks](../benchmarks/)

---

## Rủi Ro và Mitigation

| Rủi ro | Khả năng | Mitigation |
|---|---|---|
| Ent codegen chậm với schema lớn | Medium | Pre-generate trong CI, cache artifact |
| Boilerplate quá nhiều → team bỏ | High | axe CLI generator là critical path ✅ |
| Transaction manager phức tạp | Medium | Reference implementation + tests ✅ |
| Dev quen Rails không adopt | High | DX SLAs + pair programming |
| pgx migration | Low | pgx tương thích ngược `database/sql` ✅ |

---

## Definition of Done (cho mỗi Domain)

```
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
