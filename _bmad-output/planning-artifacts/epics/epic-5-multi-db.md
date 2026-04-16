# Epic 5 — Multi-Database Support

**Goal**: Mở rộng axe khỏi PostgreSQL-only bằng cách thêm MySQL và SQLite adapters, cho phép developers chọn database phù hợp với use case.

**Business Value**: Tăng addressable market — nhiều teams dùng MySQL (legacy), SQLite (testing/embedded).

**Status**: ✅ Done (Sprint 13–14)  
**Completed**: 2026-04-15  
**Priority**: P0

> ⚠️ Source of truth cho status: `sprint-status.yaml`

---

## Stories

### Story 5.1 — Database Adapter Interface
**Sprint**: 13 | **Priority**: P0

**Goal**: Thiết kế `db.Adapter` interface tách biệt driver-specific code.

**Acceptance Criteria**:
- [x] `pkg/db/adapter.go` — interface `Adapter` với `Open()`, `Ping()`, `Close()`, `DSN()`
- [x] `pkg/db/postgres/adapter.go` — extract current pgx logic vào adapter
- [x] `config.go` — thêm `DB_DRIVER` env var (`postgres` | `mysql` | `sqlite`)
- [x] Existing PostgreSQL behavior **không thay đổi** (zero regression)
- [x] Unit tests cho adapter interface

### Story 5.2 — MySQL Adapter
**Sprint**: 13 | **Priority**: P0

**Goal**: Hỗ trợ MySQL 8.x với `go-sql-driver/mysql`.

**Acceptance Criteria**:
- [x] `pkg/db/mysql/adapter.go` với connection pool config
- [x] Ent driver routing dựa trên `DB_DRIVER`
- [x] `axe generate resource` sinh Ent schema hỗ trợ MySQL types
- [x] Migration runner hoạt động với MySQL (thay `gen_random_uuid()` → `UUID()`)
- [x] Integration test với `testcontainers-go/modules/mysql`
- [x] README: MySQL quick start section

### Story 5.3 — SQLite Adapter (Test/Dev)
**Sprint**: 14 | **Priority**: P1

**Goal**: SQLite cho testing và embedded use cases (không cần Docker).

**Acceptance Criteria**:
- [x] `pkg/db/sqlite/adapter.go` với `modernc.org/sqlite` (pure Go, CGO-free)
- [x] `make test` có thể chạy với `DB_DRIVER=sqlite` (không cần Postgres)
- [x] Testcontainers fallback: nếu Docker không available → SQLite
- [x] `axe generate resource` tạo SQLite-compatible migrations
- [x] **Không** hỗ trợ SQLite trong production (warn log nếu detect production env)

### Story 5.4 — Multi-DB Integration Tests
**Sprint**: 14 | **Priority**: P1

**Goal**: CI matrix chạy integration tests trên cả 3 databases.

**Acceptance Criteria**:
- [x] GitHub Actions matrix: `{postgres, mysql, sqlite}` × integration tests
- [x] SQLite unit tests (không cần containers)
- [x] `make test-integration-mysql` target
- [x] Test isolation: mỗi test case có prefix schema riêng

---

## Technical Design

```
pkg/db/
  adapter.go          ← interface Adapter
  postgres/
    adapter.go        ← pgx implementation (hiện tại)
  mysql/
    adapter.go        ← go-sql-driver/mysql
  sqlite/
    adapter.go        ← modernc.org/sqlite (pure Go, dev/test only)

config/
  config.go           ← + DBDriver string
```

**Ent multi-driver**: Ent hỗ trợ `dialect.Postgres`, `dialect.MySQL`, `dialect.SQLite` — chỉ cần route đúng driver trong `cmd/api/main.go`.

---

## Risks
- MySQL không có `TIMESTAMPTZ` → dùng `DATETIME` + UTC convention
- SQLite không hỗ trợ `ALTER TABLE ADD COLUMN` nhiều trường hợp → document limitations
