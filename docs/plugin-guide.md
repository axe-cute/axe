# axe Plugin Authoring Guide

> **Version**: axe v1.0.0  
> **Last updated**: Sprint 25

This guide walks you through building a production-quality axe plugin from scratch.
All official axe plugins follow the same 6-layer quality model enforced by `axe plugin validate`.

---

## Table of Contents

1. [Quick Start](#1-quick-start)
2. [Plugin Interface Contract](#2-plugin-interface-contract)
3. [The 6-Layer Quality Model](#3-the-6-layer-quality-model)
4. [Dependency Declaration (DependsOn)](#4-dependency-declaration-dependson)
5. [Typed Service Locator](#5-typed-service-locator)
6. [Health Checks](#6-health-checks)
7. [Observability (Metrics & Logging)](#7-observability-metrics--logging)
8. [Event Bus Integration](#8-event-bus-integration)
9. [Admin UI Contribution](#9-admin-ui-contribution)
10. [Testing with MockApp](#10-testing-with-mockapp)
11. [Version Compatibility](#11-version-compatibility)
12. [Publishing & Discovery](#12-publishing--discovery)

---

## 1. Quick Start

```bash
# Scaffold a new plugin with all quality layers pre-wired:
axe plugin new billing

# Verify it compiles and passes quality gates:
go test ./pkg/plugin/billing/...
axe plugin validate
```

This generates:

```
pkg/plugin/billing/
  plugin.go       ← Plugin struct, Config, New(), Register(), Shutdown()
  plugin_test.go  ← Layer 4 config tests, Register lifecycle test
```

---

## 2. Plugin Interface Contract

Every axe plugin **must** implement three methods:

```go
type Plugin interface {
    Name()                                   string
    Register(ctx context.Context, app *App) error
    Shutdown(ctx context.Context)            error
}
```

| Method | When called | Purpose |
|---|---|---|
| `Name()` | Before `Start()` | Unique identifier. Used for duplicate detection and `DependsOn`. |
| `Register()` | During `Start()`, in dependency order | Wire routes, provide services, subscribe to events. |
| `Shutdown()` | During graceful shutdown, LIFO order | Close plugin-owned resources (goroutines, timers). |

> **Rule**: Never create DB/Redis connections in `Register()`. Use `app.DB`, `app.Cache` (Layer 6).

---

## 3. The 6-Layer Quality Model

The 6 layers are checked by `axe plugin validate`. All plugins **must** satisfy Layers 1–5.
Layer 6 is enforced at code review.

### Layer 1 — Interface Contract ✅ (compiler-enforced)

```go
var _ plugin.Plugin = (*Plugin)(nil) // compile-time interface check
```

### Layer 2 — Duplicate Detection ✅ (runtime, `app.Use()`)

`app.Use()` returns an error if two plugins share the same `Name()`.

### Layer 3 — Dependency Declaration

If your plugin needs another plugin's service, declare it:

```go
func (p *Plugin) DependsOn() []string {
    return []string{"auth", "storage"}
}
```

`app.Start()` validates the DAG (Kahn's algorithm) before any `Register()` is called.
Missing dependencies and circular deps are caught at startup — never at runtime.

```go
// ✅ Works — auth is registered first, oauth2 declares dependency
app.Use(auth)
app.Use(oauth2)   // oauth2.DependsOn() = ["auth"]
app.Start(ctx)

// ❌ Fails at Start() — missing dependency
app.Use(oauth2)   // "auth" not registered
app.Start(ctx)    // error: plugin "oauth2" requires "auth" — add app.Use(auth.New(...))
```

### Layer 4 — Fail-fast Config Validation

**Validate all required fields in `New()`, not `Register()`.**
This surfaces misconfiguration before the HTTP server starts.

```go
// ✅ CORRECT — fail-fast in New()
func New(cfg Config) (*Plugin, error) {
    if cfg.APIKey == "" {
        return nil, errors.New("billing: STRIPE_SECRET_KEY is required")
    }
    if cfg.WebhookSecret == "" {
        return nil, errors.New("billing: STRIPE_WEBHOOK_SECRET is required")
    }
    return &Plugin{cfg: cfg}, nil
}

// ❌ WRONG — too late, server already started
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
    if p.cfg.APIKey == "" {
        return errors.New("api key required")  // already in app.Start() territory
    }
    ...
}
```

### Layer 5 — Typed Service Key Constant

Export a typed constant so cross-plugin resolution is safe:

```go
// Your plugin:
const ServiceKey = "billing"

// Publisher (in Register):
plugin.Provide[*Service](app, ServiceKey, myService)

// Consumer (in another plugin's Register):
svc := plugin.MustResolve[*billing.Service](app, billing.ServiceKey)
//                                                ^ typed constant, not "billing" string
```

### Layer 6 — Shared Resource Pool

**Never create your own database or Redis connections.** Use what the app provides:

```go
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
    p.db    = app.DB        // ✅ shared *sql.DB pool
    p.cache = app.Cache     // ✅ shared *cache.Client
    p.hub   = app.Hub       // ✅ shared WebSocket hub
    p.log   = app.Logger.With("plugin", p.Name())
    ...
}
```

> **Why?** 100 plugins × 10 connections each = 1,000 connections → database crash.
> The app's shared pool is sized and monitored centrally.

---

## 4. Dependency Declaration (DependsOn)

```go
// pkg/plugin/payment/plugin.go
func (p *Plugin) DependsOn() []string {
    return []string{"auth"}  // requires JWT auth to be registered first
}

func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
    // Safe — auth is guaranteed registered before this runs
    jwtSvc := plugin.MustResolve[*jwtauth.Service](app, "auth")
    ...
}
```

`app.Start()` automatically places your plugin in the correct startup wave.
Plugins in the same dependency depth run **in parallel** (Story 8.11).

---

## 5. Typed Service Locator

### Provide a service

```go
// In Register() — provide your service for other plugins to use
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
    svc := &Service{...}
    plugin.Provide[*Service](app, ServiceKey, svc)
    return nil
}
```

### Resolve a service

```go
// Optional resolution — plugin may be absent
if store, ok := plugin.Resolve[storage.Store](app, storage.ServiceKey); ok {
    p.store = store
}

// Required resolution — panics if absent (combine with DependsOn)
store := plugin.MustResolve[storage.Store](app, storage.ServiceKey)
```

---

## 6. Health Checks

Implement `HealthChecker` to participate in the `/ready` probe:

```go
func (p *Plugin) HealthCheck(ctx context.Context) plugin.HealthStatus {
    start := time.Now()
    if err := p.client.Ping(ctx); err != nil {
        return plugin.HealthStatus{
            Plugin:  p.Name(),
            OK:      false,
            Message: "stripe API unreachable: " + err.Error(),
            Latency: time.Since(start),
        }
    }
    return plugin.HealthStatus{
        Plugin:  p.Name(),
        OK:      true,
        Message: "connected",
        Latency: time.Since(start),
    }
}
```

Wire the aggregator in `main.go`:

```go
agg := obs.NewAggregator(pluginApp, 5*time.Second)
r.Get("/ready", agg.Handler())  // 200 if all OK, 503 if any sick
```

---

## 7. Observability (Metrics & Logging)

Always use `pkg/plugin/obs` — it enforces the `axe_{plugin}_{metric}_{unit}` naming convention.

```go
import "github.com/axe-cute/axe/pkg/plugin/obs"

var (
    paymentSucceeded = obs.NewCounter("payment", "succeeded_total", "Successful payments.")
    paymentFailed    = obs.NewCounter("payment", "failed_total",    "Failed payments.")
    chargeDuration   = obs.NewHistogram("payment", "charge_duration_seconds", "Stripe charge latency.")
)

func (p *Plugin) Register(_ context.Context, app *plugin.App) error {
    p.log = obs.Logger(app, p.Name())  // pre-tagged: "plugin" = "payment"
    ...
}
```

> `axe plugin validate` checks that all metric names start with `axe_`.

---

## 8. Event Bus Integration

Publish and subscribe to events **without importing other plugins**:

```go
import "github.com/axe-cute/axe/pkg/plugin/events"

// Subscribe (in Register):
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
    // Triggered when any storage upload completes — no storage import needed
    app.Events.Subscribe(events.TopicStorageUploaded, p.onFileUploaded)
    return nil
}

func (p *Plugin) onFileUploaded(ctx context.Context, e events.Event) error {
    key := e.Payload["key"].(string)
    return p.generateThumbnail(ctx, key)
}

// Publish (from a handler):
func (p *Plugin) handlePayment(w http.ResponseWriter, r *http.Request) {
    ...
    app.Events.Publish(r.Context(), events.Event{
        Topic:   events.TopicPaymentSucceeded,
        Payload: map[string]any{"amount": charge.Amount, "currency": "usd"},
        Meta:    events.EventMeta{PluginSource: p.Name()},
    })
}
```

### Async delivery (non-blocking)

```go
// For heavy processing (AI, thumbnail gen, notifications):
bus, ok := app.Events.(*events.InProcessBus)
if ok {
    bus.SubscribeAsync(events.TopicStorageUploaded, p.processInBackground)
}
```

### Standard topic names

Use the constants from `pkg/plugin/events` to avoid typos:

| Constant | Topic |
|---|---|
| `TopicStorageUploaded` | `storage.uploaded` |
| `TopicUserRegistered` | `user.registered` |
| `TopicPaymentSucceeded` | `payment.succeeded` |
| `TopicJobFailed` | `job.failed` |
| … | see `events/bus.go` |

---

## 9. Admin UI Contribution

To appear in the `/axe-admin` dashboard, implement `admin.Contributor`:

```go
import "github.com/axe-cute/axe/pkg/plugin/admin"

// Minimal nav panel:
func (p *Plugin) AdminContribution() admin.Contribution {
    return admin.Contribution{
        ID:       "payment",
        NavLabel: "Payments",
        NavIcon:  "💳",
        APIRoute: "/payment/admin/dashboard",
    }
}

// With live-editable settings form (no restart required):
func (p *Plugin) AdminConfig() admin.ConfigSchema {
    return admin.ConfigSchema{
        Fields: []admin.ConfigField{
            {Key: "webhook_url",     Label: "Webhook URL",    Type: "text", Required: true},
            {Key: "secret_key",      Label: "API Secret Key", Type: "text", Sensitive: true},
            {Key: "capture_method",  Label: "Capture Method", Type: "select",
                Options: []string{"automatic", "manual"}},
        },
    }
}

func (p *Plugin) ApplyConfig(ctx context.Context, cfg map[string]any) error {
    if key, _ := cfg["secret_key"].(string); key == "" {
        return &admin.ErrInvalidConfig{Field: "secret_key", Reason: "cannot be empty"}
    }
    p.cfg.SecretKey = cfg["secret_key"].(string)
    return nil
}
```

---

## 10. Testing with MockApp

Use `plugintest.NewMockApp()` for unit tests — no infrastructure needed:

```go
import (
    "testing"
    plugintest "github.com/axe-cute/axe/pkg/plugin/testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNew_MissingAPIKey(t *testing.T) {
    _, err := New(Config{WebhookSecret: "ok"})  // APIKey missing
    require.Error(t, err)
    assert.Contains(t, err.Error(), "STRIPE_API_KEY")
}

func TestRegister_ProvidesService(t *testing.T) {
    p, err := New(Config{APIKey: "sk_test_...", WebhookSecret: "whsec_..."})
    require.NoError(t, err)

    app := plugintest.NewMockApp()
    require.NoError(t, p.Register(t.Context(), app))

    // Verify service is available after registration.
    svc, ok := plugin.Resolve[*Service](app, ServiceKey)
    require.True(t, ok)
    assert.NotNil(t, svc)
}

func TestShutdown_CleansCleansUp(t *testing.T) {
    p, _ := New(Config{APIKey: "sk_test_...", WebhookSecret: "whsec_..."})
    app := plugintest.NewMockApp()
    require.NoError(t, p.Register(t.Context(), app))
    require.NoError(t, p.Shutdown(t.Context()))
}
```

### MockApp provides

| Field | Value |
|---|---|
| `app.Router` | `chi.NewRouter()` |
| `app.Logger` | `slog.Default()` |
| `app.DB` | In-memory SQLite (`file::memory:?cache=shared`) |
| `app.Cache` | `nil` (Redis optional) |
| `app.Events` | `events.NoopBus{}` |

---

## 11. Version Compatibility

If your plugin uses APIs introduced in a specific axe release, declare `MinAxeVersion`:

```go
func (p *Plugin) MinAxeVersion() string {
    return "v1.0.0"  // requires Events Bus (Story 8.12)
}
```

`app.Start()` rejects the plugin if `AxeVersion < MinAxeVersion` with a clear error:

```
plugin "ai-openai" requires axe >= v1.5.0, running v1.0.0
→ update axe or use an older plugin version
```

**When to set `MinAxeVersion`**:
- You use `app.Events` → requires `v1.0.0`
- You use `plugin.Provide[T]` generics → requires `v1.0.0`
- You use wave-based startup guarantees → requires `v1.0.0`

---

## 12. Publishing & Discovery

### Official plugins (monorepo)

Located at `pkg/plugin/{name}/`. Added automatically to `axe plugin list`.

### Community plugins

Submit a PR to add to the registry in `cmd/axe/plugin/plugin.go`.
Community plugins follow the same 6-layer model — verified by `axe plugin validate`.

### Scaffold and validate

```bash
# Create
axe plugin new myplugin

# Implement Register() in pkg/plugin/myplugin/plugin.go

# Test
go test -race ./pkg/plugin/myplugin/...

# Quality gate (run in CI)
axe plugin validate
```

---

## Full Reference Example

```go
// pkg/plugin/billing/plugin.go
package billing

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/axe-cute/axe/pkg/plugin"
	"github.com/axe-cute/axe/pkg/plugin/events"
	"github.com/axe-cute/axe/pkg/plugin/obs"
	"github.com/go-chi/chi/v5"
)

// ServiceKey — Layer 5: typed service locator key.
const ServiceKey = "billing"

// Metrics — obs package enforces axe_{plugin}_{metric}_{unit} naming.
var (
	charged = obs.NewCounter("billing", "charges_total", "Stripe charges processed.")
	failed  = obs.NewCounter("billing", "failures_total", "Failed charge attempts.")
)

// Config — Layer 4: validated in New(), not Register().
type Config struct {
	APIKey        string
	WebhookSecret string
}

func (c *Config) validate() error {
	var errs []string
	if c.APIKey == "" {
		errs = append(errs, "STRIPE_SECRET_KEY is required")
	}
	if c.WebhookSecret == "" {
		errs = append(errs, "STRIPE_WEBHOOK_SECRET is required")
	}
	if len(errs) > 0 {
		return errors.New("billing: " + errors.Join(strsToErrs(errs)...).Error())
	}
	return nil
}

// Service is provided to other plugins via the service locator.
type Service struct{ cfg Config }

// Plugin implements plugin.Plugin.
type Plugin struct {
	cfg Config
	svc *Service
	log *slog.Logger
}

// New — Layer 4: config validated before app starts.
func New(cfg Config) (*Plugin, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Plugin{cfg: cfg}, nil
}

func (p *Plugin) Name() string { return "billing" }

// MinAxeVersion — Layer: version guard.
func (p *Plugin) MinAxeVersion() string { return "v1.0.0" }

// Register — Layer 6: use app.DB, app.Cache — never open new connections.
func (p *Plugin) Register(ctx context.Context, app *plugin.App) error {
	p.log = obs.Logger(app, p.Name())
	p.svc = &Service{cfg: p.cfg}

	// Layer 5: provide service.
	plugin.Provide[*Service](app, ServiceKey, p.svc)

	// Subscribe to relevant events (no import of user plugin needed).
	app.Events.Subscribe(events.TopicUserRegistered, p.onUserRegistered)

	// Register routes.
	app.Router.Route("/billing", func(r chi.Router) {
		r.Post("/webhook", p.handleWebhook)
	})

	p.log.Info("billing plugin registered")
	return nil
}

func (p *Plugin) Shutdown(_ context.Context) error { return nil }

func (p *Plugin) HealthCheck(ctx context.Context) plugin.HealthStatus {
	// Ping Stripe API (or check config validity).
	return plugin.HealthStatus{Plugin: p.Name(), OK: true, Message: "configured"}
}

func (p *Plugin) onUserRegistered(_ context.Context, e events.Event) error {
	p.log.Info("new user registered — creating billing customer", "email", e.Payload["email"])
	return nil
}

func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
	charged.Inc()
	app.Events.Publish(r.Context(), events.Event{  // nolint: app is captured via closure
		Topic: events.TopicPaymentSucceeded,
		Payload: map[string]any{"amount": 100},
	})
	w.WriteHeader(http.StatusOK)
}

func strsToErrs(ss []string) []error {
	e := make([]error, len(ss))
	for i, s := range ss {
		e[i] = errors.New(s)
	}
	return e
}
```

---

> Built with ❤️ using the axe plugin system.  
> Run `axe plugin validate` before every PR.
