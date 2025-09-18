# BLE CLI Tool Makefile

# Variables
BINARY_NAME=blecli
COVERAGE_DIR=coverage
GO_PACKAGES=$(shell go list ./...)

# Global LuaJIT flags for maximum performance (all builds use LuaJIT)
export CGO_LDFLAGS := -L/opt/homebrew/lib
export PKG_CONFIG_PATH := /opt/homebrew/lib/pkgconfig
BUILD_FLAGS := -tags luajit

# Default target
.PHONY: all
all: build test

# Build the application with LuaJIT (Go-LuaHit for maximum performance)
.PHONY: build
build:
	@echo "Building $(BINARY_NAME) with LuaJIT for maximum performance..."
	go build $(BUILD_FLAGS) -o $(BINARY_NAME) ./cmd/blecli

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)
	rm -rf $(COVERAGE_DIR)

# Run all tests or specific test by name
.PHONY: test
test:
	@if [ -z "$(TEST)" ]; then \
		echo "Running all tests..."; \
		go test $(BUILD_FLAGS) -v ./...; \
	else \
		echo "Running specific test: $(TEST)..."; \
		go test $(BUILD_FLAGS) -v -run $(TEST) ./...; \
	fi

# Run tests with race detection
.PHONY: test-race
test-race:
	@echo "Running tests with race detection..."
	go test $(BUILD_FLAGS) -race -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	mkdir -p $(COVERAGE_DIR)
	go test $(BUILD_FLAGS) -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report generated: $(COVERAGE_DIR)/coverage.html"

# Show coverage stats
.PHONY: coverage
coverage: test-coverage
	@echo "Coverage summary:"
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

# Run benchmarks
.PHONY: bench
bench:
	@echo "Running benchmarks..."
	go test $(BUILD_FLAGS) -bench=. -benchmem ./...

# Run benchmarks with CPU profiling
.PHONY: bench-cpu
bench-cpu:
	@echo "Running benchmarks with CPU profiling..."
	mkdir -p $(COVERAGE_DIR)
	go test $(BUILD_FLAGS) -bench=. -benchmem -cpuprofile=$(COVERAGE_DIR)/cpu.prof ./...
	@echo "CPU profile saved: $(COVERAGE_DIR)/cpu.prof"

# Run benchmarks with memory profiling
.PHONY: bench-mem
bench-mem:
	@echo "Running benchmarks with memory profiling..."
	mkdir -p $(COVERAGE_DIR)
	go test $(BUILD_FLAGS) -bench=. -benchmem -memprofile=$(COVERAGE_DIR)/mem.prof ./...
	@echo "Memory profile saved: $(COVERAGE_DIR)/mem.prof"

# Lint the code
.PHONY: lint
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		go vet ./...; \
	fi

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Check for security issues
.PHONY: security
security:
	@echo "Running security checks..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not found. Install with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"; \
	fi

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	go mod tidy

# Verify dependencies
.PHONY: verify
verify:
	@echo "Verifying dependencies..."
	go mod verify

# Full quality check
.PHONY: check
check: fmt lint test-race test-coverage security
	@echo "All quality checks completed!"

# Install development tools
.PHONY: install-tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest

# Run specific package tests
.PHONY: test-device
test-device:
	@echo "Running device package tests..."
	go test $(BUILD_FLAGS) -v ./pkg/device

.PHONY: test-ble
test-ble:
	@echo "Running BLE package tests..."
	go test $(BUILD_FLAGS) -v ./pkg/ble

.PHONY: test-config
test-config:
	@echo "Running config package tests..."
	go test $(BUILD_FLAGS) -v ./pkg/config

.PHONY: test-cli
test-cli:
	@echo "Running CLI tests..."
	go test $(BUILD_FLAGS) -v ./cmd/blecli

# Package-specific benchmarks
.PHONY: bench-device
bench-device:
	@echo "Running device package benchmarks..."
	go test $(BUILD_FLAGS) -bench=. -benchmem ./pkg/device

.PHONY: bench-ble
bench-ble:
	@echo "Running BLE package benchmarks..."
	go test $(BUILD_FLAGS) -bench=. -benchmem ./pkg/ble

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build          - Build the application"
	@echo "  test           - Run all tests or specific test (TEST=<test_name>)"
	@echo "  test-race      - Run tests with race detection"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  coverage       - Show coverage summary"
	@echo "  bench          - Run benchmarks"
	@echo "  bench-cpu      - Run benchmarks with CPU profiling"
	@echo "  bench-mem      - Run benchmarks with memory profiling"
	@echo "  lint           - Run linter"
	@echo "  fmt            - Format code"
	@echo "  security       - Run security checks"
	@echo "  check          - Run full quality check"
	@echo "  clean          - Clean build artifacts"
	@echo "  tidy           - Tidy dependencies"
	@echo "  verify         - Verify dependencies"
	@echo "  install-tools  - Install development tools"
	@echo "  help           - Show this help message"