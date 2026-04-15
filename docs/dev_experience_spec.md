# 🧑‍💻 Developer Experience Spec
> Cam kết DX của axe platform.
> Mục tiêu: dev mới có thể ship feature trong 1 ngày làm việc đầu tiên.

---

## DX SLAs (Service Level Agreements)

| Metric | Target | Đo bằng gì |
|---|---|---|
| Tạo CRUD endpoint đầy đủ | ≤ 10 phút | axe CLI + stopwatch |
| Run full test suite | ≤ 30 giây | `make test` timing |
| Understand 1 handler | ≤ 5 phút | Linear readable code |
| Onboarding (new dev) | ≤ 1 ngày | README + pair session |
| `make run` từ clone | ≤ 2 phút | Docker Compose |

---

## axe CLI Generator

### Cài đặt
```bash
go install github.com/yourorg/axe/cmd/axe@latest
```

### Commands

```bash
# Tạo full CRUD resource
axe generate resource Post \
  --fields="title:string,body:text,published:bool,author_id:uuid" \
  --belongs-to="User"

# Tạo service only
axe generate service EmailNotification

# Tạo migration file
axe migrate create add_index_to_orders_user_id

# Chạy migration
axe migrate up

# Rollback 1 step
axe migrate down
```

### Output của `axe generate resource Post`

```
✅ internal/domain/post.go
✅ internal/handler/post_handler.go
✅ internal/handler/post_handler_test.go
✅ internal/service/post_service.go
✅ internal/service/post_service_test.go
✅ internal/repository/post_repo.go       (Ent writes)
✅ internal/repository/post_query.go      (sqlc reads)
✅ ent/schema/post.go
✅ db/migrations/YYYYMMDDHHMMSS_create_posts.sql
✅ db/queries/post.sql

Next steps:
  1. Review and customize generated files
  2. Run: axe migrate up
  3. Run: go generate ./ent/...
  4. Run: sqlc generate
  5. Register handler in cmd/api/main.go
     → r.Mount("/api/v1/posts", postHandler.Routes())
```

---

## Makefile Commands

```makefile
make run          # Start server (hot reload với air)
make test         # go test ./...
make test-race    # go test -race ./...
make lint         # golangci-lint run
make generate     # go generate ./... (Ent + sqlc + Wire)
make migrate-up   # Apply pending migrations
make migrate-down # Rollback last migration
make seed         # Load test data
make docker-up    # Start PostgreSQL + Redis
make docker-down  # Stop services
```

---

## Local Setup (từ zero đến running)

```bash
# Clone
git clone https://github.com/yourorg/axe && cd axe

# Start dependencies
make docker-up

# Copy env
cp .env.example .env

# Generate code & migrate
make generate
make migrate-up

# Seed test data
make seed

# Run
make run
# → http://localhost:8080
```

**Total time: < 5 phút** (Docker pull lần đầu không tính)

---

## Reference Implementation

`internal/domain/user.go`, `internal/handler/user_handler.go`, `internal/service/user_service.go`, `internal/repository/user_repo.go` là **reference implementation chuẩn**.

Mọi khi có doubt về cách implement một pattern → đọc User domain.

---

## Onboarding Path (1 ngày)

```
Buổi sáng (4 giờ):
  □ Đọc docs/architecture_contract.md (30 phút)
  □ Đọc docs/07_mockup.md (30 phút)
  □ Clone + make run thành công (30 phút)
  □ Đọc toàn bộ User domain code (90 phút)
  □ Chạy và đọc User domain tests (30 phút)

Buổi chiều (4 giờ):
  □ axe generate resource [tên domain mới]
  □ Customize generated code
  □ Viết 1 business rule trong service
  □ Viết test cho service
  □ Submit PR cho review
```
