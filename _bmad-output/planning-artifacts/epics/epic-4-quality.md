# Epic 4: Quality & API Polish

**Status**: In Progress  
**Sprint**: 10–12  
**Goal**: Nâng chất lượng codebase từ "functional" lên "production-certifiable" — integration tests thực sự, API security hoàn chỉnh, developer tooling đầy đủ.

---

## Story 4.1 — Integration Tests với Testcontainers (Sprint 10) — P0

### Context
Unit tests hiện tại dùng mocks → không phát hiện lỗi DB, migration, hoặc wiring thực tế. Cần integration tests chạy với real PostgreSQL và real HTTP server.

### Technical Design
```
tests/integration/
   setup_test.go         ← TestMain: start Postgres container, migrate, seed
   user_test.go          ← CRUD via real HTTP + real DB
   auth_test.go          ← login → token → /auth/me
   migrate_test.go       ← all SQL files apply cleanly
```

**Testcontainers flow**:
```go
// TestMain
container := testcontainers.PostgresContainer(...)
defer container.Terminate()
// Apply migrations via axe migrate up
// Start chi router wired to real DB
// Run all tests
// Teardown container
```

**Build tag**: `//go:build integration` → `go test -tags=integration ./tests/integration/`

### Acceptance Criteria
- [ ] `make test-integration` → all pass
- [ ] `TestCreateUser`: POST /api/v1/users → verify row in DB
- [ ] `TestLogin_GetMe`: POST /auth/login → JWT → GET /auth/me → 200
- [ ] `TestListUsers_Pagination`: 3 users inserted → list limit=2 returns 2 + total=3
- [ ] `TestMigrations`: all files in db/migrations/ apply idempotently

---

## Story 4.2 — JWT Logout: JTI + Redis Blocklist (Sprint 10) — P0

### Context
Hiện tại `POST /auth/logout` là stub — client xóa token nhưng server vẫn accept token đó. Cần real revocation dùng JWT ID (JTI) + Redis.

### Technical Design

**jwtauth changes**:
```go
// Claims thêm JTI
RegisteredClaims: jwt.RegisteredClaims{
    ID: uuid.New().String(), // ← thêm JTI
}
```

**cache.BlockToken** đã có sẵn.

**Logout flow**:
```
POST /auth/logout
  ↓ extract Bearer token
  ↓ validate → get claims.ID (JTI)
  ↓ cache.BlockToken(ctx, jti, remainingTTL)
  ↓ 204 No Content

JWTAuth middleware:
  ↓ validate token → get claims
  ↓ cache.IsTokenBlocked(ctx, claims.ID)  ← NEW CHECK
  ↓ if blocked → 401 token_revoked
```

### Acceptance Criteria
- [ ] JTI auto-generated in every token
- [ ] POST /auth/logout → JTI stored in Redis with correct TTL
- [ ] Subsequent request with same token → 401 `{"code":"UNAUTHORIZED","message":"token revoked"}`
- [ ] Token expires naturally → key also expires (no Redis leak)
- [ ] Unit tests: `TestLogout_BlocksToken`, `TestJWTAuth_BlockedToken_401`

---

## Story 4.3 — Rate Limiting Middleware (Sprint 11) — P1

### Technical Design
```
pkg/ratelimit/
   ratelimit.go   ← Redis sliding window (go-redis/redis_rate)
```

**Rate limits**:
- Global API: 100 req/min per IP
- `/api/v1/auth/login` + `/refresh`: 10 req/min per IP (brute-force protection)
- Response headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- Exceeded: `429 Too Many Requests` + `Retry-After: <seconds>`

### Acceptance Criteria
- [ ] `pkg/ratelimit.Middleware(limit, window)` works as Chi middleware
- [ ] `ratelimit.StrictMiddleware(10, time.Minute)` applied to auth routes
- [ ] 429 response includes `Retry-After` header
- [ ] Unit tests with mock Redis

---

## Story 4.4 — OpenAPI 3.1 Spec + Swagger UI (Sprint 11) — P1

### Technical Design
**Approach**: hand-write `openapi.yaml` + serve via embedded `embed.FS`

```
docs/openapi.yaml          ← OpenAPI 3.1 spec
internal/handler/
   openapi_handler.go      ← serves /openapi.yaml + /docs (Swagger UI)
```

**Routes**:
```
GET /openapi.yaml  → serve embedded YAML
GET /docs          → serve Swagger UI (CDN or embedded html)
GET /docs/redoc    → serve Redoc (lightweight alternative)
```

### Acceptance Criteria
- [ ] All endpoints documented (request body, response schemas, 4xx errors)
- [ ] `securitySchemes.bearerAuth` defined
- [ ] `GET /docs` → browser shows interactive Swagger UI
- [ ] Spec validates with `swagger-cli validate`

---

## Story 4.5 — axe generate resource: Struct Tags + --with-auth (Sprint 12) — P2

### Changes needed
1. **Struct tags**: `json:"field_name"` được generated đúng (không cần TODO)
2. **`--with-auth` flag**: auto-add `r.Use(middleware.JWTAuth(jwtSvc))` to generated routes
3. **`--admin-only` flag**: auto-wrap with `RequireRole("admin")`
4. **`axe generate resource` generates chú thích** về routes cần register

---

## Sprint 10 — Execution Plan

```
Day 1: Story 4.2 (JWT Logout) — nhỏ gọn, high impact
   1. Thêm JTI vào Claims.RegisteredClaims.ID
   2. Update Logout handler → cache.BlockToken()
   3. Update JWTAuth middleware → cache.IsTokenBlocked()
   4. Unit tests

Day 1-2: Story 4.1 (Integration Tests)
   1. go get testcontainers-go/modules/postgres
   2. tests/integration/setup_test.go
   3. tests/integration/auth_test.go
   4. tests/integration/user_test.go
   5. Makefile: test-integration target
   6. CI: add integration-test job
```
