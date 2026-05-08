.PHONY: all build test lint clean proto rust help dev proto-check release-manifest-check test-smoke module-boundary

# Default target
all: build

# ============================================================================
# BUILD
# ============================================================================

# Build core binary
build:
	@echo "Building Aegion core..."
	go build -o bin/aegion ./cmd/aegion

# Build with race detector (for testing)
build-race:
	go build -race -o bin/aegion ./cmd/aegion

# Build all module images
build-images:
	./scripts/build-all.sh

# Build a single module image
build-module:
	@if [ -z "$(MODULE)" ]; then echo "Usage: make build-module MODULE=password"; exit 1; fi
	@if ! printf %s "$(MODULE)" | grep -Eq "^[A-Za-z0-9_-]+$$"; then echo "Invalid MODULE: use only letters, numbers, _, -"; exit 1; fi
	./scripts/build-module.sh "$(MODULE)"

# ============================================================================
# DEVELOPMENT
# ============================================================================

# Run in development mode
dev:
	go run ./cmd/aegion --config configs/aegion.yaml

# Start development environment
dev-up:
	docker-compose -f deploy/docker-compose.yml up -d

# Stop development environment
dev-down:
	docker-compose -f deploy/docker-compose.yml down

# View development logs
dev-logs:
	docker-compose -f deploy/docker-compose.yml logs -f

# ============================================================================
# TESTING
# ============================================================================

# Run all tests
test:
	go test -v ./...

# Run tests with coverage
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run integration tests
test-integration:
	go test -v -tags=integration ./...

# Run focused end-to-end smoke tests
test-smoke:
	./scripts/run-integration-smoke.sh

# Run Rust tests
test-rust:
	cd rust && cargo test --workspace

# ============================================================================
# LINTING
# ============================================================================

# Run all linters
lint: lint-go lint-rust

# Run Go linter
lint-go:
	golangci-lint run ./...

# Run Rust linter
lint-rust:
	cd rust && cargo clippy --workspace -- -D warnings

# Format code
fmt:
	go fmt ./...
	cd rust && cargo fmt

# ============================================================================
# CODE GENERATION
# ============================================================================

# Generate protobuf stubs
proto:
	./scripts/gen-proto.sh

# Verify generated protobuf stubs are up-to-date
proto-check:
	./scripts/check-proto-consistency.sh

# Generate Rust CGo bindings
rust-bindings:
	./scripts/gen-rust-bindings.sh

# Generate all
generate: proto rust-bindings

# Validate release manifest schema and module coverage
release-manifest-check:
	./scripts/check-release-manifest.sh

# Validate per-module import boundaries (MODULE=name)
module-boundary:
	@if [ -z "$(MODULE)" ]; then echo "Usage: make module-boundary MODULE=password"; exit 1; fi
	./scripts/check-module-boundaries.sh $(MODULE)

# ============================================================================
# DATABASE
# ============================================================================

# Run migrations
migrate:
	go run ./cmd/aegion -migrate -config configs/aegion.yaml

# Create a new migration
migrate-create:
	@if [ -z "$(NAME)" ]; then echo "Usage: make migrate-create NAME=create_users"; exit 1; fi
	@if ! printf %s "$(NAME)" | grep -Eq "^[A-Za-z0-9_-]+$$"; then echo "Invalid NAME: use only letters, numbers, _, -"; exit 1; fi
	migrate create -ext sql -dir core/migrations -seq "$(NAME)"

# Rollback last migration
migrate-down:
	@echo "Rollback is not exposed via cmd/aegion. Use migration tooling directly."
	@exit 1

# ============================================================================
# RUST
# ============================================================================

# Build Rust libraries
rust:
	cd rust && cargo build --release

# Build Rust libraries (debug)
rust-debug:
	cd rust && cargo build

# Clean Rust builds
rust-clean:
	cd rust && cargo clean

# ============================================================================
# DOCKER
# ============================================================================

# Build base images
docker-base:
	docker build -f build/Dockerfile.base -t aegion/base:latest .
	docker build -f build/Dockerfile.base-runtime -t aegion/base-runtime:latest .

# ============================================================================
# CLEANUP
# ============================================================================

# Clean all build artifacts
clean:
	rm -rf bin/
	rm -rf coverage.out coverage.html
	cd rust && cargo clean

# ============================================================================
# HELP
# ============================================================================

help:
	@echo "Aegion Makefile"
	@echo ""
	@echo "Build:"
	@echo "  make build          - Build core binary"
	@echo "  make build-images   - Build all module Docker images"
	@echo "  make build-module   - Build single module (MODULE=name)"
	@echo ""
	@echo "Development:"
	@echo "  make dev            - Run in development mode"
	@echo "  make dev-up         - Start dev environment (Docker)"
	@echo "  make dev-down       - Stop dev environment"
	@echo ""
	@echo "Testing:"
	@echo "  make test           - Run all tests"
	@echo "  make test-cover     - Run tests with coverage"
	@echo "  make test-smoke     - Run focused integration smoke tests"
	@echo "  make test-rust      - Run Rust tests"
	@echo ""
	@echo "Code Generation:"
	@echo "  make proto          - Generate protobuf stubs"
	@echo "  make proto-check    - Verify protobuf stubs are up-to-date"
	@echo "  make rust-bindings  - Generate Rust CGo bindings"
	@echo "  make release-manifest-check - Validate build/release-manifest.json"
	@echo "  make module-boundary MODULE=name - Check module import boundaries"
	@echo ""
	@echo "Database:"
	@echo "  make migrate        - Run migrations"
	@echo "  make migrate-create - Create new migration (NAME=name)"
	@echo "  make migrate-down   - Not supported via cmd/aegion (fails intentionally)"
	@echo ""
	@echo "Linting:"
	@echo "  make lint           - Run all linters"
	@echo "  make fmt            - Format code"
