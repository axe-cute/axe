# Epic 2: Developer Experience

**Status**: `todo`  
**Goal**: Dev mới có thể tạo feature trong 1 ngày  
**Depends on**: Epic 1 complete  
**Estimated**: 3-4 tuần

---

## Story 2.1: axe CLI — Project setup

**Status**: `todo`

**Goal**: CLI binary `axe` installable via `go install`.

**Acceptance Criteria**:
- [ ] `cmd/axe/main.go`: CLI entry point dùng cobra
- [ ] `axe --version` in version
- [ ] `axe help` liệt kê commands
- [ ] `go install github.com/axe-cute/axe/cmd/axe@latest` hoạt động

---

## Story 2.2: axe generate resource

**Status**: `todo`

**Goal**: Generate full CRUD resource trong < 10 phút.

**Acceptance Criteria**:
- [ ] `axe generate resource <Name> --fields="field:type,..." [--belongs-to=Entity]`
- [ ] Generate: domain.go, handler.go, handler_test.go, service.go, service_test.go, repo.go, query.go, ent/schema.go, migration.sql, queries.sql
- [ ] Templates dùng Go text/template
- [ ] Field types: string, text, int, float, bool, uuid, time
- [ ] Correct layer rules trong generated code
- [ ] `axe generate resource Post --fields="title:string,body:text"` test thủ công

---

## Story 2.3: axe migrate commands

**Status**: `todo`

**Acceptance Criteria**:
- [ ] `axe migrate create <name>` tạo timestamped migration file
- [ ] `axe migrate up` apply pending migrations
- [ ] `axe migrate down` rollback last migration
- [ ] `axe migrate status` list applied/pending

---

## Story 2.4: ADR structure + docs

**Status**: `todo`

**Acceptance Criteria**:
- [ ] `docs/adr/` folder với ADR template
- [ ] ADR-001 đến ADR-005 từ architecture.md viết ra file
- [ ] `docs/adr/README.md` index file
- [ ] Postman/Bruno collection cho User domain CRUD
