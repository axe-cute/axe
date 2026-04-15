# Epic 6 — `axe new` Project Scaffolding CLI

**Goal**: `axe new <project-name>` tạo một project hoàn chỉnh production-ready từ zero — tương đương `rails new` hoặc `django-admin startproject`.

**Business Value**: Giảm time-to-first-commit từ 2-3 ngày xuống < 5 phút. Đây là **killer feature** phân biệt axe với Gin/Fiber.

**Status**: `planned`

**Priority**: P0

---

## Stories

### Story 6.1 — `axe new` Core Command
**Sprint**: 15 | **Priority**: P0

**Goal**: Tạo command `axe new <name>` sinh project structure đầy đủ.

**Acceptance Criteria**:
- [ ] `axe new blog-api` tạo folder `blog-api/` với full axe structure
- [ ] `--db` flag: `postgres` (default) | `mysql` | `sqlite`
- [ ] `--no-worker` flag: bỏ Asynq nếu không cần background jobs
- [ ] `--no-cache` flag: bỏ Redis cache
- [ ] Generated `go.mod` với module name đúng
- [ ] Generated `.env.example`, `.gitignore`, `Makefile`, `Dockerfile`
- [ ] Generated `README.md` với project-specific quick start
- [ ] `cd blog-api && make setup && make run` phải hoạt động

**Generated structure**:
```
<name>/
├── cmd/
│   ├── api/main.go
│   └── axe/main.go          ← CLI binary
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
│   └── txmanager/
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
**Sprint**: 15 | **Priority**: P1

**Goal**: `axe new` có wizard mode nếu chạy không có flags.

**Acceptance Criteria**:
- [ ] Nếu không có args → interactive prompts (dùng `AlecAivazis/survey`)
- [ ] Prompts: project name, module path, database, features (cache/worker/metrics)
- [ ] Preview cấu trúc trước khi generate
- [ ] `--yes` flag để skip interactive mode (CI-friendly)

### Story 6.3 — Template Versioning
**Sprint**: 16 | **Priority**: P2

**Goal**: Templates được versioned và có thể update.

**Acceptance Criteria**:
- [ ] Templates nhúng vào binary bằng `go:embed`
- [ ] `axe new --template=v1.0` để pin template version
- [ ] `axe upgrade` để update templates của existing project (dry-run mode)

### Story 6.4 — `axe new` Integration Test
**Sprint**: 16 | **Priority**: P1

**Goal**: CI tự động generate project mới và verify nó build được.

**Acceptance Criteria**:
- [ ] CI job: `axe new test-project-ci && cd test-project-ci && go build ./...`
- [ ] CI job: `go test ./...` passes trên generated project
- [ ] Nếu test fail → CI blocks merge

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

Templates embed từ `cmd/axe/new/templates/` directory using `//go:embed templates/**`.

---

## Risks
- Template drift: templates trong binary có thể outdated → cần versioning (Story 6.3)
- Module path: user phải nhập đúng Go module path → validate format
