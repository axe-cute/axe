# Epic 1: Foundation

**Status**: ✅ Done (Sprint 1–4)  
**Goal**: "Hello World" production-grade hoạt động end-to-end  
**Completed**: 2026-04-15  

> ⚠️ Source of truth cho status: `sprint-status.yaml`  

---

## Story 1.1: Project Scaffold

**Status**: ✅ Done | **Sprint**: 1

**Goal**: Tạo folder structure, go.mod, và skeleton files.

**Acceptance Criteria**:
- [x] `go.mod` với module `github.com/axe-cute/axe`, Go 1.22
- [x] Folder structure đúng theo architecture.md
- [x] `.env.example` với tất cả config keys
- [x] `.gitignore` đúng cho Go project
- [x] `Makefile` skeleton với targets: `run`, `test`, `lint`, `generate`, `migrate-up`, `migrate-down`, `docker-up`, `docker-down`, `seed`
- [x] `README.md` với local setup guide
- [x] `go build ./...` pass

**Output files**:
```
axe/
├── cmd/api/main.go       (minimal stub)
├── go.mod
├── go.sum
├── .env.example
├── .gitignore
├── Makefile
└── README.md
```

---

## Story 1.2: pkg/apperror

**Status**: ✅ Done | **Sprint**: 1

**Goal**: Error taxonomy dùng trong toàn bộ codebase.

**Acceptance Criteria**:
- [x] `pkg/apperror/apperror.go`: `AppError` struct với Code, Message, Cause, HTTPStatus
- [x] Sentinel errors: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrForbidden, ErrInternal, ErrConflict
- [x] Methods: `WithMessage(string)`, `WithCause(error)`, `Is(error) bool`
- [x] `pkg/apperror/apperror_test.go`: unit tests đủ cases
- [x] `go test ./pkg/apperror/...` pass

---

## Story 1.3: pkg/txmanager

**Status**: ✅ Done | **Sprint**: 1

**Goal**: Transaction manager inject-via-context.

**Acceptance Criteria**:
- [x] `pkg/txmanager/txmanager.go`: `TxManager` interface
- [x] `pgxTxManager` implementation: BeginTx → inject ctx → commit/rollback
- [x] `injectTx(ctx, tx)` và `extractTxOrDB(ctx, db)` helpers
- [x] Unit tests với mock DB (không cần real DB)
- [x] `go test ./pkg/txmanager/...` pass

---

## Story 1.4: pkg/logger

**Status**: ✅ Done | **Sprint**: 2

**Goal**: Structured logging với request ID propagation.

**Acceptance Criteria**:
- [x] `pkg/logger/logger.go`: slog-based, JSON output in production
- [x] `FromCtx(ctx)` lấy logger từ context (có sẵn request_id)
- [x] `WithRequestID(ctx, requestID)` inject vào context
- [x] Log levels: Debug, Info, Warn, Error
- [x] Unit tests
- [x] `go test ./pkg/logger/...` pass

---

## Story 1.5: config/

**Status**: ✅ Done | **Sprint**: 2

**Goal**: Cleanenv-based config với validation.

**Acceptance Criteria**:
- [x] `config/config.go`: struct với env tags, validation
- [x] Fields: ServerPort, DatabaseURL, RedisURL, Environment, LogLevel, JWTSecret
- [x] `Load() (*Config, error)` function
- [x] Validation: required fields, port range
- [x] `.env.example` hoàn chỉnh
- [x] Unit tests với test env file

---

## Story 1.6: Chi router + middleware chain

**Status**: ✅ Done | **Sprint**: 2

**Goal**: HTTP server với middleware chuẩn.

**Acceptance Criteria**:
- [x] Chi router setup trong `cmd/api/main.go`
- [x] Middleware stack (theo thứ tự): Recovery → RequestID → Logger → ErrorHandler
- [x] `RequestID` middleware: generate UUID, set X-Request-ID header
- [x] `Logger` middleware: log method, path, status, latency dùng pkg/logger
- [x] `ErrorHandler` middleware: map `*apperror.AppError` → JSON response chuẩn
- [x] `Recovery` middleware: catch panic, return 500
- [x] `/health` endpoint → `{"status": "ok"}`
- [x] `/ready` endpoint → check DB connection
- [x] Integration test với httptest

---

## Story 1.7: Ent schema setup (User entity)

**Status**: ✅ Done | **Sprint**: 3

**Goal**: Setup Ent và define User schema.

**Acceptance Criteria**:
- [x] `go get entgo.io/ent` + `ent/schema/user.go`
- [x] User schema: id(uuid), email(unique), name, password_hash, created_at, updated_at
- [x] `go generate ./ent/...` tạo code thành công
- [x] `sqlc.yaml` config file
- [x] `db/queries/user.sql` với: GetUserByID, GetUserByEmail, ListUsers

---

## Story 1.8: pgx connection pool + sqlc

**Status**: ✅ Done | **Sprint**: 3

**Goal**: Database connection với shared pool.

**Acceptance Criteria**:
- [x] `*sql.DB` với pgx driver, connection pool config (MaxOpen, MaxIdle, Lifetime)
- [x] Ent client và sqlc queries dùng chung pool
- [x] `sqlc generate` tạo code thành công
- [x] `db/migrations/001_init.sql`: schema init
- [x] Migration runner (Atlas hoặc raw SQL)
- [x] Health check `/ready` verify DB connection thực

---

## Story 1.9: User domain — Reference Implementation

**Status**: ✅ Done | **Sprint**: 3

**Goal**: Full CRUD cho User domain — đây là reference cho mọi domain sau.

**Acceptance Criteria**:

**Domain layer**:
- [x] `internal/domain/user.go`: `User` entity, `UserRepository` interface, `UserService` interface
- [x] Không import infra packages

**Handler**:
- [x] `internal/handler/user_handler.go`: POST, GET /:id, PUT /:id, DELETE /:id, GET (list)
- [x] `internal/handler/user_handler_test.go`: test mọi endpoint, mock service

**Service**:
- [x] `internal/service/user_service.go`: CreateUser, GetUser, UpdateUser, DeleteUser, ListUsers
- [x] `internal/service/user_service_test.go`: mock repo, test happy + error paths

**Repository**:
- [x] `internal/repository/user_repo.go`: Ent writes (Create, Update, Delete, GetByID)
- [x] `internal/repository/user_query.go`: sqlc reads (List with pagination)
- [x] Extract tx from context

**Wiring**:
- [x] Register routes: `r.Mount("/api/v1/users", userHandler.Routes())`
- [x] `go test ./...` full pass

**Manual test**:
```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@example.com","name":"Test User","password":"secret123"}'
# → 201 Created với JSON body
```

---

## Story 1.10: Docker Compose + Makefile hoàn chỉnh

**Status**: ✅ Done | **Sprint**: 4

**Goal**: `make run` hoạt động trong < 2 phút từ zero.

**Acceptance Criteria**:
- [x] `docker-compose.yml`: PostgreSQL 16 + Redis 7 với health checks
- [x] Multi-stage `Dockerfile`: builder → minimal final (< 20MB)
- [x] `Makefile` với tất cả targets từ Story 1.1
- [x] `make docker-up && make migrate-up && make run` → server up
- [x] `make test` chạy < 30 giây
- [x] `make seed` load test data (1 admin user)
- [x] `README.md` hoàn chỉnh với setup guide
