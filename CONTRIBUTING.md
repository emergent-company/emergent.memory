# Contributing to Spec Server 2

Thank you for your interest in contributing to Spec Server 2! This guide will help you understand our development practices, coding standards, and architectural patterns.

---

## Table of Contents

1. [Getting Started](#getting-started)
2. [Development Workflow](#development-workflow)
3. [Database Patterns](#database-patterns)
4. [Code Style Guidelines](#code-style-guidelines)
5. [Testing](#testing)
6. [Documentation](#documentation)
7. [Pull Request Process](#pull-request-process)

---

## Getting Started

### Prerequisites

- Go 1.25+
- Docker and Docker Compose
- PostgreSQL 16+ (via Docker)
- Git
- Taskfile (`task`)

### Initial Setup

1. **Clone the repository:**

   ```bash
   git clone https://github.com/your-org/spec-server-2.git
   cd spec-server-2
   ```

2. **Set up environment:**

   ```bash
   cp .env.example .env
   # Edit .env with your local configuration
   ```

3. **Start services:**

   ```bash
   # Start Docker dependencies (PostgreSQL, Zitadel)
   docker compose -f docker/docker-compose.dev.yml up -d
   ```

4. **Verify setup:**

   ```bash
   # Build the server
   task build

   # Run the server
   ./apps/server-go/dist/server
   ```

---

## Development Workflow

### Running Services

We use `task` for managing services:

```bash
# Start server with hot reload (requires air)
task dev

# Build the server
task build

# Run unit tests
task test

# Run integration tests
task test:integration
```

### Database Migrations

We use `bun` for migrations within the Go server:

```bash
# Run pending migrations
task migrate:up

# Show migration status
task migrate:status
```

---

## Database Patterns

Spec Server 2 uses `bun` as a SQL-first ORM for Go. We emphasize:
- **Type Safety:** Using Go structs for database models.
- **Multi-tenancy:** Respecting Row-Level Security (RLS) in PostgreSQL.
- **Strategic SQL:** Using raw SQL via `bun` for complex or performance-critical queries.

---

## Code Style Guidelines

### Formatting

We use standard `gofmt` and `goimports`. It is recommended to configure your IDE to run these on save.

### Go Conventions

- Follow [Effective Go](https://golang.org/doc/effective_go.html).
- Use `PascalCase` for exported symbols and `camelCase` for internal ones.
- Keep functions small and focused.
- Handle all errors explicitly.

---

## Testing

### Test Structure

We use standard Go testing patterns:

```go
func TestExample(t *testing.T) {
    // ...
}
```

### Integration Tests

Integration tests run against a real PostgreSQL instance. Ensure your local DB is running before executing `task test:integration`.

---

## Documentation

Keep documentation in the `/docs` directory and update `README.md` or `GEMINI.md` when introducing major architectural changes.

---

## Pull Request Process

### Before Submitting

1. **Run linter**: `task lint`
2. **Run tests**: `task test`
3. **Format code**: `task fmt`
4. **Update documentation**: If you changed APIs or patterns.

### PR Guidelines

- **Title**: Use conventional commits format (e.g., `feat: add user profile service`).
- **Description**: Explain what changed and why.
- **Tests**: Add tests for new functionality.

### Conventional Commits

We use conventional commits for clear changelog generation:

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Test changes
- `chore:` - Build/tooling changes
- `perf:` - Performance improvements
