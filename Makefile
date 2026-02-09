# Dorgu Makefile

# Variables
BINARY_NAME=dorgu
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Go commands
GO=go
GOBUILD=$(GO) build
GOTEST=$(GO) test
GOMOD=$(GO) mod
GOFMT=gofmt

# Directories
CMD_DIR=./cmd/dorgu
BUILD_DIR=./build

.PHONY: all build clean test fmt lint check install install-hooks tidy

# Default target
all: build

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME)"

## install: Install the binary to $GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(LDFLAGS) $(CMD_DIR)
	@echo "Installed to $$(go env GOPATH)/bin/$(BINARY_NAME)"

## run: Run the application
run: build
	$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## test: Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

## check: Run same checks as CI (gofmt, vet, test). Run before pushing.
check:
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt' to fix formatting. Files needing change:" && gofmt -l . && exit 1)
	@echo "Running go vet..."
	$(GO) vet ./...
	@echo "Running tests..."
	$(GOTEST) ./...
	@echo "Check passed (matches CI)."

## lint: Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

## tidy: Tidy go modules
tidy:
	@echo "Tidying modules..."
	$(GOMOD) tidy

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download

## generate: Run go generate
generate:
	@echo "Running go generate..."
	$(GO) generate ./...

## release: Build release binaries for all platforms
release:
	@echo "Building release binaries..."
	@mkdir -p $(BUILD_DIR)/release
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-arm64 $(CMD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-arm64 $(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	@echo "Release binaries built in $(BUILD_DIR)/release/"

## goreleaser: Run goreleaser locally (dry run)
goreleaser:
	@which goreleaser > /dev/null || (echo "Install goreleaser: https://goreleaser.com/install/" && exit 1)
	goreleaser release --snapshot --clean

## dev: Run with hot reload (requires air)
dev:
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air

## install-hooks: Install git hooks (run 'make check' before each push)
install-hooks:
	@mkdir -p .git/hooks
	@cp .githooks/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Installed pre-push hook. Run 'make check' before every push."

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

# Example usage shortcuts
.PHONY: example-generate

## example-generate: Run generate on example app
example-generate: build
	$(BUILD_DIR)/$(BINARY_NAME) generate ./testdata/node-app --dry-run
