# Epic 8 — Plugin System & Ecosystem (Short-term)

**Goal**: Axe Plugin System cho phép community tạo và share integrations mà không cần fork core framework. Epic 8 tập trung vào **short-term plugins** — những gì mọi production backend đều cần.

**Business Value**: Tạo **ecosystem** — điều làm Laravel/Rails hùng mạnh. Plugins = composable, versioned, tested integrations.

**Status**: 🔄 In Progress (Stories 8.1–8.2, 8.6, 9.3–9.6 done ✅)

**Priority**: P2

**Scope**: Short-term (Sprint 19–24, v1.0). Long-term plugins → Epic 9.

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

### Story 8.6 — Storage Plugin Hardening (Security + Ops)
**Sprint**: 20 | **Priority**: P0 | **Status**: ✅ Done

**Goal**: Harden storage plugin cho production: authentication, security, observability.

> Completed 2026-04-17. All 6 gaps fixed, 18/18 tests PASS (-race).

**Acceptance Criteria**:
- [x] **Auth**: `POST /upload` + `DELETE /upload/*` yêu cầu JWT (GET serve public)
- [x] **Path traversal**: `safePath()` helper prevent `../../etc/passwd` attacks
- [x] **CORS**: `go-chi/cors` middleware configurable (support SPA browser upload)
- [x] **Health check**: `/ready` kiểm tra storage mount point (`os.Stat`)
- [x] **Metrics labels**: thêm `backend` label vào Prometheus counters (local vs juicefs)
- [x] **Tests**: unit tests cho path traversal, auth-protected routes, health check

**Files cần sửa**:
```
pkg/plugin/storage/fs_store.go    ← safePath() helper
pkg/plugin/storage/plugin.go      ← RequireAuth config + JWT middleware
pkg/plugin/storage/storage.go     ← Config.RequireAuth field
pkg/plugin/storage/metrics.go     ← backend label
pkg/plugin/storage/storage_test.go ← path traversal + auth tests
cmd/api/main.go                    ← CORS middleware, storage health check in /ready
config/config.go                   ← CORS config fields
```

### Story 8.6b — Storage Plugin Conformance (Reference Implementation)
**Sprint**: 21 | **Priority**: P1 | **Status**: 🟡 Planned

**Goal**: Update the storage plugin to fully conform to the 7-strategy plugin system. Storage becomes the **reference implementation** — every future plugin must match this pattern.

> **Why storage first**: Storage is the only existing plugin with real production usage. Making it the reference ensures all strategies are proven on real code before new plugins are written.

**Changes required per strategy**:

#### Strategy 1 — Correctness Gates
```go
// Layer 4 fix: move validation from Register() to New()
// Current: New() never returns error
func New(cfg Config) *Plugin { ... } // ❌ no validation

// Fixed:
func New(cfg Config) (*Plugin, error) {
    cfg.defaults()
    if cfg.MountPath == "" {
        return nil, errors.New("storage: MountPath is required")
    }
    return &Plugin{cfg: cfg}, nil
}
```
- Layer 5: `const ServiceKey = "storage"` already correct ✅
- Layer 6: Uses `app.Cache`, `app.Logger` — no self-created connections ✅

#### Strategy 3 — UI Extension (Contributor)
```go
// Add to pkg/plugin/storage/plugin.go
func (p *Plugin) AdminContribution() admin.Contribution {
    return admin.Contribution{
        ID:       "storage",
        NavLabel: "Storage",
        NavIcon:  "📦",
        APIRoute: "/storage/admin/stats",
    }
}

// New route: GET /storage/admin/stats → upload stats, recent files, mount health
app.Router.Get("/storage/admin/stats", h.handleAdminStats)
```

#### Strategy 4 — Event Bus
```go
// Publish events after upload and delete
func (h *handler) handleUpload(w http.ResponseWriter, r *http.Request) {
    result, err := h.store.Upload(ctx, key, r.Body, size, contentType)
    if err == nil {
        h.events.Publish(r.Context(), events.Event{
            Topic:   events.TopicStorageUploaded,
            Payload: map[string]any{"key": result.Key, "size": result.Size,
                "content_type": result.ContentType},
            Meta:    events.EventMeta{PluginSource: "storage"},
        })
    }
}
```

#### Strategy 5 — Observability (HealthChecker)
```go
// Add to pkg/plugin/storage/plugin.go
func (p *Plugin) HealthCheck(ctx context.Context) plugin.HealthStatus {
    start := time.Now()
    if err := p.store.HealthCheck(ctx); err != nil {
        return plugin.HealthStatus{
            Plugin:  "storage",
            OK:      false,
            Message: err.Error(),
            Latency: time.Since(start),
        }
    }
    return plugin.HealthStatus{
        Plugin:  "storage",
        OK:      true,
        Message: "mount accessible",
        Latency: time.Since(start),
    }
}
```

**Acceptance Criteria**:
- [ ] `New()` returns `(*Plugin, error)` — validates `MountPath` before `Register()`
- [ ] `Plugin` implements `admin.Contributor` → `AdminContribution()` returns storage nav panel
- [ ] `GET /storage/admin/stats` route registered (requires admin auth)
- [ ] `Plugin` implements `plugin.HealthChecker` → `HealthCheck()` tests mount point with write-verify
- [ ] Upload handler publishes `events.TopicStorageUploaded` after successful upload
- [ ] Delete handler publishes `events.TopicStorageDeleted` after successful delete
- [ ] All existing tests still PASS (`go test -race ./pkg/plugin/storage/...`)
- [ ] Plugin Authoring Guide references storage as the canonical example

**Files cần sửa**:
```
pkg/plugin/storage/storage.go      ← New() returns error
pkg/plugin/storage/plugin.go       ← Contributor + HealthChecker implementation
pkg/plugin/storage/handler.go      ← publish events after upload/delete
pkg/plugin/storage/storage_test.go ← test New() error, HealthCheck, event publish
```

### Story 8.3 — `axe-plugin-email` (SendGrid/SMTP)
**Sprint**: 22 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Email sending plugin với template support.

**Acceptance Criteria**:
- [ ] `pkg/plugin/email/` — EmailPlugin implementing `plugin.Plugin`
- [ ] `EmailPlugin.Send(ctx, to, templateID, data)` → error
- [ ] SendGrid adapter + SMTP fallback
- [ ] HTML templates từ `email/templates/*.html` (go:embed)
- [ ] Queue emails qua Asynq (async send)
- [ ] Dev mode: log emails thay vì gửi (MailHog compatible)

### Story 8.4 — Multi-Tenancy Middleware (v1.0 scope)
**Sprint**: 22 | **Priority**: P2 | **Status**: ✅ Done

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

### Story 8.5 — Plugin CLI (Unified)
**Sprint**: 23 | **Priority**: P3 | **Status**: ✅ Done

**Goal**: Unified `axe plugin` CLI covering discovery, scaffolding, injection, and validation. All plugin development workflow in one command group.

> **Scope**: Monorepo approach — hardcoded registry, no GitHub API needed. `axe plugin validate` consolidated here from Story 8.14.

**4 subcommands**:

```
axe plugin list                  → list all official plugins + installed status
axe plugin add <name>            → inject import + app.Use() into main.go
axe plugin new <name>            → scaffold new plugin from template
axe plugin validate              → check quality layers + version compat locally
```

**Acceptance Criteria**:
- [ ] `axe plugin list` → table: name, category, status (✅ installed / 🟡 available), sprint
- [ ] `axe plugin add email` → adds `"github.com/axe-cute/axe/pkg/plugin/email"` import + `app.Use(email.New(...))` to main.go via AST (not string replace)
- [ ] `axe plugin new myplugin` → scaffolds:
  ```
  pkg/plugin/myplugin/
    plugin.go      ← Plugin interface stubs + TODO comments
    config.go      ← Config struct + New() returning error
    metrics.go     ← obs.NewCounter() stubs with naming convention
    plugin_test.go ← MockApp test harness
  ```
- [ ] `axe plugin validate` → checks all 6 quality layers:
  1. Interface contract (Name/Register/Shutdown present)
  2. `New()` returns error
  3. `const ServiceKey` defined (if Provide is called)
  4. No `sql.Open` / `redis.NewClient` inside Register()
  5. Metrics follow `axe_{plugin}_{metric}_{unit}` naming
  6. `MinAxeVersion()` set if Events or admin APIs are used
- [ ] `axe plugin validate` → exit code 1 on any violation (CI-friendly)
- [ ] Plugin quality status in `list`: `official` | `community` | `deprecated`

### Story 8.7 — `axe-plugin-admin` (Admin UI)
**Sprint**: 24 | **Priority**: P3 | **Status**: ✅ Done

**Goal**: Embedded admin dashboard như một **optional plugin** với UI extension model — plugins khác có thể contribute nav panels và settings forms mà không cần coupling.

> **Design decision**: Admin UI là **plugin**, không phải built-in. Opt-in, no magic. Admin plugin defines extension points; other plugins implement them.

#### Plugin UI Extension Interfaces

```go
// pkg/plugin/admin/contrib.go — admin package owns these interfaces

// Contributor: plugin muốn có nav panel trong admin sidebar.
type Contributor interface {
    AdminContribution() Contribution
}

type Contribution struct {
    ID       string // unique: "ai-openai", "kafka", "storage"
    NavLabel string // "AI Assistant"
    NavIcon  string // "🤖"
    APIRoute string // "/ai/admin/chat" — route plugin tự đăng ký
}

// Configurable: plugin muốn có settings form trong Plugin Manager.
// Embed Contributor — phải có nav panel trước khi có config form.
type Configurable interface {
    Contributor
    AdminConfig() ConfigSchema          // JSON Schema → auto-render form
    // ApplyConfig nhận config đã validated — admin REST layer validate JSON Schema TRƯỚC
    // khi gọi ApplyConfig. Plugin vẫn phải validate lại (defense in depth).
    // Return admin.ErrInvalidConfig nếu field không hợp lệ.
    ApplyConfig(ctx context.Context, cfg map[string]any) error
}

// ErrInvalidConfig là typed error từ ApplyConfig.
type ErrInvalidConfig struct {
    Field  string
    Reason string
}

type ConfigSchema struct {
    Fields []ConfigField
}

type ConfigField struct {
    Key       string   // "model"
    Label     string   // "AI Model"
    Type      string   // "select"|"text"|"toggle"|"number"
    Options   []string // ["gpt-4o", "gpt-3.5-turbo"]
    Required  bool
    Sensitive bool     // mask in UI like password
}
```

#### Admin Plugin Discovery at Startup

```go
// Admin scans ALL registered plugins via AllPlugins() — zero coupling needed
// Note: uses app.AllPlugins() NOT the service locator (plugins are not auto-provided as services)
func (p *AdminPlugin) Register(ctx context.Context, app *plugin.App) error {
    for _, other := range app.AllPlugins() { // AllPlugins() returns []Plugin — Story 8.10 adds this
        if contrib, ok := other.(Contributor); ok {
            p.contributions = append(p.contributions, contrib.AdminContribution())
        }
        if checker, ok := other.(HealthChecker); ok {
            p.healthCheckers = append(p.healthCheckers, checker)
        }
    }
    // REST APIs for SPA
    app.Router.Get("/axe-admin/api/plugins", p.handleListPlugins)
    app.Router.Get("/axe-admin/api/nav",     p.handleNav)          // visible only
    app.Router.Put("/axe-admin/api/plugins/{id}/nav", p.handleToggleNav)
    app.Router.Get("/axe-admin/api/plugins/{id}/config-schema", p.handleConfigSchema)
    app.Router.Put("/axe-admin/api/plugins/{id}/config", p.handleApplyConfig)
    app.Router.Mount("/axe-admin", p.staticHandler()) // go:embed SPA
}
```

#### DB Schema (uses shared app.DB — Layer 6 rule)

```sql
-- Created by admin plugin at Register() if not exists
CREATE TABLE IF NOT EXISTS axe_admin_settings (
    plugin_id   TEXT PRIMARY KEY,
    nav_visible BOOLEAN NOT NULL DEFAULT true,
    config      JSONB,
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);
```

#### Plugin Manager UX

```
GET /axe-admin/plugins → Plugin Manager page

┌── Plugin Manager ──────────────────────────────────────┐
│                                                         │
│  ┌─ ai:openai ──────────── ✅ Active ─────────────────┐ │
│  │  AI Assistant · GPT-4o                             │ │
│  │  Nav panel: 🤖 AI Assistant   [👁 Hide] [Config]  │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                         │
│  ┌─ storage ────────────── ✅ Active ─────────────────┐ │
│  │  File Storage · FSStore/JuiceFS                    │ │
│  │  Nav panel: 📦 Storage Stats  [👁 Show] [Config]  │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                         │
│  ┌─ ratelimit ──────────── ✅ Active ─────────────────┐ │
│  │  Rate Limiting · Redis                             │ │
│  │  No admin panel                                    │ │
│  └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

**Acceptance Criteria**:
- [ ] `app.Use(admin.New(admin.Config{Path: "/axe-admin", Secret: "..."}))` → mount admin UI
- [ ] `Contributor` + `Configurable` interfaces exported từ `pkg/plugin/admin/contrib.go`
- [ ] Admin scans all registered plugins for `Contributor` at `Register()` time
- [ ] `GET /axe-admin/api/nav` → chỉ trả plugins có `nav_visible=true` trong DB
- [ ] `PUT /axe-admin/api/plugins/{id}/nav` → toggle nav visibility, persist vào `axe_admin_settings`
- [ ] `GET /axe-admin/api/plugins/{id}/config-schema` → JSON Schema nếu plugin là `Configurable`
- [ ] `PUT /axe-admin/api/plugins/{id}/config` → apply config via `ApplyConfig()` → no restart
- [ ] SPA sidebar tự động cập nhật khi user toggle visibility (no page reload)
- [ ] Protected bằng Basic Auth hoặc JWT (configurable)
- [ ] Embedded static assets via `go:embed` — zero external deps
- [ ] `axe plugin add admin` → inject vào main.go tự động
- [ ] `axe_admin_settings` table tự tạo tại `Register()` nếu chưa có

**Files cần tạo**:
```
pkg/plugin/admin/
  contrib.go      ← [NEW] Contributor + Configurable interfaces
  plugin.go       ← AdminPlugin: discovery, REST API, static mount
  settings.go     ← axe_admin_settings DB operations
  plugin_test.go  ← test: contributor discovery, nav toggle, config apply
```

### Story 8.8 — `axe-plugin-ratelimit`
**Sprint**: 22 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Per-route và global rate limiting plugin với Redis backend.

**Acceptance Criteria**:
- [ ] `pkg/plugin/ratelimit/` — RateLimitPlugin
- [ ] Config: `RPS`, `Burst`, `KeyFunc` (by IP, by user, by API key)
- [ ] Redis sliding window algorithm
- [ ] HTTP 429 response với `Retry-After` header
- [ ] Prometheus: `axe_ratelimit_blocked_total`

### Story 8.9 — `axe-plugin-oauth2`
**Sprint**: 23 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: OAuth2 social login plugin (Google, GitHub, Facebook).

**Acceptance Criteria**:
- [ ] `pkg/plugin/oauth2/` — OAuth2Plugin
- [ ] Providers: Google, GitHub (extensible interface for others)
- [ ] Callback routes auto-registered: `GET /auth/{provider}`, `GET /auth/{provider}/callback`
- [ ] On success: issue JWT token (reuse existing auth plugin)
- [ ] State parameter CSRF protection

### Story 8.10 — Plugin Consistency & Quality Gates
**Sprint**: 21 | **Priority**: P1 | **Status**: ✅ Done

**Goal**: Đảm bảo consistency và fluency khi thêm ngày càng nhiều plugins (kể cả 100+) mà không có runtime errors, startup panics, hoặc circular dependency deadlocks. **Đây là prerequisite của mọi story plugin mới.**

> **Motivation**: Hai gaps nguy hiểm nhất hiện tại:
> 1. Plugin A silently phụ thuộc B → `MustResolve` panic lúc runtime nếu B chưa register
> 2. A → B → C → A (circular dep) → hiện tại **không được phát hiện**, gây ra sai thứ tự khởi động

**6-Layer Quality Model**:

#### Layer 1 — Interface Contract ✅ Done
Compiler enforces `Name() / Register() / Shutdown()`. Mọi plugin thiếu một method → compile error.

#### Layer 2 — Duplicate Detection ✅ Done
```go
// plugin.go:158 — đã có
if a.names[p.Name()] {
    return fmt.Errorf("plugin: duplicate plugin name %q", p.Name())
}
```

#### Layer 3a — Dependency Declaration ❌ Missing → Phải thêm
```go
// Thêm vào pkg/plugin/plugin.go

// Dependent là optional interface — plugins có dependency implement thêm.
type Dependent interface {
    DependsOn() []string // returns tên của các plugins bắt buộc
}
```

#### Layer 3b — Cycle Detection (Kahn's Algorithm) ❌ Missing → Phải thêm
```go
// App.Start() chạy validateDAG() TRƯỚC khi gọi bất kỳ Register() nào

func (a *App) validateDAG() error {
    inDegree := make(map[string]int, len(a.plugins))
    edges     := make(map[string][]string)

    for _, p := range a.plugins {
        inDegree[p.Name()] = 0
    }
    for _, p := range a.plugins {
        dep, ok := p.(Dependent)
        if !ok { continue }
        for _, need := range dep.DependsOn() {
            if !a.names[need] {
                return fmt.Errorf(
                    "plugin %q requires %q — add app.Use(%s.New(...)) before Start()",
                    p.Name(), need, need,
                )
            }
            edges[need] = append(edges[need], p.Name())
            inDegree[p.Name()]++
        }
    }
    // Kahn's topological sort
    queue := []string{}
    for _, p := range a.plugins {
        if inDegree[p.Name()] == 0 {
            queue = append(queue, p.Name())
        }
    }
    visited := 0
    for len(queue) > 0 {
        node := queue[0]; queue = queue[1:]
        visited++
        for _, next := range edges[node] {
            inDegree[next]--
            if inDegree[next] == 0 {
                queue = append(queue, next)
            }
        }
    }
    if visited != len(a.plugins) {
        return fmt.Errorf("plugin: circular dependency detected — check DependsOn() declarations")
    }
    return nil // DAG valid, safe to Register in order
}
```

**Example — circular dep caught at startup, not runtime:**
```
app.Use(auth)    // auth.DependsOn() = ["oauth2"]
app.Use(oauth2)  // oauth2.DependsOn() = ["auth"]
app.Start(ctx)
// → error: "plugin: circular dependency detected"   ← caught before any Register()
```

#### Layer 4 — Fail-fast Config Validation ⚠️ Convention → Enforce
```go
// BAD — phát hiện lỗi SAU khi app đã start
func (p *EmailPlugin) Register(ctx, app) error {
    if p.config.APIKey == "" { return errors.New("api key required") }
}

// GOOD — phát hiện lỗi tại New() = trước app.Start()
func New(cfg Config) (*EmailPlugin, error) {
    if cfg.APIKey == "" {
        return nil, errors.New("email: SENDGRID_API_KEY is required")
    }
    return &EmailPlugin{config: cfg}, nil
}
```
**Rule**: Tất cả plugins mới **phải** validate config trong `New()`, không được để trong `Register()`.

#### Layer 5 — Typed Service Key Constants ✅ Convention → Enforce
```go
// Mỗi plugin export const tên service — không dùng string literal
const ServiceKey = "storage"  // pkg/plugin/storage/storage.go (actual value)

// Cross-plugin communication an toàn:
store := plugin.MustResolve[Store](app, storage.ServiceKey)
//                                       ^ constant, không phải string "storage"
```

#### Layer 6 — Shared Resource Pool ❌ Convention → Enforce (quan trọng ở scale lớn)
```go
// VẤN ĐỀ: 100 plugins × 10 DB connections/plugin = 1000 connections → DB crash

// BAD — plugin tự tạo connection pool riêng
func (p *EmailPlugin) Register(ctx context.Context, app *plugin.App) error {
    db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL")) // ❌ NEVER
}

// GOOD — plugin dùng shared pool từ App
func (p *EmailPlugin) Register(ctx context.Context, app *plugin.App) error {
    p.db = app.DB       // ✅ shared pool
    p.cache = app.Cache // ✅ shared client
}
```
**Rule**: Plugins **nghiêm cấm** tự tạo `*sql.DB`, `*redis.Client` mới. Chỉ được dùng `app.DB`, `app.Cache`, `app.Hub`.

**Acceptance Criteria**:
- [ ] `Dependent` interface thêm vào `pkg/plugin/plugin.go`
- [ ] `validateDAG()` (Kahn's algorithm) chạy trong `App.Start()` trước bất kỳ `Register()` nào
- [ ] `AllPlugins() []Plugin` method thêm vào `App` — trả về slice of all registered plugins (needed by admin)
- [ ] Unit test: A → B missing → error "requires"
- [ ] Unit test: A → B → C → A → error "circular dependency"
- [ ] Unit test: A → B → C (linear) → Start() success, Register() order correct
- [ ] **`pkg/plugin/testing/mock.go`** → `NewMockApp()` with in-memory SQLite, chi router, slog.Default(), nil Cache
- [ ] Plugin unit test example using `MockApp` added to Authoring Guide
- [ ] **Plugin Authoring Guide** (`docs/plugin-guide.md`) document 6 layers + MockApp + examples
- [ ] Tất cả existing plugins (storage) bổ sung `const ServiceKey` nếu chưa có
- [ ] Tất cả new plugins (8.3–8.9, 8.11) **bắt buộc** follow Layer 4 + Layer 6 (code review gate)

**Files cần sửa**:
```
pkg/plugin/plugin.go          <- Dependent interface + validateDAG() + Start() update
pkg/plugin/plugin_test.go     <- DAG tests (missing dep, cycle, linear chain)
docs/plugin-guide.md          <- [NEW] 6-layer plugin authoring guide
```

### Story 8.11 — Parallel Plugin Startup
**Sprint**: 24 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Plugins không phụ thuộc nhau khởi động song song (wave-based), giữ startup time < 3s kể cả khi có 50+ plugins.

> **Motivation**: Sequential startup với 50 plugins × avg 200ms = 10 giây. Không chấp nhận được cho production restart.

**Design — Wave-based parallel startup:**
```
Sau validateDAG() → group plugins thành waves dựa trên dependency depth:

Wave 0 (no deps):    [cache, logger, storage]  → goroutine parallel, wait all
Wave 1 (dep W0):     [ratelimit, email, jobs]  → goroutine parallel, wait all
Wave 2 (dep W1):     [oauth2, admin]           → goroutine parallel, wait all
────────────────────────────────────────────────────────────────────────────
Total: 3 waves × ~200ms = ~600ms  vs  50 × 200ms = 10s sequential
```

```go
// App.Start() sau khi validateDAG():
waves := buildWaves(a.plugins) // group by topological level

for waveIdx, wave := range waves {
    var wg sync.WaitGroup
    errs := make(chan error, len(wave))
    for _, p := range wave {
        wg.Add(1)
        go func(p Plugin) {
            defer wg.Done()
            if err := p.Register(ctx, a); err != nil {
                errs <- fmt.Errorf("plugin %s: %w", p.Name(), err)
            }
        }(p)
    }
    wg.Wait()
    close(errs)
    // collect errors from this wave — rollback if any
    for err := range errs {
        // shutdown all registered so far (LIFO)
        return a.rollback(ctx, waveIdx)
    }
}
```

**Constraints**:
- Plugin `Register()` phải **goroutine-safe** — documented requirement
- `app.Router` routes registration dùng chi.Router's built-in mutex — safe
- `Provide[T]()` / `Resolve[T]()` dùng `sync.RWMutex` — safe (đã có)

**Acceptance Criteria**:
- [ ] `buildWaves()` function: group plugins by topological depth (output of Kahn's from Story 8.10)
- [ ] `App.Start()` chạy từng wave song song với `sync.WaitGroup`
- [ ] Rollback khi bất kỳ plugin nào trong wave fail: shutdown toàn bộ waves đã hoàn thành (LIFO wave)
- [ ] Benchmark: 20 mock plugins (50ms sleep each) → parallel < 500ms, sequential = 1000ms
- [ ] `app.Logger` log wave number + plugins per wave + duration
- [ ] **Prerequisite**: Story 8.10 (validateDAG) phải done trước

**Files cần sửa**:
```
pkg/plugin/plugin.go          <- buildWaves() + parallel Start()
pkg/plugin/plugin_test.go     <- benchmark + wave grouping tests
```

---

### Story 8.12 — Plugin Event Bus
**Sprint**: 24 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Cho phép plugins communicate với nhau qua events mà **không cần direct import** — giải quyết coupling khi ecosystem có nhiều plugins.

> **Motivation**: AI plugin muốn process file khi storage plugin upload xong. Email plugin muốn notify khi user register. Nếu không có event bus → plugins phải import nhau → circular deps hoặc tight coupling.

**Design**:

```go
// pkg/plugin/events/bus.go — new package

type Event struct {
    Topic   string         // "storage.uploaded", "user.registered", "job.failed"
    Payload map[string]any // type-erased, documented per topic
    Meta    EventMeta
}

type EventMeta struct {
    PluginSource string    // which plugin published
    Timestamp    time.Time
    TraceID      string    // for distributed tracing
}

type Handler func(ctx context.Context, e Event) error

type Bus interface {
    Publish(ctx context.Context, e Event) error
    Subscribe(topic string, handler Handler)
    // Wildcard: "storage.*" matches "storage.uploaded", "storage.deleted"
}
```

**Delivery modes**:

| Mode | Config | Use case |
|---|---|---|
| In-process sync | `Delivery: Sync` | Audit log, cache invalidation |
| In-process async | `Delivery: Async` | AI analysis, thumbnail gen |
| Redis pub/sub | `Delivery: Redis` | Multi-instance fan-out |

**Example — AI plugin reacts to storage upload:**
```go
// pkg/plugin/ai/openai/plugin.go — NO import of storage package
func (p *OpenAIPlugin) Register(ctx context.Context, app *plugin.App) error {
    app.Events.Subscribe("storage.uploaded", func(ctx context.Context, e Event) error {
        key := e.Payload["key"].(string)
        return p.autoGenerateAltText(ctx, key) // AI processes new file
    })
    app.Router.Post("/ai/admin/chat", p.handleChat)
    return nil
}
```

**Standard topic conventions**:
```
storage.uploaded       storage.deleted
user.registered        user.deleted        user.login
job.enqueued           job.completed       job.failed
email.sent             email.failed
payment.succeeded      payment.failed
```

**Acceptance Criteria**:
- [ ] `pkg/plugin/events/bus.go` — `Bus` interface + in-process implementation
- [ ] `App` struct expose `Events Bus` field — accessible in all `Register()` calls
- [ ] `Sync` delivery: handler called in same goroutine as `Publish()`
- [ ] `Async` delivery: handler called in separate goroutine, errors logged
- [ ] `Redis` delivery: uses `app.Cache` (Layer 6 rule ✅ — no new connection)
- [ ] Wildcard subscriptions: `"storage.*"` matches all storage events
- [ ] Unit test: publish → subscriber receives event
- [ ] Unit test: async delivery → non-blocking Publish()
- [ ] Standard topic constants exported: `events.TopicStorageUploaded`, etc.
- [ ] **Plugin Authoring Guide** updated with event bus section

**Files cần tạo**:
```
pkg/plugin/events/
  bus.go         ← [NEW] Bus interface + in-process + Redis implementations
  topics.go      ← [NEW] Standard topic name constants
  bus_test.go    ← [NEW] sync/async/wildcard tests
pkg/plugin/plugin.go  ← add Events field to App struct
```

---

### Story 8.13 — Plugin Observability Contract
**Sprint**: 23 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Mọi plugin **tự động** có metrics, health check, và structured logging theo convention — không cần mỗi plugin tự implement từ đầu.

> **Design**: Plugin framework provides helpers; plugins use them. Observability is opt-in per feature but the **naming conventions are enforced**.

**3 pillars of plugin observability**:

#### Pillar 1 — Metrics Convention
```go
// pkg/plugin/obs/metrics.go — helper package

// NewCounter tạo Prometheus counter với axe naming convention:
// axe_{plugin}_{metric}_{unit}
func NewCounter(pluginName, metric, unit string, labels ...string) prometheus.Counter {
    return prometheus.NewCounter(prometheus.CounterOpts{
        Name: fmt.Sprintf("axe_%s_%s_%s", pluginName, metric, unit),
        // e.g.: axe_email_sent_total, axe_ai_tokens_used_total
    })
}
// Naming enforced — no "my_custom_metric_name" allowed in official plugins
```

#### Pillar 2 — Health Check Contribution
```go
// Optional interface — plugins with external dependencies implement this
type HealthChecker interface {
    HealthCheck(ctx context.Context) HealthStatus
}

type HealthStatus struct {
    Plugin  string
    OK      bool
    Message string        // "connected", "timeout after 2s"
    Latency time.Duration // optional: round-trip time
}

// GET /ready aggregates ALL plugins that implement HealthChecker:
// { "ok": true, "plugins": { "email": "ok", "redis": "ok", "storage": "ok" }}
```

#### Pillar 3 — Structured Logging
```go
// Plugin receives a pre-tagged logger — no manual slog.With("plugin", ...) needed
func (p *EmailPlugin) Register(ctx context.Context, app *plugin.App) error {
    p.log = app.Logger.With("plugin", p.Name()) // done ONCE at registration

    p.log.Info("email plugin ready", "provider", "sendgrid")
    // → {"level":"INFO","plugin":"email","provider":"sendgrid","msg":"email plugin ready"}
}
```

**Acceptance Criteria**:
- [ ] `pkg/plugin/obs/` package with `NewCounter()`, `NewHistogram()`, `NewGauge()` helpers
- [ ] Naming convention enforced: `axe_{plugin}_{metric}_{unit}` — helper generates name
- [ ] `HealthChecker` interface exported from `pkg/plugin/plugin.go`
- [ ] `GET /ready` endpoint aggregates health from all `HealthChecker` plugins
- [ ] `GET /ready` returns 503 if ANY plugin returns `OK: false`
- [ ] Pre-tagged logger: `app.Logger.With("plugin", p.Name())` documented in guide as convention
- [ ] Default metrics auto-registered per plugin: `axe_{plugin}_register_duration_seconds`
- [ ] **Plugin Authoring Guide** updated: observability section with examples

**Files cần tạo/sửa**:
```
pkg/plugin/obs/
  metrics.go     ← [NEW] naming-convention helpers
  health.go      ← [NEW] HealthChecker interface + aggregator
  obs_test.go    ← [NEW] naming convention tests
pkg/plugin/plugin.go  ← export HealthChecker interface
cmd/api/main.go       ← /ready uses plugin health aggregator
```

---

### Story 8.14 — Plugin Versioning & Compatibility
**Sprint**: 25 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Plugin khai báo axe version tối thiểu cần thiết. `App.Start()` kiểm tra compatibility trước khi register — tránh silent API mismatch.

> **Motivation**: Khi axe v2.0 thay đổi `App` struct (thêm field, đổi interface), plugins viết cho v1.x có thể compile OK nhưng misbehave lúc runtime. Versioning catch điều này tại startup.

**Design**:

```go
// pkg/plugin/plugin.go — thêm optional interface

const AxeVersion = "v1.5.0" // bumped mỗi release

// Versioned là optional — plugins declare minimum axe version.
type Versioned interface {
    MinAxeVersion() string // e.g. "v1.0.0"
}

// App.Start() checks trước validateDAG():
for _, p := range a.plugins {
    v, ok := p.(Versioned)
    if !ok { continue } // no constraint = always compatible
    if !semverCompatible(AxeVersion, v.MinAxeVersion()) {
        return fmt.Errorf(
            "plugin %q requires axe >= %s, running %s — update axe or use older plugin version",
            p.Name(), v.MinAxeVersion(), AxeVersion,
        )
    }
}
```

**Plugin declares version**:
```go
// pkg/plugin/ai/openai/plugin.go
func (p *OpenAIPlugin) MinAxeVersion() string {
    return "v1.5.0" // requires Events Bus from Story 8.12
}
```

**Compatibility rules**:
```
axe v1.5.x → compatible with plugins requiring >= v1.0.0, >= v1.5.0
axe v2.0.0 → NOT compatible with plugins requiring >= v2.1.0
Semver: MAJOR breaks → incompatible. MINOR/PATCH → backward compatible.
```

**Acceptance Criteria**:
- [ ] `const AxeVersion` in `pkg/plugin/plugin.go` — bumped on every release
- [ ] `Versioned` interface: `MinAxeVersion() string`
- [ ] `App.Start()` checks `Versioned` before `validateDAG()` — earliest possible failure
- [ ] Semver comparison using stdlib or `golang.org/x/mod/semver`
- [ ] Unit test: plugin requires v2.0, running v1.5 → error
- [ ] Unit test: plugin requires v1.0, running v1.5 → success
- [ ] Unit test: plugin not implementing `Versioned` → always succeeds
- [ ] `axe plugin validate` CLI command checks version compatibility locally
- [ ] **Plugin Authoring Guide** updated: when to set `MinAxeVersion()`

**Files cần sửa**:
```
pkg/plugin/plugin.go       ← Versioned interface + AxeVersion const + Start() check
pkg/plugin/plugin_test.go  ← version compatibility tests
cmd/axe/plugin/validate.go ← [NEW] axe plugin validate command
```

---

## Official Plugin Roadmap

### 🟢 Short-term — v1.0 (Epic 8, Sprint 19–24)

Plugins mọi production backend đều cần. Hoàn thành trước release v1.0.

#### 🔐 Security
| Plugin | `axe plugin add` | Location | Status | Sprint | Priority |
|---|---|---|---|---|---|
| JWT Auth | built-in | `pkg/middleware/` | ✅ Done | - | P0 |
| CORS | built-in | `pkg/middleware/` | ✅ Done | - | P0 |
| `ratelimit` | `ratelimit` | `pkg/plugin/ratelimit/` | ✅ Done | 22 | P2 |
| `oauth2` (Google/GitHub) | `oauth2` | `pkg/plugin/oauth2/` | ✅ Done | 23 | P2 |
| `tenant` (multi-tenancy middleware) | `tenant` | `pkg/plugin/tenant/` | ✅ Done | 22 | P2 |

#### 📦 Storage
| Plugin | `axe plugin add` | Location | Status | Sprint | Priority |
|---|---|---|---|---|---|
| `storage` (FSStore/JuiceFS) | `storage` | `pkg/plugin/storage/` | ✅ Done | 19 | P2 |
| `storage-hardening` (Auth/Security) | - | `pkg/plugin/storage/` | ✅ Done | 20 | P0 |
| `storage-ops` (fsync/healthcheck) | - | `pkg/plugin/storage/` | ✅ Done | 21 | P0 |

#### 📧 Notifications
| Plugin | `axe plugin add` | Location | Status | Sprint | Priority |
|---|---|---|---|---|---|
| `email` (SendGrid/SMTP) | `email` | `pkg/plugin/email/` | ✅ Done | 22 | P2 |
| `push` (FCM/APNs) | `push` | `pkg/plugin/push/` | 🟡 Planned | 23 | P3 |

#### 🖥️ Developer Tools
| Plugin | `axe plugin add` | Location | Status | Sprint | Priority |
|---|---|---|---|---|---|
| Plugin CLI (list/add/new/validate) | `axe plugin ...` | `cmd/axe/plugin/` | ✅ Done | 23 | P3 |
| `admin` (Admin UI Dashboard) | `admin` | `pkg/plugin/admin/` | ✅ Done | 24 | P3 |

#### 🏗️ Plugin Infrastructure (Framework Stories)
| Story | Name | Location | Status | Sprint | Priority |
|---|---|---|---|---|---|
| 8.6b | Storage Conformance (reference impl) | `pkg/plugin/storage/` | ✅ Done | 21 | P1 |
| 8.10 | Correctness Gates (6-layer + DAG + MockApp) | `pkg/plugin/plugin.go` | ✅ Done | 21 | P1 |
| 8.11 | Parallel Startup (wave-based) | `pkg/plugin/plugin.go` | ✅ Done | 24 | P2 |
| 8.12 | Event Bus (pub/sub, 3 modes) | `pkg/plugin/events/` | ✅ Done | 24 | P2 |
| 8.13 | Observability Contract (metrics/health/logging) | `pkg/plugin/obs/` | ✅ Done | 23 | P2 |
| 8.14 | Versioning & Compatibility | `pkg/plugin/plugin.go` | ✅ Done | 25 | P2 |

---

### 🔵 Long-term — v2.0+ (Epic 9, Sprint 25+)

Plugins cho advanced use cases. Prioritized by community demand. See **Epic 9** for full details.

#### 💳 Payments
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `payment:stripe` | `payment stripe` | Planned | P2 |
| `payment:payos` (Vietnam) | `payment payos` | Planned | P3 |

#### 🔍 Search
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `search:typesense` | `search typesense` | Planned | P2 |
| `search:elastic` | `search elastic` | Planned | P3 |

#### ☁️ Cloud Storage
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `storage:s3` (AWS S3 / R2) | `storage s3` | Planned | P2 |
| `storage:gcs` (Google Cloud) | `storage gcs` | Planned | P3 |

#### 📨 Advanced Messaging
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `kafka` | `kafka` | Planned | P2 |
| `rabbitmq` | `rabbitmq` | Planned | P3 |

#### 📊 Advanced Observability
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `otel` (OpenTelemetry traces) | `otel` | Planned | P2 |
| `sentry` (error tracking) | `sentry` | Planned | P2 |
| `datadog` | `datadog` | Planned | P3 |

#### 📱 SMS
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `sms:twilio` | `sms twilio` | Planned | P3 |

#### 🤖 AI (Inspired by Spring AI)
| Plugin | `axe plugin add` | Status | Priority |
|---|---|---|---|
| `ai:openai` (ChatGPT/DALL-E) | `ai openai` | Planned | P2 |
| `ai:gemini` (Google Gemini) | `ai gemini` | Planned | P2 |
| `ai:ollama` (local LLM) | `ai ollama` | Planned | P3 |
| `ai:anthropic` (Claude) | `ai anthropic` | Planned | P3 |

#### 🌐 Web Configurator
| Feature | Description | Status | Priority |
|---|---|---|---|
| `start.axe.io` | Web UI (like start.spring.io) generates `main.go` scaffold | Planned | P3 |
| Shareable config URLs | `start.axe.io?plugins=auth,storage,email` | Planned | P3 |

---

## Technical Design

### Plugin Lifecycle (with Quality Gates)

```
app.Use(pluginA)          <- enqueue (FIFO), check duplicate name [Layer 2]
app.Use(pluginB)
app.Start(ctx)
  Step 1: [Layer 7] check MinAxeVersion()   <- Versioned interface (Story 8.14)
  Step 2: [Layer 3] validateDAG()           <- Kahn's: missing dep + cycle detection (Story 8.10)
  Step 3: buildWaves()                      <- group by topological depth (Story 8.11)
  Step 4: for each wave (parallel):
    goroutines: call Register(ctx, app)      <- [Layer 4] config pre-validated in New()
    wait WaitGroup
    on any error: rollback LIFO (shutdown completed waves in reverse)
  ...app running...
app.Shutdown(ctx)
  Shutdown all in LIFO order (collect all errors via errors.Join)

Monorepo layout:
  pkg/plugin/
    plugin.go             <- Plugin + Dependent + HealthChecker + Versioned interfaces
    events/               <- [NEW] Event Bus (Story 8.12)
    obs/                  <- [NEW] Observability helpers (Story 8.13)
    testing/              <- [NEW] MockApp for plugin unit tests
    admin/                <- embedded admin UI + Contributor/Configurable interfaces
    storage/              <- FSStore (zero external deps) — reference implementation
    ratelimit/            <- Redis sliding window
    email/                <- SendGrid + SMTP
    oauth2/               <- Google, GitHub providers
    tenant/               <- multi-tenancy middleware
```

### 6-Layer Consistency Model (ADR-011)

| Layer | Mechanism | Where | Status |
|---|---|---|---|
| 1. Interface Contract | Compiler enforces `Name/Register/Shutdown` | `plugin.go` | ✅ Done |
| 2. Duplicate Detection | `names` map check in `App.Use()` | `plugin.go:158` | ✅ Done |
| 3. Dependency Declaration | `Dependent` interface + Kahn's cycle detection | `plugin.go` | ✅ Done (Story 8.10) |
| 4. Fail-fast Config | Validate in `New()`, not `Register()` | per-plugin | ✅ Enforced — all plugins return `(*Plugin, error)` |
| 5. Typed Service Keys | `const ServiceKey` per plugin | per-plugin | ✅ Enforced |
| 6. Shared Resource Pool | Use `app.DB`/`app.Cache`, never create new connections | per-plugin | ✅ Enforced — all plugins use `app.Cache.Redis()` |

### Plugin Authoring Checklist (for code review)

Every new plugin MUST:
- [ ] `Name()` returns a unique lowercase string (e.g. `"email"`, `"oauth2"`)
- [ ] `New(cfg Config) (*Plugin, error)` validates all required config fields
- [ ] Exports `const ServiceKey = "plugin-name.service"` if it provides a service
- [ ] Implements `Dependent` interface if it requires other plugins
- [ ] Has unit tests covering: happy path, missing config, shutdown
- [ ] Prometheus metrics follow naming: `axe_{plugin}_{metric}_{unit}`

---

## Risks
- **Silent dependency errors** ❗: Plugin A gọi `MustResolve` plugin B nhưng B không register → **panic lúc runtime** → Fix: Story 8.10 (`Dependent` interface)
- **Config errors at runtime** ❗: Plugin validate config trong `Register()` thay vì `New()` → app start thành công nhưng fail khi request đến → Fix: Layer 4 convention (code review gate)
- **Plugin ordering bugs**: Plugin A register route `/admin` trước Plugin B register middleware cho `/admin` → middleware bị bỏ qua → Fix: document ordering convention trong plugin guide
- **Dependency bloat**: khi thêm email (SendGrid SDK) + payment (Stripe SDK), `go get axe` sẽ nặng hơn → monitor, tách submodule nếu cần
- **Template drift**: email templates có thể outdated → versioning cần thiết
- ~~**Storage security**: upload/delete routes không có auth~~ → Fixed by Story 8.6
- ~~**Path traversal**: `../../etc/passwd` potential~~ → Fixed by Story 8.6
- ~~**FUSE silent failure**: stale mount passes os.Stat but fails writes~~ → Fixed by Stories 9.3–9.4
- ~~**Raw syscall errors leaking**: ENOTCONN etc. in HTTP responses~~ → Fixed by Story 9.4
