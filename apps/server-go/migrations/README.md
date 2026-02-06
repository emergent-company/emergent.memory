# Database Migrations with Goose

This document describes the database migration workflow using [Goose](https://pressly.github.io/goose/) for the Go server.

## Overview

The Go server uses Goose for database migrations. Migrations are stored in `apps/server-go/migrations/` as SQL files and are embedded in the binary using Go's `embed` package.

## Directory Structure

```
apps/server-go/
├── migrations/
│   ├── embed.go              # Go embed directive for SQL files
│   ├── 00001_baseline.sql    # Baseline schema (full export)
│   └── 00002_*.sql           # Future migrations
├── cmd/migrate/
│   └── main.go               # CLI tool for running migrations
└── internal/migrate/
    └── migrate.go            # Programmatic migration API
```

## Migration Commands

### Using the CLI Tool

```bash
cd apps/server-go

# Check migration status
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c status

# Run all pending migrations
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c up

# Run migrations up to a specific version
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c up-to -v 2

# Rollback the last migration
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c down

# Get current database version
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c version

# Create a new migration
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c create add_new_table

# Mark a migration as applied (for existing databases)
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c mark-applied -v 1
```

### Environment Variables

| Variable            | Description                       | Default    |
| ------------------- | --------------------------------- | ---------- |
| `DATABASE_URL`      | Full PostgreSQL connection string | -          |
| `DB_HOST`           | Database host                     | localhost  |
| `DB_PORT`           | Database port                     | 5432       |
| `POSTGRES_USER`     | Database user                     | emergent   |
| `POSTGRES_PASSWORD` | Database password                 | (required) |
| `POSTGRES_DATABASE` | Database name                     | emergent   |
| `DB_SSL_MODE`       | SSL mode                          | disable    |

## Creating New Migrations

### 1. Create the Migration File

Migrations follow the naming convention: `{version}_{name}.sql`

```bash
# Using the CLI
go run ./cmd/migrate -c create add_user_preferences

# Or manually create
touch migrations/00002_add_user_preferences.sql
```

### 2. Write the Migration

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE kb.user_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES core.user_profiles(id),
    key TEXT NOT NULL,
    value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_preferences_user_id ON kb.user_preferences(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.user_preferences;
-- +goose StatementEnd
```

### 3. Run the Migration

```bash
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c up
```

## Goose Directives

- `-- +goose Up` - Marks the start of the "up" migration
- `-- +goose Down` - Marks the start of the "down" migration
- `-- +goose StatementBegin` / `-- +goose StatementEnd` - For multi-statement blocks
- `-- +goose NO TRANSACTION` - Disable transaction wrapping (for CREATE INDEX CONCURRENTLY, etc.)

## Best Practices

### 1. Always Include Down Migrations

Even if you don't plan to rollback, include down migrations for:

- Development flexibility
- Testing migration scripts
- Emergency rollbacks

### 2. Use Schema-Qualified Names

Always use schema prefixes (`kb.`, `core.`) for table names:

```sql
CREATE TABLE kb.new_table (...)  -- Good
CREATE TABLE new_table (...)     -- Bad
```

### 3. Make Migrations Idempotent

Use `IF NOT EXISTS` / `IF EXISTS` where possible:

```sql
CREATE TABLE IF NOT EXISTS kb.my_table (...);
CREATE INDEX IF NOT EXISTS idx_my_index ON kb.my_table(...);
DROP TABLE IF EXISTS kb.my_table;
```

### 4. Split Large Migrations

For large schema changes, split into multiple migrations:

- One for adding new tables
- One for data migration
- One for removing old tables

### 5. Test Migrations

```bash
# Apply
POSTGRES_PASSWORD=... go run ./cmd/migrate -c up

# Verify
POSTGRES_PASSWORD=... go run ./cmd/migrate -c status

# Rollback
POSTGRES_PASSWORD=... go run ./cmd/migrate -c down

# Re-apply
POSTGRES_PASSWORD=... go run ./cmd/migrate -c up
```

## Migrating Existing Databases

For databases that already have the schema (e.g., migrating from TypeORM):

```bash
# 1. Ensure goose_db_version table exists
POSTGRES_PASSWORD=... go run ./cmd/migrate -c status

# 2. Mark the baseline as applied
POSTGRES_PASSWORD=... go run ./cmd/migrate -c mark-applied -v 1

# 3. Verify
POSTGRES_PASSWORD=... go run ./cmd/migrate -c status
```

## Programmatic Usage

The `internal/migrate` package provides a programmatic API:

```go
import (
    "context"
    "github.com/emergent/emergent-core/internal/migrate"
)

func runMigrations(migrator *migrate.Migrator) error {
    ctx := context.Background()

    // Run all pending migrations
    if err := migrator.Up(ctx); err != nil {
        return err
    }

    // Or run up to a specific version
    if err := migrator.UpTo(ctx, 5); err != nil {
        return err
    }

    // Get current version
    version, err := migrator.Version(ctx)
    if err != nil {
        return err
    }

    return nil
}
```

## Transition from TypeORM

The Go server now owns database migrations. The NestJS server's TypeORM migrations in `apps/server/src/migrations/` are frozen and will not be used for new changes.

### Migration Ownership

- **Before**: TypeORM (NestJS) owned migrations
- **After**: Goose (Go) owns migrations

### Sync Workflow (During Transition)

1. Schema changes are made via Goose migrations in `apps/server-go/migrations/`
2. The test schema at `apps/server-go/internal/testutil/schema.sql` must be kept in sync
3. TypeORM entities in NestJS may need updates for compatibility (if NestJS is still in use)

## Troubleshooting

### Migration Fails

```bash
# Check current state
POSTGRES_PASSWORD=... go run ./cmd/migrate -c status

# Try rolling back
POSTGRES_PASSWORD=... go run ./cmd/migrate -c down

# Fix the migration file and retry
POSTGRES_PASSWORD=... go run ./cmd/migrate -c up
```

### Version Mismatch

If `goose_db_version` shows a different version than expected:

```sql
-- Check the table directly
SELECT * FROM goose_db_version ORDER BY id;
```

### Embed Issues

If migrations aren't being found:

1. Ensure `embed.go` exists in `migrations/`
2. Ensure migration files end in `.sql`
3. Rebuild: `go build ./...`
