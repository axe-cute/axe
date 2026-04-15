# Epic 8 — Plugin System & Ecosystem

**Goal**: Axe Plugin System cho phép community tạo và share integrations (Stripe, SendGrid, S3, Slack...) mà không cần fork core framework.

**Business Value**: Tạo **ecosystem** — điều làm Laravel/Rails hùng mạnh. Plugins = composable, versioned, tested integrations.

**Status**: `planned`

**Priority**: P2

---

## Stories

### Story 8.1 — Plugin Interface
**Sprint**: 19 | **Priority**: P2

**Goal**: Design `Plugin` interface cho phép third-party code integrate vào axe lifecycle.

**Acceptance Criteria**:
- [ ] `pkg/plugin/plugin.go` — interface `Plugin` với `Name()`, `Register(app)`, `Shutdown(ctx)`
- [ ] `App` struct trong `cmd/api/` expose registration point
- [ ] Plugins nhận access vào: router, config, logger, db, cache
- [ ] Plugin order preserved (dependency ordering)
- [ ] `app.Use(plugin)` để register

**Plugin interface**:
```go
type Plugin interface {
    Name() string
    Register(ctx context.Context, app *App) error
    Shutdown(ctx context.Context) error
}

// Usage in main.go:
app.Use(stripe.New(cfg.StripeKey))
app.Use(sendgrid.New(cfg.SendGridKey))
app.Use(s3.New(cfg.S3Bucket))
```

### Story 8.2 — `axe-plugin-storage` (S3/MinIO)
**Sprint**: 19 | **Priority**: P2

**Goal**: File upload plugin với S3/MinIO backend.

**Acceptance Criteria**:
- [ ] `go get github.com/axe-cute/axe-plugin-storage`
- [ ] `StoragePlugin.Upload(ctx, file)` → URL
- [ ] `StoragePlugin.Delete(ctx, key)` → error
- [ ] Local filesystem adapter (dev mode — không cần S3)
- [ ] Presigned URL generation
- [ ] Chi route: `POST /upload` → returns `{url: "..."}`
- [ ] Metrics: `storage_upload_bytes_total`, `storage_upload_errors_total`

### Story 8.3 — `axe-plugin-email` (SendGrid/SMTP)
**Sprint**: 20 | **Priority**: P2

**Goal**: Email sending plugin với template support.

**Acceptance Criteria**:
- [ ] `go get github.com/axe-cute/axe-plugin-email`
- [ ] `EmailPlugin.Send(ctx, to, templateID, data)` → error
- [ ] SendGrid adapter + SMTP fallback
- [ ] HTML templates từ `email/templates/*.html` (go:embed)
- [ ] Queue emails qua Asynq (async send)
- [ ] Dev mode: log emails thay vì gửi (MailHog compatible)

### Story 8.4 — Multi-Tenancy Foundation
**Sprint**: 20 | **Priority**: P2

**Goal**: Built-in multi-tenant support với tenant isolation.

**Acceptance Criteria**:
- [ ] Tenant identification: subdomain | JWT claim | header
- [ ] `middleware.Tenant()` → extract tenant từ request, set vào context
- [ ] `tenant.FromCtx(ctx)` → get current tenant
- [ ] Per-tenant Redis key namespacing (`{tenant}:cache:key`)
- [ ] Per-tenant rate limits (separate Redis counters)
- [ ] **Schema-per-tenant** (PostgreSQL): `SET search_path = tenant_{id}`
- [ ] `axe generate resource Post --multi-tenant` → thêm `TenantID` field tự động

### Story 8.5 — Plugin Registry
**Sprint**: 21 | **Priority**: P3

**Goal**: `axe plugin list` và plugin discovery.

**Acceptance Criteria**:
- [ ] `axe plugin list` → list official + community plugins từ registry API
- [ ] `axe plugin add axe-plugin-storage` → `go get` + update `main.go`
- [ ] Registry: GitHub repo + GitHub Releases API
- [ ] Plugin quality badges: official, community, deprecated

---

## Official Plugin Roadmap

| Plugin | Status | Priority |
|---|---|---|
| `axe-plugin-storage` (S3/MinIO) | planned | P2 |
| `axe-plugin-email` (SendGrid/SMTP) | planned | P2 |
| `axe-plugin-payment` (Stripe) | planned | P2 |
| `axe-plugin-search` (Elasticsearch/Typesense) | planned | P3 |
| `axe-plugin-push` (FCM/APNs) | planned | P3 |
| `axe-plugin-analytics` (PostHog) | planned | P3 |
| `axe-plugin-feature-flags` (Unleash) | planned | P3 |

---

## Technical Design

```
Plugin lifecycle:
  app.Use(plugin)
    ↓ Register(ctx, app)  ← inject dependencies
    ↓ Route registration  ← add routes to Chi router
    ↓ ...app running...
    ↓ Shutdown(ctx)       ← graceful cleanup

Separate repositories:
  github.com/axe-cute/axe              ← core framework
  github.com/axe-cute/axe-plugin-storage
  github.com/axe-cute/axe-plugin-email
  github.com/axe-cute/axe-plugin-payment
```
