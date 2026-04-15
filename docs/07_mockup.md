# 🗺️ Architecture Mockup
> Bản thiết kế tổng thể của nền tảng axe —
> từ request đến database và background jobs.

---

## 1. Tổng Quan Kiến Trúc

```
┌─────────────────────────────────────────────────────────────┐
│                        CLIENT                                │
│         (Browser / Mobile App / External Service)           │
└─────────────────────┬───────────────────────────────────────┘
                      │ HTTPS
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   MIDDLEWARE CHAIN                           │
│   RequestID → Logger → RateLimiter → Auth(JWT) → CORS       │
│              (Chi Router — pure net/http)                    │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   HANDLER LAYER                              │
│              internal/handler/*.go                           │
│                                                             │
│  Parse HTTP → Validate Input → Call Service → Write Response │
│  (No DB calls, No business logic)                           │
└─────────────────────┬───────────────────────────────────────┘
                      │ interface call
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   SERVICE LAYER                              │
│              internal/service/*.go                           │
│                                                             │
│  Business Rules → Authorization Check → TxManager.WithinTx  │
│  → Call Repository Interface(s) → Append Outbox Event       │
└──────────┬───────────────────────────┬──────────────────────┘
           │ write (Ent)               │ read (sqlc)
           ▼                           ▼
┌────────────────────┐    ┌────────────────────────────────────┐
│  REPOSITORY LAYER  │    │       QUERY LAYER                  │
│  internal/repo/*.go│    │    internal/repository/*_query.go  │
│                    │    │                                    │
│  Ent Client        │    │  sqlc generated functions          │
│  (Mutations)       │    │  (Analytics, Search, Reports)      │
└────────┬───────────┘    └──────────────┬─────────────────────┘
         │                               │
         └───────────────┬───────────────┘
                         │ pgx driver (shared *sql.DB pool)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                     PostgreSQL                               │
│                                                             │
│  Tables + Indexes + Constraints                             │
│  outbox_events (for async consistency)                      │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Composition Root (cmd/api/main.go)

```
main.go — Gốc Lắp Ráp (Composition Root)
│
├── Load Config (Cleanenv → typed Config struct)
│
├── Connect Database (pgx → *sql.DB → shared pool)
│   ├── ent.NewClient(ent.Driver(db))   → entClient
│   └── sqlc.New(db)                   → queries
│
├── Connect Redis (Asynq client)
│
├── Build Infrastructure:
│   ├── txmanager.New(db)             → txMgr
│   ├── logger.New(config.LogLevel)   → log
│   └── cache.NewRedis(redisClient)   → cache
│
├── Build Repositories:
│   ├── repository.NewUserRepo(entClient, queries)
│   ├── repository.NewOrderRepo(entClient, queries)
│   └── repository.NewOutboxRepo(entClient)
│
├── Build Services:
│   ├── service.NewUserService(userRepo, txMgr, log)
│   └── service.NewOrderService(orderRepo, inventoryRepo, outboxRepo, txMgr, log)
│
├── Build Handlers:
│   ├── handler.NewUserHandler(userSvc)
│   └── handler.NewOrderHandler(orderSvc)
│
├── Setup Router (Chi):
│   ├── Middleware chain (global)
│   ├── /api/v1/users  → userHandler
│   └── /api/v1/orders → orderHandler
│
├── Start HTTP Server
└── Start Background Workers (Asynq)
```

---

## 3. Request Lifecycle — Tường Minh

```
POST /api/v1/orders
│
├── [Middleware] RequestID: inject uuid vào context
├── [Middleware] Logger: log request start với request_id
├── [Middleware] RateLimiter: check rate limit
├── [Middleware] Auth: validate JWT → inject userID vào context
│
├── [Handler] orderHandler.Create(w, r)
│   ├── json.Decode(r.Body) → CreateOrderRequest{}
│   ├── validate.Struct(req) → 400 nếu invalid
│   └── orderSvc.PlaceOrder(ctx, userID, req) → ...
│
├── [Service] orderSvc.PlaceOrder(ctx, userID, input)
│   ├── Check businessrule (user active? có credit không?)
│   └── txMgr.WithinTransaction(ctx, func(ctx) error {
│       ├── orderRepo.Create(ctx, order)      ← Ent
│       ├── inventoryRepo.Deduct(ctx, items)  ← Ent
│       └── outboxRepo.Append(ctx,            ← Ent (same tx!)
│               OrderPlacedEvent{...})
│   })
│
├── [Repository] postgresOrderRepo.Create(ctx, order)
│   └── entClient.Order.Create()...Save(ctx)
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
│   ├── type UserRepository interface { ... }
│   └── type UserService interface { ... }
│
├── order.go
│   ├── type Order struct { ID, UserID, Status, TotalPrice, Items }
│   ├── type OrderStatus string (const: Pending, Confirmed, Shipped)
│   ├── type OrderRepository interface { ... }
│   └── type OrderService interface { ... }
│
└── events.go
    ├── type OrderPlacedEvent struct { OrderID, UserID, Amount }
    └── type UserRegisteredEvent struct { UserID, Email }

RULES (compiler-enforced via linter):
  ✅ Only standard library imports
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
  return fmt.Errorf(          return apperror.        switch apperror.Code(err):
    "create order: %w",         ErrNotFound.          case "NOT_FOUND":
    err)                        WithCause(repoErr)      w.WriteHeader(404)
                                                      case "INVALID_INPUT":
                                                        w.WriteHeader(400)
                                                      default:
                                                        w.WriteHeader(500)
                                                        log.Error(ctx, err)
```

---

## 6. Data Layer — Ent + sqlc Coexistence

```
Shared connection:
  db := sql.Open("pgx", config.DatabaseURL)
  db.SetMaxOpenConns(25)
  db.SetMaxIdleConns(5)
  db.SetConnMaxLifetime(5 * time.Minute)

  entClient := ent.NewClient(ent.Driver(db))  ← Write model
  queries   := sqlc.New(db)                   ← Read model

Write paths (use Ent):
  ├── User CRUD
  ├── Order placement
  ├── Inventory updates
  └── All transactional mutations

Read paths (use sqlc):
  ├── Dashboard: orders_per_day, revenue_by_category
  ├── Search: full-text search với GIN index
  ├── Reports: complex joins, aggregations
  └── Paginated lists với cursor-based pagination
```

---

## 7. Background Jobs Architecture

```
┌────────────────┐   Outbox Poll    ┌──────────────────────┐
│  PostgreSQL    │ ──────────────►  │   Outbox Publisher   │
│  outbox_events │  (every 1s)      │   (goroutine)        │
└────────────────┘                  └──────────┬───────────┘
                                               │ Enqueue
                                               ▼
                                    ┌──────────────────────┐
                                    │   Redis (Asynq)      │
                                    │   Task Queue         │
                                    └──────────┬───────────┘
                                               │
                          ┌────────────────────┼────────────────────┐
                          ▼                    ▼                    ▼
               ┌─────────────────┐  ┌──────────────────┐  ┌──────────────────┐
               │  Email Worker   │  │  Analytics Worker│  │  Notify Worker   │
               │  (send welcome) │  │  (update stats)  │  │  (push notif)    │
               └─────────────────┘  └──────────────────┘  └──────────────────┘

Retry policy: exponential backoff (1s, 5s, 30s, 5m, 30m)
Dead letter: after 5 retries → dead_tasks table + alert
```

---

## 8. Project Directory Snapshot

```
axe/
├── cmd/
│   ├── api/
│   │   └── main.go               # HTTP server entry point
│   └── worker/
│       └── main.go               # Background worker entry point
│
├── internal/
│   ├── domain/                   # Entities + Interfaces (NO external imports)
│   │   ├── user.go
│   │   ├── order.go
│   │   └── events.go
│   ├── handler/                  # HTTP handlers (Chi)
│   │   ├── user_handler.go
│   │   ├── user_handler_test.go
│   │   └── order_handler.go
│   ├── service/                  # Business logic
│   │   ├── user_service.go
│   │   ├── user_service_test.go
│   │   └── order_service.go
│   └── repository/               # Data access
│       ├── user_repo.go          # Ent (writes)
│       ├── user_query.go         # sqlc (reads)
│       └── outbox_repo.go        # Outbox appender
│
├── pkg/                          # Shared, reusable packages
│   ├── apperror/                 # Error taxonomy
│   │   └── apperror.go
│   ├── txmanager/                # Transaction manager
│   │   └── txmanager.go
│   ├── logger/                   # Structured logging (slog)
│   │   └── logger.go
│   ├── middleware/               # Chi middlewares
│   │   ├── auth.go
│   │   ├── logger.go
│   │   └── requestid.go
│   └── validator/                # Input validation
│       └── validator.go
│
├── ent/                          # Ent ORM schemas + generated code
│   ├── schema/
│   │   ├── user.go
│   │   └── order.go
│   └── generate.go               # go:generate directive
│
├── db/
│   ├── migrations/               # SQL migration files
│   │   ├── 20260101_create_users.sql
│   │   └── 20260102_create_orders.sql
│   └── queries/                  # sqlc raw SQL queries
│       ├── user.sql
│       └── order.sql
│
├── config/
│   └── config.go                 # Cleanenv typed config
│
├── docs/                         # This docs folder
│   ├── 01_bright_spots.md
│   ├── 02_blind_spots.md
│   ├── 03_murky_areas.md
│   ├── 04_invalid_points.md
│   ├── 05_plans.md
│   ├── 06_ai_skills.md
│   ├── 07_mockup.md              # This file
│   ├── architecture_contract.md
│   ├── data_consistency.md
│   └── adr/                      # Architecture Decision Records
│       └── ADR-001-no-magic.md
│
├── Makefile                      # make run, test, migrate, generate
├── docker-compose.yml            # PostgreSQL + Redis + App
└── sqlc.yaml                     # sqlc configuration
```

---

## 9. Tech Stack Summary

| Category | Choice | Rationale |
|---|---|---|
| Language | Go 1.22+ | Static typing, goroutines, explicit |
| HTTP Router | Chi | Pure net/http, no vendor lock-in |
| ORM (writes) | Ent | Compile-time safe, schema migrations |
| Query gen (reads) | sqlc | SQL-first, zero runtime magic |
| DB Driver | pgx v5 | Native PostgreSQL, performance |
| DI | Manual + Wire (if needed) | Compile-time, no reflection |
| Config | Cleanenv | 12-Factor, env vars only |
| Logging | slog (stdlib) | Structured, no dependency |
| Background jobs | Asynq | Redis-backed, Sidekiq-like |
| Consistency | Outbox Pattern | Atomic DB + Queue |
| Transactions | TxManager interface | Unit of Work |
| Auth | JWT (golang-jwt) | Stateless, standard |
| Testing (unit) | testify + gomock | Interface mocking |
| Testing (integration) | testcontainers-go | Real PostgreSQL in Docker |
| Migration | Atlas (Ent) + raw SQL | Ent migrations + manual SQL |
| Database | PostgreSQL 16 | Production standard |
| Cache | Redis 7 | Session, cache-aside |
| Deployment | Docker + GitHub Actions | Cloud-native |
