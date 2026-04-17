# Epic 2: Developer Experience

**Status**: ✅ Done (Sprint 5–6)  
**Goal**: Dev mới có thể tạo feature trong 1 ngày  
**Depends on**: Epic 1 complete  
**Completed**: 2026-04-15  

> ⚠️ Source of truth cho status: `sprint-status.yaml`

---

## Story 2.1: axe CLI — Project setup

**Status**: ✅ Done | **Sprint**: 5

**Goal**: CLI binary `axe` installable via `go install`.

**Acceptance Criteria**:
- [x] `cmd/axe/main.go`: CLI entry point dùng cobra
- [x] `axe --version` in version
- [x] `axe help` liệt kê commands
- [x] `go install github.com/axe-cute/axe/cmd/axe@latest` hoạt động

---

## Story 2.2: axe generate resource

**Status**: ✅ Done | **Sprint**: 5

**Goal**: Generate full CRUD resource trong < 10 phút.

**Acceptance Criteria**:
- [x] `axe generate resource <Name> --fields="field:type,..." [--belongs-to=Entity]`
- [x] Generate: domain.go, handler.go, handler_test.go, service.go, service_test.go, repo.go, query.go, ent/schema.go, migration.sql, queries.sql
- [x] Templates dùng Go text/template
- [x] Field types: string, text, int, float, bool, uuid, time
- [x] Correct layer rules trong generated code
- [x] `axe generate resource Post --fields="title:string,body:text"` test thủ công

---

## Story 2.3: axe migrate commands

**Status**: ✅ Done | **Sprint**: 6

**Acceptance Criteria**:
- [x] `axe migrate create <name>` tạo timestamped migration file
- [x] `axe migrate up` apply pending migrations
- [x] `axe migrate down` rollback last migration
- [x] `axe migrate status` list applied/pending

---

## Story 2.4: ADR structure + docs

**Status**: ✅ Done | **Sprint**: 6

**Acceptance Criteria**:
- [x] `docs/adr/` folder với ADR template
- [x] ADR-001 đến ADR-005 từ architecture.md viết ra file
- [x] `docs/adr/README.md` index file
- [x] Postman/Bruno collection cho User domain CRUD
