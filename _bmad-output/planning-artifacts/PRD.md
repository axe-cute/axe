# PRD — axe Go Web Framework

**Status**: Approved  
**Version**: 1.0  
**Date**: 2026-04-15

---

## Problem Statement

Các team Go backend hiện tại mất 2-3 ngày chỉ để scaffold một project mới với Clean Architecture đúng chuẩn. Không có framework Go nào enforce kiến trúc ở compile-time mà vẫn giữ tính transparent.

---

## Vision

**axe** cung cấp một nền tảng Go backend "không ma thuật" (no runtime magic) với:
- Kiến trúc Clean Architecture baked-in, compiler-enforced
- CLI generator tạo CRUD endpoint production-grade trong < 10 phút
- Production-grade từ ngày đầu: transaction safety, observability, error handling

---

## Goals

1. **Dev mới ship feature trong 1 ngày đầu** (clone → run → PR)
2. **CRUD endpoint đầy đủ trong < 10 phút** với `axe generate resource`
3. **Zero runtime magic** — mọi behavior đều traceable tại compile-time
4. **Full test suite chạy < 30 giây**
5. **Production-ready từ Phase 1**: transaction, structured logging, error taxonomy

---

## Non-Goals

- Không build full-stack (chỉ backend API)
- Không support database khác ngoài PostgreSQL (v1)
- Không có admin UI riêng (dùng Asynqmon cho jobs)
- Không distributed tracing phức tạp ở Phase 1

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
| Tạo CRUD endpoint đầy đủ | ≤ 10 phút |
| Run full test suite | ≤ 30 giây |
| Hiểu 1 handler (linear read) | ≤ 5 phút |
| Onboarding (new dev) | ≤ 1 ngày |
| `make run` từ clone | ≤ 2 phút |

---

## Epics

- **Epic 1**: Foundation — Hello World production-grade (4-6 tuần)
- **Epic 2**: Developer Experience — axe CLI generator (3-4 tuần)
- **Epic 3**: Production Hardening — observability, auth, cache, jobs (3-4 tuần)

---

## Success Metrics

- [ ] `make run` hoạt động sau `git clone` (< 2 phút)
- [ ] User domain CRUD đầy đủ làm reference implementation
- [ ] `axe generate resource Post` tạo đủ 10 files
- [ ] Test coverage ≥ 80% cho handler + service layers
- [ ] Zero forbidden imports trong internal/domain/
