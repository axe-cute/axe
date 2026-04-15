# axe

> Go web framework — Clean Architecture, no runtime magic, production-grade from day one.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## Philosophy

- **No runtime magic** — every behavior is traceable at compile-time
- **Clean Architecture baked-in** — layer violations caught by compiler
- **Production-grade from day one** — transactions, structured logging, error taxonomy
- **DX-first** — new dev ships feature on day one

---

## Quick Start (< 2 minutes)

**Prerequisites**: Go 1.22+, Docker

```bash
# Clone
git clone https://github.com/axe-go/axe && cd axe

# One-command setup (copies .env, starts Docker, migrates, seeds)
make setup

# Run
make run
# → http://localhost:8080
```

Check health:
```bash
curl http://localhost:8080/health
# {"status":"ok","service":"axe"}
```

---

## Development Commands

```bash
make run           # Start server (hot-reload with air if installed)
make test          # Run all unit tests (< 30s)
make test-race     # Run with race detector
make lint          # golangci-lint
make vet           # go vet

make generate      # Run all code generators (Ent + sqlc + Wire)
make migrate-up    # Apply pending migrations
make migrate-down  # Rollback last migration
make seed          # Load test data

make docker-up     # Start PostgreSQL + Redis
make docker-down   # Stop services
make build         # Build binary to ./bin/axe
make clean         # Remove build artifacts
```

---

## Project Structure

```
axe/
├── cmd/
│   ├── api/main.go          # Composition Root
│   └── axe/main.go          # CLI tool (axe generate, axe migrate)
├── internal/
│   ├── domain/              # Entities + Interfaces ONLY (no infra imports)
│   ├── handler/             # HTTP layer (Chi)
│   ├── service/             # Business logic
│   └── repository/          # Data access (Ent writes, sqlc reads)
├── pkg/
│   ├── apperror/            # Error taxonomy
│   ├── txmanager/           # Transaction manager
│   ├── logger/              # Structured logging (slog)
│   └── validator/           # Input validation
├── ent/schema/              # Ent ORM schema definitions
├── db/
│   ├── migrations/          # SQL migrations
│   └── queries/             # sqlc SQL queries
├── config/                  # Cleanenv configuration
└── docs/
    ├── architecture_contract.md
    ├── data_consistency.md
    ├── dev_experience_spec.md
    └── adr/                 # Architecture Decision Records
```

---

## Architecture

See [`docs/architecture_contract.md`](docs/architecture_contract.md) for the full contract.

Key rules:
- `internal/domain/` — only stdlib imports, no infra
- `internal/handler/` — parse request, validate, call service, write response
- `internal/service/` — business rules, transactions, outbox events
- `internal/repository/` — DB access only (Ent for writes, sqlc for complex reads)

---

## axe CLI Generator

```bash
# Generate full CRUD resource
axe generate resource Post \
  --fields="title:string,body:text,published:bool,author_id:uuid" \
  --belongs-to="User"

# Output: 10 files generated across all layers
# ✅ internal/domain/post.go
# ✅ internal/handler/post_handler.go + test
# ✅ internal/service/post_service.go + test
# ✅ internal/repository/post_repo.go + post_query.go
# ✅ ent/schema/post.go
# ✅ db/migrations/YYYYMMDD_create_posts.sql
# ✅ db/queries/post.sql
```

---

## Reference Implementation

The `User` domain is the **canonical reference** for all other domains:
- [`internal/domain/user.go`](internal/domain/user.go)
- [`internal/handler/user_handler.go`](internal/handler/user_handler.go)
- [`internal/service/user_service.go`](internal/service/user_service.go)
- [`internal/repository/user_repo.go`](internal/repository/user_repo.go)

When in doubt → read User domain.

---

## Onboarding (1 day)

**Morning** (4h):
1. Read [`docs/architecture_contract.md`](docs/architecture_contract.md) → 30 min
2. `make setup && make run` → 30 min
3. Read User domain code end-to-end → 90 min
4. Run and read User tests → 30 min

**Afternoon** (4h):
1. `axe generate resource [YourDomain]`
2. Customize generated code
3. Write 1 business rule in service
4. Submit PR

---

## License

MIT
