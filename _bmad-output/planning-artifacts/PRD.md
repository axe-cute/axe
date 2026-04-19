# PRD — axe Go Web Framework

**Status**: Approved  
**Version**: 3.0  
**Date**: 2026-04-19  
**Changelog**: v3.0 — Admin UI promoted to optional plugin; added Epic 9 (Long-term Plugin Ecosystem); full plugin taxonomy (short-term vs long-term)

---

## Problem Statement

Các team Go backend hiện tại mất 2-3 ngày chỉ để scaffold một project mới với Clean Architecture đúng chuẩn. Không có framework Go nào enforce kiến trúc ở compile-time mà vẫn giữ tính transparent.

---

## Vision

**axe** cung cấp một nền tảng Go backend "không ma thuật" (no runtime magic) với:
- Kiến trúc Clean Architecture baked-in, compiler-enforced
- CLI generator tạo CRUD endpoint production-grade trong < 10 phút
- `axe new` scaffold project hoàn chỉnh trong < 5 phút
- Production-grade từ ngày đầu: transaction safety, observability, error handling

---

## Goals

1. **Dev mới ship feature trong 1 ngày đầu** (`axe new` → `make run` → PR)
2. **CRUD endpoint đầy đủ trong < 10 phút** với `axe generate resource`
3. **Zero runtime magic** — mọi behavior đều traceable tại compile-time
4. **Full test suite chạy < 30 giây**
5. **Production-ready từ Phase 1**: transaction, structured logging, error taxonomy
6. **Multi-database support**: PostgreSQL, MySQL, SQLite (pluggable adapter interface)
7. **Real-time ready**: WebSocket hub với room management, Redis pub/sub scale-out
8. **Plugin ecosystem**: extend framework không cần fork core

---

## Non-Goals

- Không build full-stack (chỉ backend API)
- Không có **built-in** admin UI — nhưng `axe-plugin-admin` là optional plugin (Epic 8.7)
- Không distributed tracing phức tạp ở Phase 1 (OpenTelemetry defer sang Epic 9)
- Không schema-per-tenant multi-tenancy ở v1.0 (chỉ tenant middleware)
- Không lock-in cloud provider — storage/messaging/AI đều pluggable
- **Không gRPC ở v1.0** — REST-first. gRPC support sẽ được đánh giá cho v2.0 dựa trên community demand. `pkg/ratelimit` và `pkg/ws` đã expose programmatic API (non-HTTP) có thể dùng từ gRPC handlers.

---

## Target Users

| User | Need |
|---|---|
| Go backend developer | Scaffold production-grade project nhanh |
| Tech lead | Enforce architecture rules uniformly |
| New team member | Onboard và ship feature trong 1 ngày |

---

## DX SLAs (Developer Experience Service Level Agreements)

| Metric | Target |
|---|---|
| Tạo project mới (`axe new`) | ≤ 5 phút |
| Tạo CRUD endpoint đầy đủ | ≤ 10 phút |
| Run full test suite | ≤ 30 giây |
| Hiểu 1 handler (linear read) | ≤ 5 phút |
| Onboarding (new dev) | ≤ 1 ngày |

---

## Epics

- **Epic 1**: Foundation — Hello World production-grade (Sprint 1–4) ✅
- **Epic 2**: Developer Experience — axe CLI generator (Sprint 5–6) ✅
- **Epic 3**: Production Hardening — observability, auth, cache, jobs (Sprint 7–9) ✅
- **Epic 4**: Quality & API Polish — integration tests, rate limit, OpenAPI (Sprint 10–12) ✅
- **Epic 5**: Multi-Database Support — PostgreSQL + MySQL + SQLite (Sprint 13–14) ✅
- **Epic 6**: `axe new` — Project Scaffolding CLI (Sprint 15–16) ✅
- **Epic 7**: WebSocket Hub — real-time support (Sprint 17–18) ✅
- **Epic 8**: Plugin System & Ecosystem — short-term plugins (Sprint 19–24) ✅ Done
- **Epic 9**: Long-term Plugin Ecosystem — AI, Cloud, Observability (Sprint 25+) 🔄 In Progress (7/13 stories done)

---

## Success Metrics

- [x] `axe new blog && cd blog && make setup && make run` hoạt động (< 5 phút)
- [x] User domain CRUD đầy đủ làm reference implementation
- [x] `axe generate resource Post` tạo đủ 10 files
- [x] Test coverage ≥ 80% cho handler (86.5%) + service (96.5%) layers
- [x] Zero forbidden imports trong internal/domain/
- [x] Multi-DB: PostgreSQL + MySQL + SQLite integration tests pass
- [x] WebSocket hub hoạt động với Redis pub/sub
- [x] Plugin system: `app.Use(plugin)` lifecycle hoạt động
- [x] **Short-term**: ≥ 6 official plugins shipped (storage, email, ratelimit, oauth2, tenant, admin)
- [x] **Short-term**: `axe plugin add email` tự động inject code
- [x] **Short-term**: Plugin infrastructure — correctness gates (8.10) + event bus (8.12) + observability contract (8.13) + versioning (8.14) done
- [x] **Long-term**: OpenAI plugin shipped (87.7% coverage) + Sentry (94.1%) + OTel (89.8%)
- [ ] **Long-term**: ≥ 3 AI plugins (openai ✅, gemini, ollama)
- [ ] **Long-term**: Web project configurator (start.axe.io) — deferred, CLI sufficient for early adopters
- [x] **Adoption**: `CHANGELOG.md` available for upgrade decisions
- [x] **Adoption**: Incremental adoption guide published (`docs/guides/incremental-adoption.md`)
- [x] **Adoption**: ≥ 3 real-world example projects (ecommerce, webtoon, 3 standalone examples)
