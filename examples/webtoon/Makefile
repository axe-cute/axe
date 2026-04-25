SHELL := /bin/bash
.DEFAULT_GOAL := help

# ─── Variables ────────────────────────────────────────────────────────────────
BINARY_NAME  := api
MAIN_PATH    := ./cmd/api
BIN_DIR      := ./bin
GO           := $(shell which go 2>/dev/null || echo /usr/local/go/bin/go)

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
build: ## Build the API binary
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)/main.go
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

# ─── Database ─────────────────────────────────────────────────────────────────
.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	$(GO) run ./cmd/axe/main.go migrate up

.PHONY: migrate-down
migrate-down: ## Rollback last migration
	$(GO) run ./cmd/axe/main.go migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GO) run ./cmd/axe/main.go migrate status

.PHONY: seed
seed: ## Seed demo data (8 series + episodes)
	$(GO) run ./cmd/seed

# ─── Code Generation ──────────────────────────────────────────────────────────
.PHONY: generate
generate: ## Run go generate
	$(GO) generate ./...

# ─── Docker ───────────────────────────────────────────────────────────────────
.PHONY: docker-up
docker-up: ## Start services via Docker Compose
	docker compose up -d
	@echo "✅ Services started"

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
	@echo "→ Waiting for database..."
	@sleep 3
	@echo "→ Applying migrations..."
	@$(MAKE) migrate-up
	@echo ""
	@echo "✅ Setup complete! Run: make run"

.PHONY: tidy
tidy: ## Run go mod tidy
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html tmp/
	@echo "✅ Cleaned"
