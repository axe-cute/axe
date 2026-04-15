# Epic 5 — Multi-Database Support

**Goal**: Mở rộng axe khỏi PostgreSQL-only bằng cách thêm MySQL và SQLite adapters, cho phép developers chọn database phù hợp với use case.

**Business Value**: Tăng addressable market — nhiều teams dùng MySQL (legacy), SQLite (testing/embedded).

**Status**: `planned`

**Priority**: P0

---

## Stories

### Story 5.1 — Database Adapter Interface
**Sprint**: 13 | **Priority**: P0

**Goal**: Thiết kế `db.Adapter` interface tách biệt driver-specific code.

**Acceptance Criteria**:
- [ ] `pkg/db/adapter.go` — interface `Adapter` với `Open()`, `Ping()`, `Close()`, `DSN()`
- [ ] `pkg/db/postgres/adapter.go` — extract current pgx logic vào adapter
- [ ] `config.go` — thêm `DB_DRIVER` env var (`postgres` | `mysql` | `sqlite`)
- [ ] Existing PostgreSQL behavior **không thay đổi** (zero regression)
- [ ] Unit tests cho adapter interface

### Story 5.2 — MySQL Adapter
**Sprint**: 13 | **Priority**: P0

**Goal**: Hỗ trợ MySQL 8.x với `go-sql-driver/mysql`.

**Acceptance Criteria**:
- [ ] `pkg/db/mysql/adapter.go` với connection pool config
- [ ] Ent driver routing dựa trên `DB_DRIVER`
- [ ] `axe generate resource` sinh Ent schema hỗ trợ MySQL types
- [ ] Migration runner hoạt động với MySQL (thay `gen_random_uuid()` → `UUID()`)
- [ ] Integration test với `testcontainers-go/modules/mysql`
- [ ] README: MySQL quick start section

### Story 5.3 — SQLite Adapter (Test/Dev)
**Sprint**: 14 | **Priority**: P1

**Goal**: SQLite cho testing và embedded use cases (không cần Docker).

**Acceptance Criteria**:
- [ ] `pkg/db/sqlite/adapter.go` với `mattn/go-sqlite3`
- [ ] `make test` có thể chạy với `DB_DRIVER=sqlite` (không cần Postgres)
- [ ] Testcontainers fallback: nếu Docker không available → SQLite
- [ ] `axe generate resource` tạo SQLite-compatible migrations
- [ ] **Không** hỗ trợ SQLite trong production (warn log nếu detect production env)

### Story 5.4 — Multi-DB Integration Tests
**Sprint**: 14 | **Priority**: P1

**Goal**: CI matrix chạy integration tests trên cả 3 databases.

**Acceptance Criteria**:
- [ ] GitHub Actions matrix: `{postgres, mysql}` × integration tests
- [ ] SQLite unit tests (không cần containers)
- [ ] `make test-integration-mysql` target
- [ ] Test isolation: mỗi test case có prefix schema riêng

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
    adapter.go        ← mattn/go-sqlite3 (dev/test only)

config/
  config.go           ← + DBDriver string
```

**Ent multi-driver**: Ent hỗ trợ `dialect.Postgres`, `dialect.MySQL`, `dialect.SQLite` — chỉ cần route đúng driver trong `cmd/api/main.go`.

---

## Risks
- MySQL không có `TIMESTAMPTZ` → dùng `DATETIME` + UTC convention
- SQLite không hỗ trợ `ALTER TABLE ADD COLUMN` nhiều trường hợp → document limitations
