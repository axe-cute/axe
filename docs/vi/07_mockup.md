# 🗺️ Bản Thiết Kế Kiến Trúc (Architecture Mockup)
> Bản thiết kế tổng thể của nền tảng axe —
> từ request đến database và background jobs.
>
> 🇬🇧 [English version](../07_mockup.md)

---

## 1. Tổng Quan Kiến Trúc

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
│   Parse HTTP → Validate → Gọi Service → Trả Response       │
│   (Không gọi DB trực tiếp, không chứa business logic)      │
└─────────────────────┬───────────────────────────────────────┘
                      │ gọi qua interface
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   SERVICE LAYER                              │
│              internal/service/*.go                           │
│   Business Rules → Authorization → TxManager.WithinTx       │
│   → Gọi Repository → Append Outbox Event                   │
└──────────┬───────────────────────────┬──────────────────────┘
           │ write (Ent)               │ read (sqlc)
           ▼                           ▼
┌────────────────────┐    ┌────────────────────────────────────┐
│  REPOSITORY LAYER  │    │       QUERY LAYER                  │
│  Ent Client        │    │  sqlc generated functions          │
│  (Mutations)       │    │  (Analytics, Search, Reports)      │
└────────┬───────────┘    └──────────────┬─────────────────────┘
         └───────────────┬───────────────┘
                         │ pgx v5 driver (shared *sql.DB pool)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│           PostgreSQL / MySQL / SQLite                        │
│           (pluggable — pkg/db adapter interface)             │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Composition Root (cmd/api/main.go)

```
main.go — Gốc Lắp Ráp (Composition Root)
│
├── Load Config (Cleanenv → Config struct có type)
│
├── Kết nối Database (pluggable adapter — pkg/db)
│   ├── pkg/db/postgres/ → PostgreSQL (pgx v5)
│   ├── pkg/db/mysql/    → MySQL 8+
│   └── pkg/db/sqlite/   → SQLite (pure Go, không CGO)
│
├── Kết nối Redis
│   ├── Cache client (pkg/cache)
│   └── Asynq client (pkg/worker)
│
├── Khởi tạo Infrastructure:
│   ├── txmanager.New(db)              → txMgr
│   ├── logger.New(config.LogLevel)    → log (slog)
│   ├── cache.NewRedis(redisClient)    → cache
│   ├── jwtauth.New(config.JWTSecret)  → jwt
│   ├── metrics.NewPrometheus()        → metrics
│   └── ratelimit.New(redisClient)     → limiter
│
├── Khởi tạo Repositories:
│   ├── repository.NewUserRepo(entClient)
│   ├── repository.NewPostRepo(entClient)
│   └── repository.NewOutboxRepo(entClient)
│
├── Khởi tạo Services:
│   ├── service.NewUserService(userRepo, txMgr, log)
│   └── service.NewPostService(postRepo, txMgr, log)
│
├── Khởi tạo Handlers:
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
│   ├── /upload/*       → Storage Handler [optional]
│   ├── /health, /ready, /metrics, /debug/routes
│   └── /docs, /docs/redoc, /openapi.yaml
│
├── Start HTTP Server
├── Start Background Workers (Asynq)
└── Start Outbox Poller
```

---

## 3. Vòng Đời Request — Tường Minh

```
POST /api/v1/posts
│
├── [Middleware] RequestID: inject uuid vào context
├── [Middleware] Logger: log request start với request_id
├── [Middleware] RateLimiter: check Redis sliding-window
├── [Middleware] Auth: validate JWT → inject userID vào context
│
├── [Handler] postHandler.Create(w, r)
│   ├── json.Decode(r.Body) → CreatePostRequest{}
│   ├── validate(req) → 400 nếu invalid
│   └── postSvc.Create(ctx, userID, req) → ...
│
├── [Service] postSvc.Create(ctx, userID, input)
│   ├── Check business rules
│   └── txMgr.WithinTransaction(ctx, func(ctx) error {
│       ├── postRepo.Create(ctx, post)    ← Ent
│       └── outboxRepo.Append(ctx,        ← cùng transaction!
│               PostCreatedEvent{...})
│   })
│
├── [Repository] postRepo.Create(ctx, post)
│   └── entClient.Post.Create()...Save(ctx)
│
├── [Outbox Poller - Background]
│   ├── Đọc outbox_events chưa xử lý
│   ├── Publish vào Asynq queue
│   └── Đánh dấu đã xử lý
│
└── [Handler] return 201 Created + JSON response
```

---

## 4. WebSocket (pkg/ws)

```
┌────────────────────────────────────────────────────────────┐
│  Client → WS Connect (JWT auth qua query/header)          │
│         → pkg/ws/auth.go: WSAuth middleware                │
└──────────────┬─────────────────────────────────────────────┘
               ▼
┌──────────────────────────────────────────────────┐
│             pkg/ws/hub.go                         │
│  Hub { rooms map[string]*Room, register/unreg }   │
│                                                   │
│  Adapter interface:                               │
│    ├── MemoryAdapter (single instance)            │
│    └── RedisAdapter  (multi-instance pub/sub)     │
└───────┬──────────────────────┬────────────────────┘
        ▼                      ▼
┌──────────────┐    ┌──────────────────────────┐
│  Room        │    │  Client                  │
│  (broadcast) │    │  (read/write goroutines) │
└──────────────┘    └──────────────────────────┘

Prometheus: ws_connections_total, ws_messages_total, ws_rooms_active
```

---

## 5. Cấu Trúc Thư Mục Thực Tế

```
axe/
├── cmd/
│   ├── api/main.go                    # HTTP server (Composition Root)
│   └── axe/                           # CLI tool
│       ├── main.go                    #   CLI entry point
│       ├── new/                       #   axe new (scaffold)
│       ├── generate/                  #   axe generate resource
│       ├── migrate/                   #   axe migrate up/down
│       └── plugin/                    #   axe plugin add
│
├── internal/
│   ├── domain/                        # Entities + Interfaces ONLY
│   │   ├── user.go, post.go, pagination.go
│   ├── handler/                       # HTTP layer (Chi)
│   │   ├── user_handler.go, post_handler.go, auth_handler.go
│   │   └── middleware/ (auth, logger, recovery, CORS)
│   ├── service/                       # Business logic
│   │   ├── user_service.go, post_service.go
│   └── repository/                    # Data access (Ent writes)
│       ├── user_repo.go, post_repo.go
│
├── pkg/
│   ├── apperror/    # Error taxonomy (NotFound, Forbidden, Conflict...)
│   ├── cache/       # Redis cache client
│   ├── db/          # Multi-DB adapters (postgres, mysql, sqlite)
│   ├── devroutes/   # Rails-like route listing (dev mode)
│   ├── jwtauth/     # JWT issue / verify / refresh
│   ├── logger/      # Structured slog wrapper (JSON in prod)
│   ├── metrics/     # Prometheus middleware + /metrics
│   ├── outbox/      # Outbox event poller → Asynq
│   ├── plugin/      # Plugin system + storage plugin
│   ├── ratelimit/   # Redis sliding-window rate limiter
│   ├── txmanager/   # Transaction manager (Unit of Work)
│   ├── worker/      # Asynq background worker
│   └── ws/          # WebSocket (hub, client, room, auth, redis adapter)
│
├── ent/schema/      # Ent ORM schemas
├── db/migrations/   # SQL migration files
├── config/          # Cleanenv typed config
├── benchmarks/      # Framework benchmarks (vs Gin/Echo/Fiber)
├── docs/            # Tài liệu (EN + VI)
│   ├── vi/          #   Phiên bản tiếng Việt
│   └── *.md         #   Phiên bản tiếng Anh
└── docker-compose.yml
```

---

## 6. Tech Stack (Thực Tế)

| Hạng mục | Lựa chọn | Lý do |
|---|---|---|
| Ngôn ngữ | Go 1.22+ | Static typing, goroutines, tường minh |
| HTTP Router | Chi v5 | Pure net/http, nhanh nhất ([benchmarks](../benchmarks/)) |
| ORM (writes) | Ent | Compile-time safe, schema migrations |
| Query gen (reads) | sqlc | SQL-first, zero runtime magic |
| DB Driver | pgx v5 | Native PostgreSQL, hiệu năng tốt nhất |
| Multi-DB | pkg/db adapters | PostgreSQL, MySQL, SQLite |
| Config | Cleanenv | 12-Factor, chỉ env vars |
| Logging | slog (stdlib) | Structured JSON, không dependency |
| Auth | JWT (golang-jwt) | Access + refresh tokens, Redis blocklist |
| Cache | Redis | Cache-aside, rate limiter, WS pub/sub |
| Background Jobs | Asynq | Redis-backed queues, Asynqmon dashboard |
| Consistency | Outbox Pattern | Atomic DB + queue publish |
| Transactions | TxManager | Unit of Work qua context |
| WebSocket | nhooyr.io/websocket | Hub/Client/Room, Redis adapter |
| File Storage | FSStore plugin | POSIX, local + JuiceFS |
| Metrics | Prometheus | /metrics endpoint |
| Testing | testify + httptest | Interface mocking, testcontainers-go |
| CI | GitHub Actions | Multi-DB matrix, lint, race, coverage |
| Deployment | Docker | Multi-stage build, < 20MB image |
