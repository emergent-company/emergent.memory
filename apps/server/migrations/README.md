# Database Migrations with Goose

This document describes the database migration workflow using [Goose](https://pressly.github.io/goose/) for the Go server.

## Overview

The Go server uses Goose for database migrations. Migrations are stored in `apps/server/migrations/` as SQL files and are embedded in the binary using Go's `embed` package.

## Directory Structure

```
apps/server/
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
cd apps/server

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

# Apply pending migrations, allowing out-of-order (missing) migrations
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c up -allow-missing

# Verify post-conditions (detect indexes left invalid by killed concurrent DDL)
POSTGRES_PASSWORD=your-password go run ./cmd/migrate -c verify
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

## Merge Order and Out-of-Order Migrations

Goose orders migrations by the numeric version parsed from `NNNNN_name.sql`. If it finds a
migration whose version is **lower than a version already recorded** in `goose_db_version`
it refuses to run and the server crash-loops:

```
Error running migrations: found 2 missing migrations before current version 172:
        version 170: 00170_embedding_indexes_hnsw.sql
        version 171: 00171_graph_relationships_embedding_hnsw.sql
```

This happens when a higher-numbered migration merges and is applied **before** a
lower-numbered one (issue #750). Each PR was individually valid; the merge order produced
the gap.

### CI guard (required check)

The `Migration Order Guard` job fails a PR that **adds** a migration below the base
branch's maximum version. It compares the PR's added `.sql` files against `origin/main`'s
highest version. Renames/reformats are covered by the separate `Migration Immutability`
job, and a PR that adds no migrations always passes.

To run it locally:

```bash
cd apps/server
go run ./cmd/migration-order-guard -base origin/main
```

**Remediation for a violation:** renumber the migration **above** the base maximum. The
failure message names the next free version.

```bash
git mv apps/server/migrations/00170_thing.sql \
       apps/server/migrations/00177_thing.sql
```

**Escape hatch — deliberately filling a gap.** Adding a migration below the base max is
sometimes legitimate (the lower version was never applied anywhere and you are filling the
gap). Mark the added migration with a reason:

```sql
-- out-of-order-migration-allowed: filling the 00170/00171 gap (issue #750)
```

The reason is required; a bare directive does not exempt the file.

### Applying a gap to an existing database

If a gap already exists on a running environment, apply the missing migrations
out-of-order:

```bash
POSTGRES_PASSWORD=... go run ./cmd/migrate -c up -allow-missing
```

The runner names the missing files and this remedy in its error output.

## Verifying Out-of-Band Version Records

`mark-applied` (and a manual `INSERT INTO goose_db_version`) records a version **without
running its migration**, so Goose can never run it later and any objects it should have
created are unchecked. Migration `00164` is the cautionary example: its version was
recorded while a `CREATE INDEX CONCURRENTLY` never took effect (issue #734).

`mark-applied` prints a loud warning, and post-conditions can be checked at any time:

```bash
POSTGRES_PASSWORD=... go run ./cmd/migrate -c verify
```

`verify` runs the read-only query below and fails if any index in `kb`/`core` is not both
`indisvalid` and `indisready` — the residue a killed concurrent index build leaves:

```sql
SELECT n.nspname AS schema, c.relname AS table, i.relname AS index
FROM pg_index x
JOIN pg_class c ON c.oid = x.indrelid
JOIN pg_class i ON i.oid = x.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname IN ('kb', 'core')
  AND NOT (x.indisvalid AND x.indisready)
ORDER BY n.nspname, c.relname, i.relname;
```

This check finds indexes that **exist** in the catalog but are not usable. An object that is
missing entirely has no `pg_index` row, so it is **not** reported here — that is the case
`00164` actually hit. When a version is recorded without its object, verify the expected
objects yourself and repair forward with a new migration; `00175` is the example (its
`DO`-block guard raises if the HNSW index is missing or invalid, so goose refuses to record
the version). Migrations `00170`/`00175` show the in-migration `indisvalid` post-condition
guard convention.

Remediation: `REINDEX INDEX CONCURRENTLY <index>`, or re-run the owning forward migration.

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

For databases that already have the schema (e.g., an existing production database):

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
    "github.com/emergent-company/emergent.memory/internal/migrate"
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
