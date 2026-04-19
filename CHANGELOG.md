# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Pre-1.0 Notice**: axe is pre-1.0. Minor version bumps (0.x → 0.y) may contain
> breaking changes. Pin your version in `go.mod` and check this changelog before upgrading.

---

## [Unreleased]

### Added
- `examples/ecommerce/` — full e-commerce API with PlaceOrder flow, order status machine, product/review validation
- `examples/webtoon/` — webtoon platform API with genre whitelist, episode view tracking, bookmark toggle
- Example Projects section in main README

---

## [v1.0.0-rc.1] — 2026-04-19

**First release candidate.** All library packages ≥80% test coverage, zero TODO/FIXME,
full documentation sync, and incremental adoption path for existing Go projects.

### Added
- `CHANGELOG.md` — proper version history for upgrade decisions
- `docs/guides/incremental-adoption.md` — 4-stage guide for existing Go projects
- `examples/` — 3 runnable examples (apperror, txmanager, plugin system)
- `README.md` documentation section linking all guides
- gRPC non-goal position documented in PRD

### Changed
- **Coverage milestone**: 30/32 tested packages ≥80% (avg 83.9%)
  - `pkg/plugin/ratelimit`: 54.3% → 92.9%
  - `pkg/worker`: 54.5% → 84.8%
  - `pkg/ws`: 77.8% → 88.9%
  - `cmd/axe/generate`: 34.4% → 88.7%
- Epic 8: 6-Layer consistency table synced (all 6 layers ✅)
- Epic 9: Sentry + OpenAI marked Done (were stale "Planned")
- PRD success metrics updated to reflect actual achievements
- Sprint status updated through Sprint 31

---

## [v0.3.5] — 2026-04-19

### Changed
- **Coverage milestone**: 26/52 packages ≥ 80% (production-grade at 50%)

### Added
- `pkg/plugin/otel` — OpenTelemetry tracing plugin (89.8% coverage)
- `pkg/plugin/sentry` — Sentry error tracking plugin (94.1% coverage)
- `pkg/plugin/ai/openai` — OpenAI integration plugin (87.7% coverage)

---

## [v0.3.4] — 2026-04-18

### Changed
- Coverage scorecard: 22/52 packages ≥ 80%
- `internal/handler` coverage → 86.5%
- `internal/service` coverage → 96.5%

---

## [v0.3.3] — 2026-04-17

### Changed
- `pkg/plugin/obs` coverage → 100%
- `pkg/plugin/email` coverage → 95.1%
- `cmd/axe/generate` coverage → 34.4%
- `pkg/plugin/storage` coverage → 86.0%
- `pkg/ws` coverage → 77.8%

---

## [v0.3.2] — 2026-04-16

### Changed
- `config` test hardening: 57.1% → 85.7%
- `pkg/outbox` refactored: extracted `Enqueuer` interface, coverage 62.3% → 82.1%

---

## [v0.3.1] — 2026-04-15

### Changed
- `pkg/plugin/tenant` coverage → 91.8%
- `pkg/plugin/email` coverage → 67.9%
- Plugin discovery CLI improvements

---

## [v0.3.0] — 2026-04-14

### Added
- Plugin discovery CLI: `axe plugin list`, `axe plugin info`, `axe plugin validate`
- OAuth2 plugin test hardening

### Changed
- Auth handler test coverage improved
- Outbox, worker, jwtauth, txmanager test hardening

---

## [v0.2.2] — 2026-04-12

### Fixed
- Scaffold integration tests — catch compile errors in CI
- Build errors shown inline in `make run`
- Plugin `add storage` injection path fixed

---

## [v0.2.1] — 2026-04-11

### Fixed
- Scaffold issues found during end-to-end verification
- Ent decoupling from core migration files

---

## [v0.2.0] — 2026-04-10

### Added
- **Plugin ecosystem** (Epic 8): `app.Use(plugin)` lifecycle with:
  - `Dependent` interface + Kahn's DAG validation (cycle detection)
  - `Versioned` interface + semver compatibility check
  - `HealthChecker` interface + `/ready` aggregation
  - Wave-based parallel startup (`buildWaves`)
  - Plugin Event Bus (`pkg/plugin/events`) — sync, async, Redis delivery
  - Observability helpers (`pkg/plugin/obs`) — metrics naming convention
  - MockApp test harness (`pkg/plugin/testing`)
- **16 official plugins**: storage, email, ratelimit, oauth2, tenant, admin, stripe, typesense, s3, kafka, otel, sentry, openai (and interface packages)
- **Plugin CLI**: `axe plugin list|add|new|validate`
- Leader Pattern documentation
- JuiceFS storage integration guide

### Changed
- Storage plugin hardened: JWT auth on POST/DELETE, path traversal protection, CORS
- Storage fsync + FUSE health check + wrapped system errors
- Architecture contract updated with plugin conventions

### Breaking Changes
- `plugin.New()` functions now return `(*Plugin, error)` instead of `*Plugin`
- Plugin `Register()` requires `context.Context` as first arg

---

## [v0.1.13] — 2026-04-05

### Changed
- Multi-database adapter improvements

## [v0.1.12] — 2026-04-04

### Changed
- WebSocket hub improvements

---

## [v0.1.6] — 2026-03-28

### Added
- **WebSocket Hub** (Epic 7): Hub/Client/Room pattern with JWT auth
- Redis Pub/Sub adapter for multi-instance scaling
- `axe generate resource --with-ws` flag

---

## [v0.1.5] — 2026-03-25

### Added
- **`axe new` command** (Epic 6): full project scaffolding
  - `axe new blog-api` — PostgreSQL + worker + cache (default)
  - `axe new shop --db=mysql` — MySQL backend
  - `axe new lite --db=sqlite --no-worker --no-cache` — minimal setup
  - `axe new media --with-storage` — file upload endpoints
  - Interactive wizard mode

---

## [v0.1.4] — 2026-03-22

### Added
- **Multi-Database Support** (Epic 5): PostgreSQL, MySQL, SQLite
- `pkg/db/adapter.go` — pluggable DB adapter interface
- Integration tests for all 3 databases

---

## [v0.1.0] — 2026-03-10

### Added
- **Foundation** (Epics 1-4): Complete Go web framework
  - Clean Architecture: `domain/` → `handler/` → `service/` → `repository/`
  - Chi v5 HTTP router with structured middleware
  - Ent ORM (writes) + sqlc (reads) — shared `*sql.DB` pool
  - `pkg/apperror` — typed error taxonomy (NotFound, Unauthorized, etc.)
  - `pkg/txmanager` — Unit of Work pattern for transactions
  - `pkg/outbox` — Transactional Outbox pattern → Asynq background jobs
  - `pkg/jwtauth` — JWT access/refresh tokens + Redis blocklist
  - `pkg/cache` — Redis cache-aside layer
  - `pkg/ratelimit` — Redis sliding window rate limiter
  - `pkg/logger` — structured slog with request ID propagation
  - `pkg/metrics` — Prometheus `/metrics` endpoint
  - `pkg/worker` — Asynq background job processing
  - `axe generate resource` — CLI code generator (10 files per resource)
  - User domain as reference implementation
  - Health check endpoints (`/health`, `/ready`)
  - Docker Compose: PostgreSQL + Redis + Asynqmon
  - GitHub Actions CI pipeline
  - OpenAPI 3.1 spec + Swagger UI

---

[Unreleased]: https://github.com/axe-cute/axe/compare/v1.0.0-rc.1...HEAD
[v1.0.0-rc.1]: https://github.com/axe-cute/axe/compare/v0.3.5...v1.0.0-rc.1
[v0.3.5]: https://github.com/axe-cute/axe/compare/v0.3.4...v0.3.5
[v0.3.4]: https://github.com/axe-cute/axe/compare/v0.3.3...v0.3.4
[v0.3.3]: https://github.com/axe-cute/axe/compare/v0.3.2...v0.3.3
[v0.3.2]: https://github.com/axe-cute/axe/compare/v0.3.1...v0.3.2
[v0.3.1]: https://github.com/axe-cute/axe/compare/v0.3.0...v0.3.1
[v0.3.0]: https://github.com/axe-cute/axe/compare/v0.2.2...v0.3.0
[v0.2.2]: https://github.com/axe-cute/axe/compare/v0.2.1...v0.2.2
[v0.2.1]: https://github.com/axe-cute/axe/compare/v0.2.0...v0.2.1
[v0.2.0]: https://github.com/axe-cute/axe/compare/v0.1.13...v0.2.0
[v0.1.13]: https://github.com/axe-cute/axe/compare/v0.1.12...v0.1.13
[v0.1.12]: https://github.com/axe-cute/axe/compare/v0.1.6...v0.1.12
[v0.1.6]: https://github.com/axe-cute/axe/compare/v0.1.5...v0.1.6
[v0.1.5]: https://github.com/axe-cute/axe/compare/v0.1.4...v0.1.5
[v0.1.4]: https://github.com/axe-cute/axe/compare/v0.1.0...v0.1.4
[v0.1.0]: https://github.com/axe-cute/axe/releases/tag/v0.1.0
