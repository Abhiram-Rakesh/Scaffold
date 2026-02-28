.PHONY: build test lint clean install snapshot release

BINARY_NAME = scaffold
MAIN_PACKAGE = ./cmd/scaffold
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -s -w \
	-X github.com/scaffold-tool/scaffold/pkg/version.Version=$(VERSION) \
	-X github.com/scaffold-tool/scaffold/pkg/version.Commit=$(COMMIT) \
	-X github.com/scaffold-tool/scaffold/pkg/version.BuildDate=$(BUILD_DATE)

## build: Build the binary for current platform
build:
	@echo "→ Building scaffold $(VERSION)..."
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "✓ Built: bin/$(BINARY_NAME)"

## install: Build and install to /usr/local/bin
install: build
	@echo "→ Installing to /usr/local/bin/$(BINARY_NAME)..."
	sudo cp bin/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@echo "✓ Installed"

## test: Run unit tests
test:
	@echo "→ Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✓ Tests complete. Coverage report: coverage.html"

## test-short: Run tests without integration tests
test-short:
	go test -v -short ./...

## lint: Run golangci-lint
lint:
	@which golangci-lint > /dev/null 2>&1 || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

## fmt: Format code
fmt:
	gofmt -s -w .
	goimports -w .

## vet: Run go vet
vet:
	go vet ./...

## tidy: Tidy go modules
tidy:
	go mod tidy

## snapshot: Build snapshot release with goreleaser (no publish)
snapshot:
	goreleaser release --snapshot --clean

## release: Create a tagged release (requires GITHUB_TOKEN)
release:
	goreleaser release --clean

## clean: Remove built artifacts
clean:
	rm -rf bin/ dist/ coverage.out coverage.html

## help: Show this help message
help:
	@echo "Scaffold Build Commands"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
