# Epic 8 — Plugin System & Ecosystem

**Goal**: Axe Plugin System cho phép community tạo và share integrations (Stripe, SendGrid, S3, Slack...) mà không cần fork core framework.

**Business Value**: Tạo **ecosystem** — điều làm Laravel/Rails hùng mạnh. Plugins = composable, versioned, tested integrations.

**Status**: 🔄 In Progress (Stories 8.1–8.2 done ✅)

**Priority**: P2

> ⚠️ Source of truth cho status: `sprint-status.yaml`

---

## Architecture Decision: Monorepo (ADR-009)

Tất cả official plugins nằm trong `pkg/plugin/` (monorepo approach). Lý do:
- **Ship fast**: 1 PR sửa interface + tất cả plugins cùng lúc
- **1 CI pipeline**: không lo version matrix
- **Split later**: khi dependency bloat thực sự gây vấn đề, tách plugin có heavy SDK ra repo riêng

---

## Stories

### Story 8.1 — Plugin Interface
**Sprint**: 19 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Design `Plugin` interface cho phép third-party code integrate vào axe lifecycle.

**Acceptance Criteria**:
- [x] `pkg/plugin/plugin.go` — interface `Plugin` với `Name()`, `Register(app)`, `Shutdown(ctx)`
- [x] `App` struct expose registration point
- [x] Plugins nhận access vào: router, config, logger, db, cache
- [x] Plugin order preserved (FIFO register, LIFO shutdown)
- [x] `app.Use(plugin)` để register
- [x] Typed service locator: `Provide[T]()`, `Resolve[T]()`, `MustResolve[T]()`
- [x] Error rollback: nếu plugin N fail Register, shutdown plugins 1..N-1

**Plugin interface**:
```go
type Plugin interface {
    Name() string
    Register(ctx context.Context, app *App) error
    Shutdown(ctx context.Context) error
}

// Usage in main.go:
app.Use(storage.New(storageCfg))
app.Use(email.New(emailCfg))
app.Start(ctx)
```

### Story 8.2 — `axe-plugin-storage` (FSStore)
**Sprint**: 19 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: File upload plugin với POSIX filesystem backend (local dev + JuiceFS production).

**Acceptance Criteria**:
- [x] `pkg/plugin/storage/` — FSStore adapter, zero external deps (standard `os` pkg)
- [x] Works identically on local dirs and JuiceFS POSIX mount
- [x] Chi routes: `POST /upload` (multipart), `GET /upload/{key}` (serve), `DELETE /upload/{key}` (remove)
- [x] Prometheus: `axe_storage_upload_bytes_total`, `axe_storage_operations_total`, `axe_storage_upload_errors_total`
- [x] Config: `STORAGE_BACKEND`, `STORAGE_MOUNT_PATH`, `STORAGE_MAX_FILE_SIZE`, `STORAGE_URL_PREFIX`

### Story 8.3 — `axe-plugin-email` (SendGrid/SMTP)
**Sprint**: 20 | **Priority**: P2 | **Status**: Planned

**Goal**: Email sending plugin với template support.

**Acceptance Criteria**:
- [ ] `pkg/plugin/email/` — EmailPlugin implementing `plugin.Plugin`
- [ ] `EmailPlugin.Send(ctx, to, templateID, data)` → error
- [ ] SendGrid adapter + SMTP fallback
- [ ] HTML templates từ `email/templates/*.html` (go:embed)
- [ ] Queue emails qua Asynq (async send)
- [ ] Dev mode: log emails thay vì gửi (MailHog compatible)

### Story 8.4 — Multi-Tenancy Middleware (v1.0 scope)
**Sprint**: 21 | **Priority**: P2 | **Status**: Planned

**Goal**: Tenant context middleware — lightweight multi-tenancy cho v1.0.

> **Scope giảm so với plan ban đầu**: v1.0 chỉ làm middleware + Redis namespace. Schema-per-tenant defer sang v2.0 (quá phức tạp, cần real user demand).

**v1.0 Acceptance Criteria**:
- [ ] Tenant identification: subdomain | JWT claim | header `X-Tenant-ID`
- [ ] `middleware.Tenant()` → extract tenant từ request, set vào context
- [ ] `tenant.FromCtx(ctx)` → get current tenant
- [ ] Per-tenant Redis key namespacing (`{tenant}:cache:key`)
- [ ] Per-tenant rate limits (separate Redis counters)
- [ ] `axe generate resource Post --multi-tenant` → thêm `TenantID` field tự động

**Deferred to v2.0**:
- Schema-per-tenant (`SET search_path = tenant_{id}`)
- Tenant provisioning (auto-create schema on onboard)
- Cross-tenant migration runner

### Story 8.5 — Plugin Discovery CLI
**Sprint**: 22 | **Priority**: P3 | **Status**: Planned

**Goal**: `axe plugin list` liệt kê available plugins.

> **Scope note**: Vì plugins nằm trong monorepo, story này đơn giản hơn plan ban đầu — chỉ cần hardcoded list, không cần GitHub API discovery.

**Acceptance Criteria**:
- [ ] `axe plugin list` → list official plugins với status (installed/available)
- [ ] `axe plugin add email` → add import + `app.Use()` vào main.go (code mod)
- [ ] Plugin quality badges: official, community, deprecated

---

## Official Plugin Roadmap

| Plugin | Location | Status | Priority |
|---|---|---|---|
| `storage` (FSStore/JuiceFS) | `pkg/plugin/storage/` | ✅ Done | P2 |
| `email` (SendGrid/SMTP) | `pkg/plugin/email/` | Planned | P2 |
| `payment` (Stripe) | `pkg/plugin/payment/` | Planned | P2 |
| `search` (Elasticsearch/Typesense) | `pkg/plugin/search/` | Planned | P3 |
| `push` (FCM/APNs) | `pkg/plugin/push/` | Planned | P3 |
| `analytics` (PostHog) | `pkg/plugin/analytics/` | Planned | P3 |

---

## Technical Design

```
Plugin lifecycle:
  app.Use(plugin)
    ↓ Register(ctx, app)  ← inject dependencies, add routes
    ↓ ...app running...
    ↓ Shutdown(ctx)       ← graceful cleanup (LIFO order)

Monorepo layout:
  pkg/plugin/
    plugin.go             ← Plugin interface + App host + service locator
    storage/              ← FSStore (zero external deps)
    email/                ← SendGrid + SMTP
    payment/              ← Stripe
```

---

## Risks
- **Dependency bloat**: khi thêm email (SendGrid SDK) + payment (Stripe SDK), `go get axe` sẽ nặng hơn → monitor, tách repo nếu cần
- Template drift: email templates có thể outdated → versioning cần thiết
