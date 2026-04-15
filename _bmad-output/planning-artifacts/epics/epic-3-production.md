# Epic 3: Production Hardening

**Status**: `todo`  
**Goal**: Ready để deploy real workload  
**Depends on**: Epic 1 + Epic 2 complete  
**Estimated**: 3-4 tuần

---

## Story 3.1: Observability — Structured Logging

**Status**: `todo`

**Acceptance Criteria**:
- [ ] JSON log output với fields: timestamp, level, message, request_id, trace_id
- [ ] Log sampling config cho production
- [ ] Sensitive field redaction (password, token)
- [ ] Log rotation config

---

## Story 3.2: Observability — Metrics + Tracing

**Status**: `todo`

**Acceptance Criteria**:
- [ ] Prometheus `/metrics` endpoint
- [ ] Custom metrics: request count, latency histogram, DB pool stats
- [ ] OpenTelemetry tracer setup (OTLP exporter)
- [ ] Trace ID propagation vào logger context
- [ ] Docker Compose thêm Jaeger (local tracing UI)

---

## Story 3.3: Redis Cache Layer

**Status**: `todo`

**Acceptance Criteria**:
- [ ] `pkg/cache/` package với Cache interface
- [ ] Redis implementation với go-redis v9
- [ ] Cache-aside pattern trong service layer
- [ ] TTL configurable per resource type
- [ ] Cache miss → DB fallback
- [ ] Cache invalidation on write

---

## Story 3.4: JWT Authentication + RBAC

**Status**: `todo`

**Acceptance Criteria**:
- [ ] Access token (15 phút), Refresh token (7 ngày, stored in DB)
- [ ] POST `/api/v1/auth/login`, POST `/api/v1/auth/refresh`, POST `/api/v1/auth/logout`
- [ ] JWT middleware: ExtractToken → ValidateToken → InjectUser
- [ ] RBAC: roles table, permission table, user_roles join
- [ ] `RequireRole("admin")` middleware factory
- [ ] Permission check trong service layer (ownership)

---

## Story 3.5: Background Jobs — Asynq + Outbox Poller

**Status**: `todo`

**Acceptance Criteria**:
- [ ] Asynq client + server setup
- [ ] Outbox poller goroutine: poll 1s, batch 100, publish to Asynq, mark processed
- [ ] Dead letter queue + Asynqmon UI (Docker Compose)
- [ ] Retry policy: exponential backoff, max 3 retries
- [ ] Example worker: SendWelcomeEmail (idempotent)
- [ ] Graceful shutdown: drain in-flight jobs

---

## Story 3.6: Docker + CI/CD

**Status**: `todo`

**Acceptance Criteria**:
- [ ] Multi-stage Dockerfile: builder (Go 1.22) → final (debian-slim), < 20MB
- [ ] `.dockerignore` đúng chuẩn
- [ ] GitHub Actions workflow:
  - `go vet ./...`
  - `staticcheck ./...`
  - `go test ./...`
  - Docker build + push to GHCR
  - Migration run on deploy
- [ ] Kubernetes manifests (optional): Deployment, Service, ConfigMap, HPA
