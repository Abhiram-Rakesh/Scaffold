# Contributing to Scaffold

Thank you for your interest in contributing! Here's how to get started.

## Development Setup

```bash
git clone https://github.com/scaffold-tool/scaffold
cd scaffold
go mod download
make build
```

**Requirements:**
- Go 1.21+
- golangci-lint (installed automatically by `make lint`)

## Running Tests

```bash
make test          # Unit tests with coverage
make test-short    # Skip integration tests
make lint          # Run golangci-lint
```

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- All public functions must have Go doc comments
- Errors should be wrapped with context: `fmt.Errorf("doing X: %w", err)`
- AWS operations must be idempotent (check-before-create pattern)

## Pull Request Process

1. Create a feature branch from `main`: `git checkout -b feat/your-feature`
2. Write tests for new functionality
3. Ensure `make test lint` passes
4. Write a clear PR description explaining the change and motivation
5. Reference any related issues

## Commit Messages

Follow Conventional Commits:
- `feat:` — New feature
- `fix:` — Bug fix
- `docs:` — Documentation
- `test:` — Tests
- `refactor:` — Code refactoring
- `chore:` — Build/tooling changes

## Reporting Issues

Please include:
- Scaffold version (`scaffold version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant error messages or logs (`--verbose` flag)
