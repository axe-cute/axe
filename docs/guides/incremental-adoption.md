# Incremental Adoption Guide

> **Audience**: Go developers with an existing backend project who want to adopt axe patterns and packages incrementally — without rewriting their codebase.

## Philosophy

axe is designed as a **library-first framework**. Every `pkg/` package can be imported and used independently in your existing Go project. You don't need `axe new` to benefit from axe.

This guide shows you how to adopt axe in 4 progressive stages — from the smallest change (zero-risk) to a full migration (when you're ready).

---

## Quick Reference — What Can I Use Today?

| Package | Import Path | Dependencies | Use Case |
|---|---|---|---|
| `apperror` | `github.com/axe-cute/axe/pkg/apperror` | **Zero** (stdlib only) | Typed HTTP error taxonomy |
| `txmanager` | `github.com/axe-cute/axe/pkg/txmanager` | **Zero** (stdlib `database/sql`) | Unit of Work transactions |
| `cache` | `github.com/axe-cute/axe/pkg/cache` | `go-redis/v9` | Redis cache-aside + JWT blocklist |
| `logger` | `github.com/axe-cute/axe/pkg/logger` | **Zero** (stdlib `log/slog`) | Structured logging with request ID |
| `jwtauth` | `github.com/axe-cute/axe/pkg/jwtauth` | `golang-jwt/jwt/v5` | JWT access/refresh token pairs |
| `worker` | `github.com/axe-cute/axe/pkg/worker` | `hibiken/asynq` | Background job processing |
| `outbox` | `github.com/axe-cute/axe/pkg/outbox` | `worker` | Transactional outbox pattern |
| `ws` | `github.com/axe-cute/axe/pkg/ws` | `nhooyr.io/websocket` | WebSocket Hub/Client/Room |
| `plugin/*` | `github.com/axe-cute/axe/pkg/plugin/...` | Varies | Plugin system (full framework) |

---

## Stage 1: Drop-in Packages (5 minutes, zero risk)

These packages have **zero internal dependencies** — they work in any Go project.

### 1.1 — `apperror`: Typed Error Taxonomy

**Problem**: Your handlers return raw `http.StatusNotFound` with inconsistent error formats.

**Solution**:

```go
// go get github.com/axe-cute/axe@latest

import "github.com/axe-cute/axe/pkg/apperror"

func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
    user, err := h.svc.FindByID(r.Context(), chi.URLParam(r, "id"))
    if err != nil {
        // Before: http.Error(w, "not found", 404) — inconsistent, no JSON
        // After:  structured, typed, consistent across your entire API
        apperror.Write(w, err) // auto-maps NotFound→404, Unauthorized→401, etc.
        return
    }
    json.NewEncoder(w).Encode(user)
}

// In your service layer:
func (s *UserService) FindByID(ctx context.Context, id string) (*User, error) {
    user, err := s.repo.Get(ctx, id)
    if err != nil {
        return nil, apperror.NotFound("user", id) // ← typed error, maps to 404
    }
    return user, nil
}
```

**Error types available**: `NotFound`, `Unauthorized`, `Forbidden`, `Conflict`, `ValidationFailed`, `Internal`.

### 1.2 — `txmanager`: Transaction Unit of Work

**Problem**: You're passing `*sql.Tx` through function params, or forgetting to commit/rollback.

**Solution**:

```go
import "github.com/axe-cute/axe/pkg/txmanager"

// Setup (once, in your main.go or DI):
tx := txmanager.New(db) // db is your *sql.DB

// Usage (in any service):
func (s *OrderService) Create(ctx context.Context, order Order) error {
    return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
        // All repo calls use the SAME transaction from ctx — automatic.
        if err := s.orderRepo.Insert(ctx, order); err != nil {
            return err // auto-rollback
        }
        return s.inventoryRepo.Deduct(ctx, order.Items) // same tx
        // auto-commit on nil return
    })
}

// Your repository extracts the tx from context:
func (r *OrderRepo) Insert(ctx context.Context, order Order) error {
    tx := txmanager.FromCtx(ctx) // returns *sql.Tx or the raw *sql.DB
    _, err := tx.ExecContext(ctx, "INSERT INTO orders ...", ...)
    return err
}
```

### 1.3 — `logger`: Structured Logging with Request ID

**Problem**: Your logs use `log.Printf` or inconsistent slog setup.

```go
import "github.com/axe-cute/axe/pkg/logger"

// Setup:
log := logger.New(logger.Config{
    Level:  "info",       // or "debug", "warn", "error"
    Format: "json",       // or "text" for development
})

// In HTTP middleware — inject request ID:
func RequestIDMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        reqID := r.Header.Get("X-Request-ID")
        if reqID == "" {
            reqID = uuid.NewString()
        }
        ctx := logger.WithRequestID(r.Context(), reqID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// Anywhere in your code — request ID is automatically included:
log.InfoContext(ctx, "order created", "order_id", order.ID)
// Output: {"level":"info","msg":"order created","order_id":"123","request_id":"abc-def"}
```

---

## Stage 2: Infrastructure Packages (30 minutes)

These require one external dependency but integrate seamlessly.

### 2.1 — `cache`: Redis Cache-Aside

```go
import "github.com/axe-cute/axe/pkg/cache"

// Setup:
c, err := cache.New(cache.Config{
    Addr:   "localhost:6379",
    Prefix: "myapp:", // avoid collisions
})

// Usage:
val, err := c.Get(ctx, "user:42")
if errors.Is(err, cache.ErrCacheMiss) {
    // fetch from DB, then cache
    c.Set(ctx, "user:42", jsonData, 5*time.Minute)
}

// JWT logout blocklist (works with any JWT library):
c.BlockToken(ctx, jti, remainingTTL)
blocked, _ := c.IsTokenBlocked(ctx, jti)
```

### 2.2 — `jwtauth`: JWT Token Pair

```go
import "github.com/axe-cute/axe/pkg/jwtauth"

auth := jwtauth.New(jwtauth.Config{
    AccessSecret:  os.Getenv("JWT_ACCESS_SECRET"),
    RefreshSecret: os.Getenv("JWT_REFRESH_SECRET"),
    AccessTTL:     15 * time.Minute,
    RefreshTTL:    7 * 24 * time.Hour,
})

// Generate tokens:
pair, err := auth.GenerateTokenPair(userID, map[string]interface{}{
    "role": "admin",
})
// pair.AccessToken, pair.RefreshToken

// Chi middleware (works with any router):
r.Route("/api", func(r chi.Router) {
    r.Use(auth.Verifier())   // extract token from Authorization header
    r.Use(auth.Authenticator()) // reject invalid tokens
    r.Get("/me", meHandler)
})
```

---

## Stage 3: `axe generate resource` (Code Generator Only)

If your project follows a **handler → service → repository** pattern (or similar), you can use the axe CLI generator even without the full framework.

### Setup

```bash
# Install the CLI:
go install github.com/axe-cute/axe/cmd/axe@latest

# Navigate to your existing project:
cd ~/myproject

# Generate a resource:
axe generate resource Post \
  --fields "title:string,body:text,published:bool,author_id:uuid" \
  --module github.com/yourorg/myproject
```

This generates 10 files following Clean Architecture:

```
domain/post.go              ← struct + interface
internal/handler/post.go    ← HTTP handlers
internal/service/post.go    ← business logic
internal/repository/post.go ← database queries
db/migrations/xxx_post.sql  ← SQL migration
```

**You keep your existing project structure** — just move the generated files to match your layout if it differs from axe conventions.

### What if my structure is different?

The generated code uses clean dependency injection. The key interfaces are:

```go
// domain/post.go — your domain layer
type PostRepository interface {
    Create(ctx context.Context, post *Post) error
    FindByID(ctx context.Context, id string) (*Post, error)
    // ...
}
```

You can rename packages, move files, and rewire the DI — the generated code is **your code** once generated. There's no runtime dependency on axe.

---

## Stage 4: Full Framework Migration (When Ready)

When you're ready to go all-in, use `axe new` for your **next** project:

```bash
axe new my-next-api --db=postgres
cd my-next-api
axe generate resource Product --fields "name:string,price:decimal,sku:string"
make run
```

For your **existing** project, you've already adopted the valuable parts (Stages 1-3). The remaining framework features (plugin system, plugin CLI) are only needed if you want:

- Plugin lifecycle management (`app.Use(plugin)`)
- Automatic dependency DAG resolution
- Wave-based parallel plugin startup
- Admin UI dashboard

Most teams find that **Stages 1-3 cover 80% of the value**.

---

## Migration Checklist

Use this checklist to track your adoption:

```
Stage 1 — Drop-in (5 min each)
[ ] pkg/apperror — replace raw http.Error calls
[ ] pkg/txmanager — replace manual *sql.Tx passing
[ ] pkg/logger — replace log.Printf with structured slog

Stage 2 — Infrastructure (30 min each)
[ ] pkg/cache — replace raw redis calls
[ ] pkg/jwtauth — replace manual JWT code

Stage 3 — Generator (1 hour)
[ ] Install axe CLI
[ ] Generate 1 resource to see the output
[ ] Adapt generated code to your project structure

Stage 4 — Full framework (next project)
[ ] Use axe new for a new project
[ ] Evaluate plugin ecosystem
```

---

## FAQ

### Q: Does importing `pkg/apperror` pull in the entire axe dependency tree?

**No.** Each `pkg/` package declares only its own dependencies in `go.mod`. `apperror` has zero external dependencies — it only uses the Go standard library. Go's module system only downloads what you actually import.

### Q: Can I use `axe generate` in a project that doesn't use axe?

**Yes.** The generated code is standalone Go code. It uses standard `database/sql`, `net/http`, and `encoding/json`. There's no runtime import of `github.com/axe-cute/axe` in the generated handler/service/repo files (except `apperror` for typed errors, which you can remove if you prefer).

### Q: What Go version do I need?

axe requires **Go 1.25+** (for generics in the plugin system). The standalone packages (`apperror`, `txmanager`, `logger`) work with Go 1.21+.

### Q: I use Gin/Echo/Fiber, not Chi. Can I still use axe packages?

**Yes for Stages 1-2.** `apperror`, `txmanager`, `cache`, `logger`, `jwtauth`, and `worker` are router-agnostic. The `jwtauth` middleware returns `http.Handler` (stdlib), which works with any router that supports standard middleware.

**No for Stage 3.** The code generator produces Chi router code. You'd need to manually adapt the handler layer to your router, but the service and repository layers work unchanged.

### Q: How do I report issues or request features?

Open an issue at [github.com/axe-cute/axe/issues](https://github.com/axe-cute/axe/issues). Include your Go version, axe version (`axe --version`), and a minimal reproduction.
