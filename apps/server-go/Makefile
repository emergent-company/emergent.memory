.PHONY: build run dev test clean deps tidy fmt lint swagger swagger-check swagger-validate test-coverage test-coverage-html test-e2e test-e2e-coverage

# Build variables
BINARY_NAME=server-go
BUILD_DIR=./bin
CMD_DIR=./cmd/server
COVERAGE_DIR=./coverage
DOCS_DIR=./docs/swagger

# Go variables
GOFLAGS=-ldflags="-s -w"

# Default target
all: build

# Install dependencies
deps:
	go mod download

# Tidy dependencies
tidy:
	go mod tidy

# Format code
fmt:
	go fmt ./...
	gofumpt -l -w .

# Lint code
lint:
	golangci-lint run

# Build the binary (includes swagger generation)
build: swagger
	go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

# Run the server
run: build
	$(BUILD_DIR)/$(BINARY_NAME)

# Run with hot reload (requires air)
dev:
	air

# Run tests
test:
	go test -v ./...

# Run tests with coverage (text output)
test-coverage:
	@mkdir -p $(COVERAGE_DIR)
	go test -v -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

# Run tests with coverage (HTML output)
test-coverage-html:
	@mkdir -p $(COVERAGE_DIR)
	go test -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/index.html
	@echo "Coverage report generated: $(COVERAGE_DIR)/index.html"
	@echo "View at: http://localhost:3002/coverage (when server is running in debug mode)"

# Run E2E tests
test-e2e:
	./scripts/run-e2e-tests.sh

# Run E2E tests with coverage
test-e2e-coverage:
	@mkdir -p $(COVERAGE_DIR)
	go test -v -coverprofile=$(COVERAGE_DIR)/e2e-coverage.out ./tests/e2e/...
	go tool cover -html=$(COVERAGE_DIR)/e2e-coverage.out -o $(COVERAGE_DIR)/e2e-index.html
	@echo "E2E coverage report generated: $(COVERAGE_DIR)/e2e-index.html"

# Generate combined coverage (unit + e2e)
test-all-coverage:
	@mkdir -p $(COVERAGE_DIR)
	@echo "Running unit tests with coverage..."
	go test -coverprofile=$(COVERAGE_DIR)/unit.out ./domain/... ./pkg/... ./internal/...
	@echo "Running E2E tests with coverage..."
	go test -coverprofile=$(COVERAGE_DIR)/e2e.out ./tests/e2e/...
	@echo "Merging coverage profiles..."
	@echo "mode: set" > $(COVERAGE_DIR)/coverage.out
	@tail -n +2 $(COVERAGE_DIR)/unit.out >> $(COVERAGE_DIR)/coverage.out 2>/dev/null || true
	@tail -n +2 $(COVERAGE_DIR)/e2e.out >> $(COVERAGE_DIR)/coverage.out 2>/dev/null || true
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/index.html
	@echo "Combined coverage report: $(COVERAGE_DIR)/index.html"

# Generate OpenAPI/Swagger documentation
swagger:
	@mkdir -p $(DOCS_DIR)
	swag init -g cmd/server/main.go -o $(DOCS_DIR) --parseDependency --parseInternal
	@echo "Swagger docs generated: $(DOCS_DIR)/swagger.json"
	@echo "View at: http://localhost:3002/docs (when server is running in debug mode)"

# Generate swagger and format output
swagger-fmt: swagger
	@if command -v jq > /dev/null; then \
		jq . $(DOCS_DIR)/swagger.json > $(DOCS_DIR)/swagger.json.tmp && mv $(DOCS_DIR)/swagger.json.tmp $(DOCS_DIR)/swagger.json; \
	fi

# Check swagger annotation coverage (informational)
swagger-check:
	@./scripts/check-swagger-annotations.sh

# Validate swagger annotation coverage (strict - fails if coverage < 95%)
swagger-validate:
	@./scripts/check-swagger-annotations.sh --strict

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -rf $(COVERAGE_DIR)
	rm -rf $(DOCS_DIR)

# Install development tools
tools:
	go install github.com/air-verse/air@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/swaggo/swag/cmd/swag@latest

# Generate (if needed for future code generation)
generate:
	go generate ./...

# Docker build
docker-build:
	docker build -t $(BINARY_NAME):latest .

# Help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build & Run:"
	@echo "  build              - Build the binary"
	@echo "  run                - Build and run"
	@echo "  dev                - Run with hot reload (air)"
	@echo ""
	@echo "Testing:"
	@echo "  test               - Run all tests"
	@echo "  test-coverage      - Run tests with coverage (text output)"
	@echo "  test-coverage-html - Run tests with coverage (HTML report)"
	@echo "  test-e2e           - Run E2E tests"
	@echo "  test-e2e-coverage  - Run E2E tests with coverage"
	@echo "  test-all-coverage  - Generate combined coverage report"
	@echo ""
	@echo "Documentation:"
	@echo "  swagger            - Generate OpenAPI/Swagger docs"
	@echo "  swagger-fmt        - Generate and format swagger docs"
	@echo "  swagger-check      - Check annotation coverage (informational)"
	@echo "  swagger-validate   - Validate annotation coverage (fails if < 95%)"
	@echo ""
	@echo "Code Quality:"
	@echo "  fmt                - Format code"
	@echo "  lint               - Run linter"
	@echo "  tidy               - Tidy go.mod"
	@echo ""
	@echo "Setup:"
	@echo "  deps               - Download dependencies"
	@echo "  tools              - Install development tools"
	@echo "  clean              - Clean build artifacts"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build       - Build Docker image"
