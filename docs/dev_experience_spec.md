# 🧑‍💻 Developer Experience Spec

> The DX contract for the axe framework.
> Goal: a new developer ships their first feature on day one.

---

## DX Service Level Agreements

| Metric | Target | How to measure |
|---|---|---|
| Create a full CRUD endpoint | ≤ 10 minutes | `axe generate resource` + stopwatch |
| Run full test suite | ≤ 30 seconds | `make test` wall time |
| Understand one handler | ≤ 5 minutes | Linear, readable code — no hidden magic |
| New developer onboarding | ≤ 1 working day | README → pair session → first PR |
| `make run` from fresh clone | ≤ 2 minutes | Docker Compose + `make setup` |

---

## axe CLI

### Installation

```bash
# From source (inside the axe repo)
go build -o bin/axe ./cmd/axe

# Or install globally
go install github.com/axe-cute/axe/cmd/axe@latest
```

### Project Scaffolding

```bash
# Create a new project with all defaults (Postgres, worker, cache)
axe new blog-api --module=github.com/acme/blog-api

# MySQL project
axe new shop --db=mysql --module=github.com/acme/shop

# Lightweight project (SQLite, no worker, no cache — no Docker needed)
axe new lite --db=sqlite --no-worker --no-cache --yes

# Interactive wizard (prompts for all options)
axe new
```

### Resource Generator

```bash
# Generate a full CRUD resource (10 files across all layers)
axe generate resource Post \
  --fields="title:string,body:text,published:bool,author_id:uuid" \
  --belongs-to="User"

# With JWT authentication middleware
axe generate resource Order \
  --fields="amount:float,status:string" \
  --with-auth

# Admin-only access (implies --with-auth)
axe generate resource Config \
  --fields="key:string,value:text" \
  --admin-only

# With WebSocket room handler
axe generate resource Chat \
  --fields="message:text" \
  --with-ws
```

**Supported field types:** `string`, `text`, `int`, `float`, `bool`, `uuid`, `time`

### Output of `axe generate resource Post`

```
✅ internal/domain/post.go                          — Entity + repository interface
✅ internal/handler/post_handler.go                  — HTTP handler (CRUD routes)
✅ internal/handler/post_handler_test.go             — Handler unit tests
✅ internal/service/post_service.go                  — Business logic
✅ internal/service/post_service_test.go             — Service unit tests
✅ internal/repository/post_repo.go                  — Ent-based data access
✅ ent/schema/post.go                                — Ent ORM schema
✅ db/migrations/YYYYMMDDHHMMSS_create_posts.sql     — SQL migration
✅ db/queries/post.sql                               — sqlc queries

Next steps:
  1. Register handler in cmd/api/main.go:
     postHandler := handler.NewPostHandler(postSvc)
     r.Mount("/api/v1/posts", postHandler.Routes())
  2. Run: go generate ./ent/...
  3. Run: make migrate-up
  4. Run: make test
```

### Migration Runner

```bash
axe migrate up         # Apply all pending migrations
axe migrate down       # Rollback the last migration
axe migrate status     # Show current migration state
axe migrate create add_index_to_orders    # Create a new migration file
```

---

## Makefile Commands

```bash
# Development
make run                     # Start server (hot-reload with air if available)
make build                   # Build binary to ./bin/axe

# Testing
make test                    # All unit tests (< 30s)
make test-race               # With race detector
make test-coverage           # HTML coverage report
make test-integration        # PostgreSQL integration (Docker required)
make test-integration-mysql  # MySQL integration (Docker required)
make test-integration-sqlite # SQLite integration (no Docker needed)
make test-ws                 # WebSocket hub unit tests
make test-ws-integration     # WebSocket Redis integration (Docker required)
make test-plugin             # Plugin + storage unit tests

# Quality
make lint                    # golangci-lint
make vet                     # go vet
make fmt                     # gofmt + goimports

# Code generation
make generate                # All generators (Ent + sqlc + Wire)
make generate-ent            # Ent ORM only
make generate-sqlc           # sqlc only

# Database
make migrate-up              # Apply pending migrations
make migrate-down            # Rollback last migration
make migrate-status          # Show migration state
make seed                    # Load test/seed data

# Docker
make docker-up               # Start PostgreSQL + Redis + Asynqmon
make docker-down             # Stop services
make docker-logs             # Follow compose logs

# Setup & cleanup
make setup                   # Full local setup from zero
make tidy                    # go mod tidy
make clean                   # Remove build artifacts
```

---

## Local Setup (from zero to running)

```bash
# Clone
git clone https://github.com/axe-cute/axe && cd axe

# One-command setup (copies .env, starts Docker, migrates, seeds)
make setup

# Run
make run
# → http://localhost:8080
```

**Total time: < 2 minutes** (excluding initial Docker image pull)

### Or with a fresh scaffolded project:

```bash
axe new my-api --db=sqlite --no-worker --no-cache --yes
cd my-api
make run
# → http://localhost:8080
```

**Total time: < 1 minute** (SQLite, no Docker needed)

---

## Reference Implementation

The **User domain** is the canonical reference for every pattern in axe:

| Layer | File | What to learn |
|---|---|---|
| Domain | `internal/domain/user.go` | Entity struct, repository interface, validation |
| Handler | `internal/handler/user_handler.go` | Route setup, JSON parsing, service call |
| Service | `internal/service/user_service.go` | Business rules, error mapping, tx usage |
| Repository | `internal/repository/user_repo.go` | Ent queries, interface implementation |

**When in doubt about how to implement something → read the User domain.**

---

## Onboarding Path (1 day)

### Morning (4 hours)

| Time | Activity |
|---|---|
| 30 min | Read [`docs/architecture_contract.md`](architecture_contract.md) |
| 30 min | Read [`docs/data_consistency.md`](data_consistency.md) |
| 30 min | Clone, `make setup`, `make run` — verify everything works |
| 90 min | Read the entire User domain code end-to-end |
| 30 min | Run and read User domain tests |

### Afternoon (4 hours)

| Time | Activity |
|---|---|
| 15 min | `axe generate resource YourDomain --fields="..."` |
| 90 min | Customize generated code, add your business rules |
| 60 min | Write unit tests for your service layer |
| 45 min | Submit PR for review |
| 30 min | Address review feedback |

---

*Last updated: 2026-04-16*
