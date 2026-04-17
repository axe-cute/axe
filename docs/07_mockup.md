# 🗺️ Architecture Mockup
> Complete blueprint of the axe platform —
> from HTTP request to database and background jobs.
>
> 🇻🇳 [Phiên bản tiếng Việt](vi/07_mockup.md)

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        CLIENT                               │
│         (Browser / Mobile App / External Service)           │
└─────────────────────┬───────────────────────────────────────┘
                      │ HTTPS
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   MIDDLEWARE CHAIN                           │
│   RequestID → Logger → RateLimiter → Auth(JWT) → CORS       │
│              (Chi v5 Router — pure net/http)                 │
└─────────────────────┬───────────────────────────────────────┘
                      │
           ┌──────────┼──────────┐
           ▼          ▼          ▼
      REST API    WebSocket   File Storage
      /api/v1/*   /ws/*       /upload/*
           │          │          │
           ▼          ▼          ▼
┌─────────────────────────────────────────────────────────────┐
│                   HANDLER LAYER                              │
│              internal/handler/*.go                           │
│   Parse HTTP → Validate Input → Call Service → Write Response│
│   (No DB calls, No business logic)                          │
└─────────────────────┬───────────────────────────────────────┘
                      │ interface call
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   SERVICE LAYER                              │
│              internal/service/*.go                           │
│   Business Rules → Authorization → TxManager.WithinTx       │
│   → Call Repository → Append Outbox Event                   │
└──────────┬───────────────────────────┬──────────────────────┘
           │ write (Ent)               │ read (sqlc)
           ▼                           ▼
┌────────────────────┐    ┌────────────────────────────────────┐
│  REPOSITORY LAYER  │    │       QUERY LAYER                  │
│  internal/repo/*.go│    │    internal/repository/*_query.go  │
│  Ent Client        │    │  sqlc generated functions          │
│  (Mutations)       │    │  (Analytics, Search, Reports)      │
└────────┬───────────┘    └──────────────┬─────────────────────┘
         └───────────────┬───────────────┘
                         │ pgx v5 driver (shared *sql.DB pool)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│           PostgreSQL / MySQL / SQLite                        │
│           (pluggable via pkg/db adapter)                     │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Composition Root (cmd/api/main.go)

```
main.go — Composition Root
│
├── Load Config (Cleanenv → typed Config struct)
│
├── Connect Database (pluggable adapter via pkg/db)
│   ├── pkg/db/postgres/adapter.go  → PostgreSQL (pgx v5)
│   ├── pkg/db/mysql/adapter.go     → MySQL 8+
│   └── pkg/db/sqlite/adapter.go    → SQLite (pure Go, no CGO)
│
├── Connect Redis
│   ├── Cache client (pkg/cache)
│   └── Asynq client (pkg/worker)
│
├── Build Infrastructure:
│   ├── txmanager.New(db)                 → txMgr
│   ├── logger.New(config.LogLevel)       → log (slog)
│   ├── cache.NewRedis(redisClient)       → cache
│   ├── jwtauth.New(config.JWTSecret)     → jwt
│   ├── metrics.NewPrometheus()           → metrics
│   └── ratelimit.New(redisClient)        → limiter
│
├── Build Repositories:
│   ├── repository.NewUserRepo(entClient)
│   ├── repository.NewPostRepo(entClient)
│   └── repository.NewOutboxRepo(entClient)
│
├── Build Services:
│   ├── service.NewUserService(userRepo, txMgr, log)
│   └── service.NewPostService(postRepo, txMgr, log)
│
├── Build Handlers:
│   ├── handler.NewUserHandler(userSvc)
│   ├── handler.NewPostHandler(postSvc)
│   └── handler.NewAuthHandler(userSvc, jwt)
│
├── Setup Router (Chi v5):
│   ├── Middleware: RequestID, Logger, Recoverer, RateLimiter, CORS
│   ├── /api/v1/users   → userHandler
│   ├── /api/v1/posts   → postHandler
│   ├── /api/v1/auth    → authHandler
│   ├── /ws/*           → WebSocket Hub (pkg/ws)
│   ├── /upload/*       → Storage Handler (pkg/plugin/storage) [optional]
│   ├── /health         → liveness
│   ├── /ready          → readiness (DB + Redis)
│   ├── /metrics        → Prometheus
│   └── /debug/routes   → devroutes (dev mode)
│
├── Start HTTP Server
├── Start Background Workers (Asynq)
└── Start Outbox Poller
```

---

## 3. Request Lifecycle

```
POST /api/v1/posts
│
├── [Middleware] RequestID: inject uuid into context
├── [Middleware] Logger: log request start with request_id
├── [Middleware] RateLimiter: check Redis sliding-window
├── [Middleware] Auth: validate JWT → inject userID into context
│
├── [Handler] postHandler.Create(w, r)
│   ├── json.Decode(r.Body) → CreatePostRequest{}
│   ├── validate(req) → 400 if invalid
│   └── postSvc.Create(ctx, userID, req) → ...
│
├── [Service] postSvc.Create(ctx, userID, input)
│   ├── Check business rules
│   └── txMgr.WithinTransaction(ctx, func(ctx) error {
│       ├── postRepo.Create(ctx, post)           ← Ent
│       └── outboxRepo.Append(ctx,               ← same tx!
│               PostCreatedEvent{...})
│   })
│
├── [Repository] postRepo.Create(ctx, post)
│   └── entClient.Post.Create()...Save(ctx)
│
├── [Outbox Poller - Background]
│   ├── Read unprocessed outbox_events
│   ├── Publish to Asynq queue
│   └── Mark as processed
│
└── [Handler] return 201 Created + JSON response
```

---

## 4. Domain Layer Structure

```
internal/domain/
├── user.go
│   ├── type User struct { ID, Email, Name, Role, CreatedAt }
│   ├── type UserRepository interface { Create, FindByID, List, Update, Delete }
│   └── type UserService interface { ... }
│
├── post.go
│   ├── type Post struct { ID, Title, Body, Published, AuthorID }
│   ├── type PostRepository interface { Create, FindByID, List, Update, Delete }
│   └── type PostService interface { ... }
│
└── pagination.go
    └── type Pagination struct { Page, PageSize, Total }

RULES (compiler-enforced):
  ✅ Only standard library imports (time, strings, errors, fmt, context)
  ✅ Only uuid for ID types
  ❌ No database imports
  ❌ No framework imports
  ❌ No logging imports
```

---

## 5. Error Flow

```
Repository:                 Service:                Handler:
  DB error                    Repo error              Svc error
     │                           │                       │
     ▼                           ▼                       ▼
  return fmt.Errorf(          return apperror.        switch on apperror type:
    "create post: %w",         ErrNotFound.          case NotFound → 404
    err)                       WithCause(repoErr)    case InvalidInput → 400
                                                     case Unauthorized → 401
                                                     case Forbidden → 403
                                                     default → 500 + log
```

---

## 6. Data Layer — Ent + sqlc + Multi-DB

```
Pluggable DB adapter (pkg/db):
  ┌──────────────────────────────────────────────┐
  │  pkg/db/adapter.go — interface Adapter {     │
  │      Open() (*sql.DB, error)                 │
  │      Driver() string                         │
  │  }                                           │
  ├──────────────────────────────────────────────┤
  │  postgres/ → pgx v5 (production default)     │
  │  mysql/    → MySQL 8+ driver                 │
  │  sqlite/   → modernc.org/sqlite (no CGO)     │
  └──────────────────────────────────────────────┘

Shared connection:
  adapter := postgres.New(config.DatabaseURL)
  db, _ := adapter.Open()                        ← shared *sql.DB pool
  entClient := ent.NewClient(ent.Driver(db))     ← Write model
  queries   := sqlc.New(db)                      ← Read model

Write paths (Ent):  CRUD mutations, relationships, transactions
Read paths (sqlc):  Dashboard, reports, analytics, complex joins, pagination
```

---

## 7. WebSocket Architecture (pkg/ws)

```
┌────────────────────────────────────────────────────────────┐
│  Client → WS Connect (JWT auth via query/header)           │
│         → pkg/ws/auth.go: WSAuth middleware                │
└──────────────┬─────────────────────────────────────────────┘
               ▼
┌──────────────────────────────────────────────────┐
│             pkg/ws/hub.go                         │
│  Hub { rooms map[string]*Room, register/unreg }   │
│                                                   │
│  Adapter interface (pkg/ws/adapter.go):           │
│    ├── MemoryAdapter (single instance)            │
│    └── RedisAdapter  (multi-instance pub/sub)     │
└───────┬──────────────────────┬────────────────────┘
        ▼                      ▼
┌──────────────┐    ┌──────────────────────────┐
│  Room        │    │  Client                  │
│  (broadcast) │    │  (per-connection, read/  │
│              │    │   write goroutines)      │
└──────────────┘    └──────────────────────────┘

Prometheus: ws_connections_total, ws_messages_total, ws_rooms_active
```

---

## 8. Background Jobs Architecture

```
┌────────────────┐   Outbox Poll    ┌──────────────────────┐
│  PostgreSQL    │ ──────────────►  │   Outbox Publisher   │
│  outbox_events │  (every 1s)      │   (pkg/outbox)       │
└────────────────┘                  └──────────┬───────────┘
                                               │ Enqueue
                                               ▼
                                    ┌──────────────────────┐
                                    │   Redis (Asynq)      │
                                    │   pkg/worker         │
                                    └──────────┬───────────┘
                                               │
                          ┌────────────────────┼──────────────┐
                          ▼                    ▼              ▼
               ┌──────────────┐    ┌──────────────┐  ┌──────────────┐
               │ Email Worker │    │ Analytics    │  │ Notify       │
               └──────────────┘    └──────────────┘  └──────────────┘

Retry: exponential backoff (1s, 5s, 30s, 5m, 30m)
Dead letter: after 5 retries → dead_tasks + alert
Dashboard: Asynqmon at :8081
```

---

## 9. Actual Project Directory

```
axe/
├── cmd/
│   ├── api/main.go                    # HTTP server (Composition Root)
│   └── axe/                           # CLI tool
│       ├── main.go                    #   CLI entry point
│       ├── new/                       #   axe new (scaffold)
│       │   ├── new.go                 #     flags, command
│       │   ├── scaffold.go            #     file creation logic
│       │   ├── templates.go           #     all Go templates
│       │   └── interactive.go         #     wizard mode
│       ├── generate/                  #   axe generate resource
│       │   ├── generate.go            #     codegen logic
│       │   └── templates.go           #     resource templates
│       ├── migrate/                   #   axe migrate up/down
│       │   └── migrate.go
│       └── plugin/                    #   axe plugin add
│           └── plugin.go
│
├── internal/
│   ├── domain/                        # Entities + Interfaces ONLY
│   │   ├── user.go
│   │   ├── post.go
│   │   └── pagination.go
│   ├── handler/                       # HTTP layer (Chi)
│   │   ├── user_handler.go
│   │   ├── post_handler.go
│   │   ├── auth_handler.go
│   │   ├── openapi_handler.go
│   │   └── middleware/
│   │       ├── middleware.go          #   Logger, Recovery, RequestID, CORS
│   │       └── auth.go               #   JWT + RBAC middleware
│   ├── service/                       # Business logic
│   │   ├── user_service.go
│   │   └── post_service.go
│   └── repository/                    # Data access (Ent writes)
│       ├── user_repo.go
│       └── post_repo.go
│
├── pkg/                               # Shared, reusable packages
│   ├── apperror/apperror.go           # Error taxonomy
│   ├── cache/cache.go                 # Redis cache client
│   ├── db/                            # Pluggable DB adapters
│   │   ├── adapter.go                 #   Adapter interface
│   │   ├── postgres/adapter.go        #   PostgreSQL (pgx v5)
│   │   ├── mysql/adapter.go           #   MySQL 8+
│   │   └── sqlite/adapter.go          #   SQLite (no CGO)
│   ├── devroutes/devroutes.go         # Rails-like route listing (dev mode)
│   ├── jwtauth/jwtauth.go             # JWT issue / verify / refresh
│   ├── logger/logger.go               # Structured slog wrapper
│   ├── metrics/metrics.go             # Prometheus middleware + /metrics
│   ├── outbox/poller.go               # Outbox event poller → Asynq
│   ├── plugin/                        # Plugin system
│   │   ├── plugin.go                  #   Plugin interface + App + typed locator
│   │   └── storage/                   #   File storage plugin
│   │       ├── storage.go             #     Store interface + Config
│   │       ├── fs_store.go            #     FSStore (POSIX filesystem)
│   │       ├── handler.go             #     HTTP handler (upload/serve/delete)
│   │       ├── plugin.go              #     Plugin registration
│   │       └── metrics.go             #     Prometheus counters
│   ├── ratelimit/ratelimit.go         # Redis sliding-window rate limiter
│   ├── txmanager/txmanager.go         # Transaction manager (Unit of Work)
│   ├── worker/worker.go               # Asynq background worker
│   └── ws/                            # WebSocket
│       ├── hub.go                     #   Hub with Room-based broadcasting
│       ├── client.go                  #   Per-connection client
│       ├── room.go                    #   Room abstraction
│       ├── auth.go                    #   WSAuth middleware
│       ├── adapter.go                 #   Adapter interface (Memory/Redis)
│       ├── redis_adapter.go           #   Redis Pub/Sub for multi-instance
│       └── metrics.go                 #   Prometheus counters
│
├── ent/schema/                        # Ent ORM schemas (user.go, post.go)
├── db/migrations/                     # SQL migration files
├── config/config.go                   # Cleanenv typed config
├── docs/                              # Documentation (EN + VI)
├── benchmarks/                        # Framework benchmarks (vs Gin/Echo/Fiber)
├── docker-compose.yml                 # PostgreSQL + Redis + Asynqmon
├── Makefile                           # make run, test, lint, migrate, setup
└── .env.example                       # Environment variable reference
```

---

## 10. Tech Stack (Actual)

| Category | Choice | Rationale |
|---|---|---|
| Language | Go 1.22+ | Static typing, goroutines, explicit |
| HTTP Router | Chi v5 | Pure net/http, fastest middleware ([benchmarks](../benchmarks/)) |
| ORM (writes) | Ent | Compile-time safe, schema migrations |
| Query gen (reads) | sqlc | SQL-first, zero runtime magic |
| DB Driver | pgx v5 | Native PostgreSQL, best performance |
| Multi-DB | pkg/db adapters | PostgreSQL, MySQL, SQLite |
| Config | Cleanenv | 12-Factor, env vars only |
| Logging | slog (stdlib) | Structured JSON, no dependency |
| Auth | JWT (golang-jwt) | Access + refresh tokens, Redis blocklist |
| Cache | Redis | Cache-aside, rate limiter, WS pub/sub |
| Background Jobs | Asynq | Redis-backed queues, Asynqmon dashboard |
| Consistency | Outbox Pattern | Atomic DB + queue publish |
| Transactions | TxManager | Unit of Work via context |
| WebSocket | nhooyr.io/websocket | Hub/Client/Room, Redis adapter |
| File Storage | FSStore plugin | POSIX, local + JuiceFS |
| Metrics | Prometheus | /metrics endpoint, per-package counters |
| Testing | testify + httptest | Interface mocking, testcontainers-go |
| CI | GitHub Actions | Multi-DB matrix, lint, race, coverage |
| Deployment | Docker | Multi-stage build, < 20MB image |
