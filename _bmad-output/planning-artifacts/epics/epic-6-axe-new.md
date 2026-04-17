# Epic 6 — `axe new` Project Scaffolding CLI

**Goal**: `axe new <project-name>` tạo một project hoàn chỉnh production-ready từ zero — tương đương `rails new` hoặc `django-admin startproject`.

**Business Value**: Giảm time-to-first-commit từ 2-3 ngày xuống < 5 phút. Đây là **killer feature** phân biệt axe với Gin/Fiber.

**Status**: ✅ Done (Sprint 15–16, trừ Story 6.4)

**Priority**: P0

> ⚠️ Source of truth cho status: `sprint-status.yaml`

---

## Stories

### Story 6.1 — `axe new` Core Command
**Sprint**: 15 | **Priority**: P0 | **Status**: ✅ Done

**Goal**: Tạo command `axe new <name>` sinh project structure đầy đủ.

**Acceptance Criteria**:
- [x] `axe new blog-api` tạo folder `blog-api/` với full axe structure
- [x] `--db` flag: `postgres` (default) | `mysql` | `sqlite`
- [x] `--no-worker` flag: bỏ Asynq nếu không cần background jobs
- [x] `--no-cache` flag: bỏ Redis cache
- [x] Generated `go.mod` với module name đúng
- [x] Generated `.env.example`, `.gitignore`, `Makefile`, `Dockerfile`
- [x] Generated `README.md` với project-specific quick start
- [x] `cd blog-api && make setup && make run` phải hoạt động

**Implementation**:
- `cmd/axe/new/new.go`: cobra command, flag validation
- `cmd/axe/new/scaffold.go`: creates full directory tree, renders all templates, writes 27+ files
- `cmd/axe/new/templates.go`: 1600+ lines of Go text/templates for all generated files

**Generated structure**:
```
<name>/
├── cmd/
│   ├── api/main.go
│   └── axe/main.go          ← CLI binary (migrate up/down/status/create)
├── config/config.go
├── db/migrations/
│   └── 001_init.sql
├── internal/
│   ├── domain/
│   ├── handler/
│   │   └── middleware/
│   ├── repository/
│   └── service/
├── pkg/
│   ├── apperror/
│   ├── cache/
│   ├── jwtauth/
│   ├── logger/
│   ├── metrics/
│   ├── ratelimit/
│   ├── txmanager/
│   └── ws/
├── docs/openapi.yaml
├── ent/schema/
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── go.mod
```

### Story 6.2 — Interactive Mode
**Sprint**: 15 | **Priority**: P1 | **Status**: ✅ Done

**Goal**: `axe new` có wizard mode nếu chạy không có flags.

**Acceptance Criteria**:
- [x] Nếu không có args → interactive prompts
- [x] Prompts: project name, module path, database, features (cache/worker/metrics)
- [x] Preview cấu trúc trước khi generate
- [x] `--yes` flag để skip interactive mode (CI-friendly)

**Implementation**: `cmd/axe/new/interactive.go` — wizard mode; `axe new` (no args) triggers wizard, `--yes` for CI mode

### Story 6.3 — Bootable Scaffold
**Sprint**: 16 | **Priority**: P0 | **Status**: ✅ Done

**Goal**: Generated project immediately buildable and runnable.

**Acceptance Criteria**:
- [x] `scaffold()` runs `go mod tidy` in project dir → go.sum populated
- [x] `tmplMainAxeGo`: full self-contained migrate CLI (up/down/status/create)
- [x] `make setup && make run` verified end-to-end

**Results**:
- scaffold() step 3: exec `go mod tidy` → project immediately buildable
- Migrate CLI: pgx/v5 inline, no external deps
- API :8080 + Redis + Asynq worker all green ✅

### Story 6.4 — `axe new` Integration Test
**Sprint**: 20 (carried from 16) | **Priority**: P1 | **Status**: ✅ Done

**Goal**: CI tự động generate project mới và verify nó build được.

**Acceptance Criteria**:
- [x] CI job: `axe new test-project-ci && cd test-project-ci && go build ./...`
- [x] CI job: `go test ./...` passes trên generated project
- [x] Nếu test fail → CI blocks merge

**Results**:
- `.github/workflows/ci.yml`: `scaffold-test` job added
- Matrix: postgres, mysql, sqlite (3 variants, fail-fast: false)
- Steps: build CLI → `axe new --yes` → `go vet` → `go build` → `go test`
- Docker job now depends on scaffold-test passing

---

## Technical Design

```go
// cmd/axe/new/new.go
func Command() *cobra.Command {
    var opts NewOptions
    cmd := &cobra.Command{
        Use:   "new <name>",
        Short: "Scaffold a new axe project",
        RunE: func(cmd *cobra.Command, args []string) error {
            return scaffold(args[0], opts)
        },
    }
    cmd.Flags().StringVar(&opts.DB, "db", "postgres", "Database driver")
    cmd.Flags().BoolVar(&opts.NoWorker, "no-worker", false, "Skip Asynq worker")
    cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "Skip Redis cache")
    return cmd
}
```

Templates: `cmd/axe/new/templates.go` — 1600+ lines, embedded via Go string literals.

---

## Risks
- ~~Template drift: templates trong binary có thể outdated~~ → Mitigated: templates.go + `go mod tidy` at scaffold time keeps deps in sync
- Module path: user phải nhập đúng Go module path → validate format ✅
