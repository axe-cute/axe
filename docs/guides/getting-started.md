# Getting Started with Axe

> From zero to running API in 15 minutes.
> This is the **single entry point** for new users — no link-chasing required.

---

## Prerequisites

- Go 1.25+ installed
- Docker (for PostgreSQL/MySQL/Redis, optional for SQLite)

---

## 1. Install the CLI

```bash
go install github.com/axe-cute/axe/cmd/axe@latest
```

Verify:
```bash
axe --help
```

---

## 2. Create a New Project

```bash
axe new my-api --db=postgres --module=github.com/myorg/my-api --yes
cd my-api
```

This generates a complete project with:
```
my-api/
├── cmd/api/main.go          # Entry point (composition root)
├── internal/
│   ├── domain/              # Entity definitions + interfaces
│   ├── handler/             # HTTP handlers (Chi router)
│   ├── service/             # Business logic
│   └── repository/          # Data access (Ent or sqlc)
├── config/config.go         # Environment-based config
├── db/migrations/           # SQL migrations
├── Dockerfile               # Multi-stage production build
└── go.mod
```

**Architecture rule**: Dependencies flow downward. `domain/` imports nothing from `handler/` or `service/`. The Go compiler enforces this.

---

## 3. Generate a Resource

```bash
axe generate resource Post --fields="title:string,body:text,published:bool"
```

This creates **10 files** across all layers:

| Layer | File | What it does |
|---|---|---|
| Domain | `internal/domain/post.go` | Entity struct + repository/service interfaces |
| Handler | `internal/handler/post_handler.go` | HTTP CRUD endpoints |
| Handler Test | `internal/handler/post_handler_test.go` | Handler unit tests with mocks |
| Service | `internal/service/post_service.go` | Business logic |
| Service Test | `internal/service/post_service_test.go` | Service unit tests |
| Repository | `internal/repository/post_repo.go` | Ent data access |
| Ent Schema | `ent/schema/post.go` | Database schema definition |
| Migration | `db/migrations/002_create_posts.sql` | SQL migration |

---

## 4. Run the Server

```bash
# Start dependencies (PostgreSQL + Redis)
docker compose up -d

# Run migrations
go run cmd/api/main.go migrate

# Start the server
go run cmd/api/main.go
```

Your API is now running at `http://localhost:8080`.

### Test it:
```bash
# Create a post
curl -X POST http://localhost:8080/api/v1/posts \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello World","body":"My first post","published":true}'

# List posts
curl http://localhost:8080/api/v1/posts

# Health check
curl http://localhost:8080/health
```

---

## 5. Add a Plugin

Axe has a plugin system for optional features. Example: add file storage.

```bash
axe plugin add storage
```

This wires the storage plugin into your project. Use it in your service:

```go
store := plugin.MustResolve[storage.Store](app, storage.ServiceKey)
err := store.Save(ctx, "uploads/photo.jpg", fileData)
```

See [Plugin Maturity Tiers](../plugin-maturity.md) for which plugins are production-ready.

---

## 6. Understand the Architecture

### Layer Rules (enforced by Go compiler)

```
domain/     → Pure Go types + interfaces. ZERO infrastructure imports.
handler/    → Parse HTTP → call service → write response. NO business logic.
service/    → Business rules, transactions, authorization. NO HTTP concerns.
repository/ → Database access via Ent or sqlc. NO business logic.
```

### Key Design Decisions

| Decision | Choice | Why |
|---|---|---|
| Router | Chi | Stdlib `net/http` compatible, middleware ecosystem |
| ORM | Ent (or sqlc) | Schema-as-code, compile-time safety. **Choose one per project.** |
| Config | Cleanenv | Struct-based, env vars, no YAML magic |
| Background jobs | Asynq | Redis-backed, reliable, dashboard included |
| WebSocket | nhooyr.io/websocket | Stdlib-friendly, no gorilla dependency |

### Error Handling

Errors flow upward through layers with increasing context:

```
Repository  →  fmt.Errorf("create order: %w", err)
Service     →  apperror.ErrNotFound.WithMessage("order not found")
Handler     →  Central middleware maps AppError → HTTP status + JSON
```

You never manually set HTTP status codes in handlers — the error middleware does it.

---

## 7. Deploy

### Docker

```bash
docker build -t my-api .
docker run -p 8080:8080 \
  -e DATABASE_URL=postgres://... \
  -e REDIS_ADDR=redis:6379 \
  my-api
```

### Health Endpoints

| Endpoint | Purpose | Use for |
|---|---|---|
| `GET /health` | Liveness probe | Kubernetes `livenessProbe` |
| `GET /ready` | Readiness probe (DB + Redis) | Kubernetes `readinessProbe` |
| `GET /metrics` | Prometheus metrics | Monitoring |

---

## Next Steps

- **[Architecture Contract](../architecture_contract.md)** — The full rules document
- **[Plugin Guide](../plugin-guide.md)** — Deep dive into the plugin system
- **[WebSocket Semantics](websocket-semantics.md)** — Message ordering and delivery guarantees
- **[Data Consistency](../data_consistency.md)** — Transaction patterns and outbox
- **[Plugin Maturity](../plugin-maturity.md)** — Which plugins are production-ready

---

*Last updated: 2026-04-20*
