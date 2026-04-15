# Epic 1: Foundation

**Status**: In Progress  
**Goal**: "Hello World" production-grade hoạt động end-to-end  
**Estimated**: 4-6 tuần  

---

## Story 1.1: Project Scaffold

**Status**: `todo`

**Goal**: Tạo folder structure, go.mod, và skeleton files.

**Acceptance Criteria**:
- [ ] `go.mod` với module `github.com/axe-go/axe`, Go 1.22
- [ ] Folder structure đúng theo architecture.md
- [ ] `.env.example` với tất cả config keys
- [ ] `.gitignore` đúng cho Go project
- [ ] `Makefile` skeleton với targets: `run`, `test`, `lint`, `generate`, `migrate-up`, `migrate-down`, `docker-up`, `docker-down`, `seed`
- [ ] `README.md` với local setup guide
- [ ] `go build ./...` pass

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

**Status**: `todo`

**Goal**: Error taxonomy dùng trong toàn bộ codebase.

**Acceptance Criteria**:
- [ ] `pkg/apperror/apperror.go`: `AppError` struct với Code, Message, Cause, HTTPStatus
- [ ] Sentinel errors: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrForbidden, ErrInternal, ErrConflict
- [ ] Methods: `WithMessage(string)`, `WithCause(error)`, `Is(error) bool`
- [ ] `pkg/apperror/apperror_test.go`: unit tests đủ cases
- [ ] `go test ./pkg/apperror/...` pass

---

## Story 1.3: pkg/txmanager

**Status**: `todo`

**Goal**: Transaction manager inject-via-context.

**Acceptance Criteria**:
- [ ] `pkg/txmanager/txmanager.go`: `TxManager` interface
- [ ] `pgxTxManager` implementation: BeginTx → inject ctx → commit/rollback
- [ ] `injectTx(ctx, tx)` và `extractTxOrDB(ctx, db)` helpers
- [ ] Unit tests với mock DB (không cần real DB)
- [ ] `go test ./pkg/txmanager/...` pass

---

## Story 1.4: pkg/logger

**Status**: `todo`

**Goal**: Structured logging với request ID propagation.

**Acceptance Criteria**:
- [ ] `pkg/logger/logger.go`: slog-based, JSON output in production
- [ ] `FromCtx(ctx)` lấy logger từ context (có sẵn request_id)
- [ ] `WithRequestID(ctx, requestID)` inject vào context
- [ ] Log levels: Debug, Info, Warn, Error
- [ ] Unit tests
- [ ] `go test ./pkg/logger/...` pass

---

## Story 1.5: config/

**Status**: `todo`

**Goal**: Cleanenv-based config với validation.

**Acceptance Criteria**:
- [ ] `config/config.go`: struct với env tags, validation
- [ ] Fields: ServerPort, DatabaseURL, RedisURL, Environment, LogLevel, JWTSecret
- [ ] `Load() (*Config, error)` function
- [ ] Validation: required fields, port range
- [ ] `.env.example` hoàn chỉnh
- [ ] Unit tests với test env file

---

## Story 1.6: Chi router + middleware chain

**Status**: `todo`

**Goal**: HTTP server với middleware chuẩn.

**Acceptance Criteria**:
- [ ] Chi router setup trong `cmd/api/main.go`
- [ ] Middleware stack (theo thứ tự): Recovery → RequestID → Logger → ErrorHandler
- [ ] `RequestID` middleware: generate UUID, set X-Request-ID header
- [ ] `Logger` middleware: log method, path, status, latency dùng pkg/logger
- [ ] `ErrorHandler` middleware: map `*apperror.AppError` → JSON response chuẩn
- [ ] `Recovery` middleware: catch panic, return 500
- [ ] `/health` endpoint → `{"status": "ok"}`
- [ ] `/ready` endpoint → check DB connection
- [ ] Integration test với httptest

---

## Story 1.7: Ent schema setup (User entity)

**Status**: `todo`

**Goal**: Setup Ent và define User schema.

**Acceptance Criteria**:
- [ ] `go get entgo.io/ent` + `ent/schema/user.go`
- [ ] User schema: id(uuid), email(unique), name, password_hash, created_at, updated_at
- [ ] `go generate ./ent/...` tạo code thành công
- [ ] `sqlc.yaml` config file
- [ ] `db/queries/user.sql` với: GetUserByID, GetUserByEmail, ListUsers

---

## Story 1.8: pgx connection pool + sqlc

**Status**: `todo`

**Goal**: Database connection với shared pool.

**Acceptance Criteria**:
- [ ] `*sql.DB` với pgx driver, connection pool config (MaxOpen, MaxIdle, Lifetime)
- [ ] Ent client và sqlc queries dùng chung pool
- [ ] `sqlc generate` tạo code thành công
- [ ] `db/migrations/001_init.sql`: schema init
- [ ] Migration runner (Atlas hoặc raw SQL)
- [ ] Health check `/ready` verify DB connection thực

---

## Story 1.9: User domain — Reference Implementation

**Status**: `todo`

**Goal**: Full CRUD cho User domain — đây là reference cho mọi domain sau.

**Acceptance Criteria**:

**Domain layer**:
- [ ] `internal/domain/user.go`: `User` entity, `UserRepository` interface, `UserService` interface
- [ ] Không import infra packages

**Handler**:
- [ ] `internal/handler/user_handler.go`: POST, GET /:id, PUT /:id, DELETE /:id, GET (list)
- [ ] `internal/handler/user_handler_test.go`: test mọi endpoint, mock service

**Service**:
- [ ] `internal/service/user_service.go`: CreateUser, GetUser, UpdateUser, DeleteUser, ListUsers
- [ ] `internal/service/user_service_test.go`: mock repo, test happy + error paths

**Repository**:
- [ ] `internal/repository/user_repo.go`: Ent writes (Create, Update, Delete, GetByID)
- [ ] `internal/repository/user_query.go`: sqlc reads (List with pagination)
- [ ] Extract tx from context

**Wiring**:
- [ ] Register routes: `r.Mount("/api/v1/users", userHandler.Routes())`
- [ ] `go test ./...` full pass

**Manual test**:
```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@example.com","name":"Test User","password":"secret123"}'
# → 201 Created với JSON body
```

---

## Story 1.10: Docker Compose + Makefile hoàn chỉnh

**Status**: `todo`

**Goal**: `make run` hoạt động trong < 2 phút từ zero.

**Acceptance Criteria**:
- [ ] `docker-compose.yml`: PostgreSQL 16 + Redis 7 với health checks
- [ ] Multi-stage `Dockerfile`: builder → minimal final (< 20MB)
- [ ] `Makefile` với tất cả targets từ Story 1.1
- [ ] `make docker-up && make migrate-up && make run` → server up
- [ ] `make test` chạy < 30 giây
- [ ] `make seed` load test data (1 admin user)
- [ ] `README.md` hoàn chỉnh với setup guide
