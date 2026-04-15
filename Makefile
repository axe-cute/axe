SHELL := /bin/bash
.DEFAULT_GOAL := help

# ─── Variables ────────────────────────────────────────────────────────────────
BINARY_NAME  := axe
MAIN_PATH    := ./cmd/api
BIN_DIR      := ./bin
GO           := $(shell which go 2>/dev/null || echo /usr/local/go/bin/go)
GOFLAGS      :=

# ─── Help ─────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ─── Development ──────────────────────────────────────────────────────────────
.PHONY: run
run: ## Run the API server (with air hot-reload if available)
	@if command -v air > /dev/null 2>&1; then \
		air; \
	else \
		$(GO) run $(MAIN_PATH)/main.go; \
	fi

.PHONY: build
build: ## Build the binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)/main.go
	@echo "✅ Built: $(BIN_DIR)/$(BINARY_NAME)"

# ─── Testing ──────────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run all tests
	$(GO) test ./... -timeout 60s

.PHONY: test-race
test-race: ## Run tests with race detector
	$(GO) test -race ./... -timeout 120s

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

.PHONY: test-integration
test-integration: ## Run Postgres integration tests (requires Docker)
	$(GO) test ./... -tags=integration -timeout 300s

.PHONY: test-integration-mysql
test-integration-mysql: ## Run MySQL integration tests (requires Docker)
	$(GO) test -tags=integration_mysql ./tests/integration/mysql/ -v -timeout 300s

.PHONY: test-integration-sqlite
test-integration-sqlite: ## Run SQLite integration tests (no Docker needed)
	$(GO) test -tags=integration_sqlite ./tests/integration/sqlite/ -v -timeout 120s

# ─── Code Quality ─────────────────────────────────────────────────────────────
.PHONY: lint
lint: ## Run golangci-lint
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "⚠️  golangci-lint not installed. Run: brew install golangci-lint"; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format code
	$(GO) fmt ./...
	@if command -v goimports > /dev/null 2>&1; then goimports -w .; fi

# ─── Code Generation ──────────────────────────────────────────────────────────
.PHONY: generate
generate: ## Run all code generators (Ent + sqlc + Wire)
	@echo "→ Generating Ent code..."
	$(GO) generate ./ent/...
	@echo "→ Generating sqlc code..."
	@if command -v sqlc > /dev/null 2>&1; then sqlc generate; else echo "⚠️  sqlc not installed"; fi
	@echo "→ Generating Wire code..."
	@if command -v wire > /dev/null 2>&1; then wire ./cmd/api/...; else echo "⚠️  wire not installed"; fi
	@echo "✅ Code generation complete"

.PHONY: generate-ent
generate-ent: ## Generate Ent ORM code only
	$(GO) generate ./ent/...

.PHONY: generate-sqlc
generate-sqlc: ## Generate sqlc query code only
	sqlc generate

# ─── Database ─────────────────────────────────────────────────────────────────
.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	@echo "→ Applying migrations..."
	$(GO) run ./cmd/axe/main.go migrate up

.PHONY: migrate-down
migrate-down: ## Rollback last migration
	@echo "→ Rolling back last migration..."
	$(GO) run ./cmd/axe/main.go migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GO) run ./cmd/axe/main.go migrate status

.PHONY: seed
seed: ## Load test/seed data
	$(GO) run ./db/seed/main.go

# ─── Docker ───────────────────────────────────────────────────────────────────
.PHONY: docker-up
docker-up: ## Start PostgreSQL + Redis + Jaeger via Docker Compose
	docker compose up -d
	@echo "✅ Services started"
	@echo "   PostgreSQL: localhost:5432"
	@echo "   Redis:      localhost:6379"

.PHONY: docker-down
docker-down: ## Stop and remove Docker containers
	docker compose down

.PHONY: docker-logs
docker-logs: ## Follow Docker Compose logs
	docker compose logs -f

# ─── Setup ────────────────────────────────────────────────────────────────────
.PHONY: setup
setup: ## Full local setup from zero
	@echo "→ Copying .env.example to .env..."
	@cp -n .env.example .env || true
	@echo "→ Starting Docker services..."
	@$(MAKE) docker-up
	@echo "→ Waiting for PostgreSQL..."
	@sleep 3
	@echo "→ Applying migrations..."
	@$(MAKE) migrate-up
	@echo "→ Seeding data..."
	@$(MAKE) seed
	@echo ""
	@echo "✅ Setup complete! Run: make run"

.PHONY: tidy
tidy: ## Run go mod tidy
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html tmp/
	@echo "✅ Cleaned"
