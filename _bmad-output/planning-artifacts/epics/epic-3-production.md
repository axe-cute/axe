# Epic 3: Production Hardening

**Status**: ✅ Done (Sprint 7–9)  
**Goal**: Ready để deploy real workload  
**Depends on**: Epic 1 + Epic 2 complete  
**Completed**: 2026-04-15  

> ⚠️ Source of truth cho status: `sprint-status.yaml`

---

## Story 3.1: Observability — Structured Logging

**Status**: ✅ Done | **Sprint**: 7

**Acceptance Criteria**:
- [x] JSON log output với fields: timestamp, level, message, request_id, trace_id
- [x] Log sampling config cho production
- [x] Sensitive field redaction (password, token)
- [x] Log rotation config

---

## Story 3.2: Observability — Metrics + Tracing

**Status**: ✅ Done | **Sprint**: 7

**Acceptance Criteria**:
- [x] Prometheus `/metrics` endpoint
- [x] Custom metrics: request count, latency histogram, DB pool stats
- [x] OpenTelemetry tracer setup (OTLP exporter)
- [x] Trace ID propagation vào logger context
- [x] Docker Compose thêm Jaeger (local tracing UI)

---

## Story 3.3: Redis Cache Layer

**Status**: ✅ Done | **Sprint**: 8

**Acceptance Criteria**:
- [x] `pkg/cache/` package với Cache interface
- [x] Redis implementation với go-redis v9
- [x] Cache-aside pattern trong service layer
- [x] TTL configurable per resource type
- [x] Cache miss → DB fallback
- [x] Cache invalidation on write

---

## Story 3.4: JWT Authentication + RBAC

**Status**: ✅ Done | **Sprint**: 8

**Acceptance Criteria**:
- [x] Access token (15 phút), Refresh token (7 ngày, stored in DB)
- [x] POST `/api/v1/auth/login`, POST `/api/v1/auth/refresh`, POST `/api/v1/auth/logout`
- [x] JWT middleware: ExtractToken → ValidateToken → InjectUser
- [x] RBAC: roles table, permission table, user_roles join
- [x] `RequireRole("admin")` middleware factory
- [x] Permission check trong service layer (ownership)

---

## Story 3.5: Background Jobs — Asynq + Outbox Poller

**Status**: ✅ Done | **Sprint**: 9

**Acceptance Criteria**:
- [x] Asynq client + server setup
- [x] Outbox poller goroutine: poll 1s, batch 100, publish to Asynq, mark processed
- [x] Dead letter queue + Asynqmon UI (Docker Compose)
- [x] Retry policy: exponential backoff, max 3 retries
- [x] Example worker: SendWelcomeEmail (idempotent)
- [x] Graceful shutdown: drain in-flight jobs

---

## Story 3.6: Docker + CI/CD

**Status**: ✅ Done | **Sprint**: 9

**Acceptance Criteria**:
- [x] Multi-stage Dockerfile: builder (Go 1.22) → final (debian-slim), < 20MB
- [x] `.dockerignore` đúng chuẩn
- [x] GitHub Actions workflow:
  - `go vet ./...`
  - `staticcheck ./...`
  - `go test ./...`
  - Docker build + push to GHCR
  - Migration run on deploy
- [x] Kubernetes manifests (optional): Deployment, Service, ConfigMap, HPA
