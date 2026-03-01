<instructions>
You are executing the `agent-go-migrations` skill.
Your goal is to help the user create, write, and run database migrations in this Go project.

The project uses [Goose v3](https://pressly.github.io/goose/) for migrations.
Migration files live at `apps/server-go/migrations/` and are embedded in the binary.
The migration CLI is at `apps/server-go/cmd/migrate/main.go`.

---

## Running Migrations

Always prefer the Taskfile shortcuts (they handle working directory automatically):

| What you want | Command |
|---|---|
| Check status | `task migrate:status` |
| Apply all pending | `task migrate:up` |
| Roll back one step | `task migrate:down` |

To run a specific version or use advanced commands, use the CLI directly from `apps/server-go/`:

```bash
cd apps/server-go

# Run up to a specific version
go run ./cmd/migrate -c up-to -v 5

# See current version number
go run ./cmd/migrate -c version

# Mark a migration as already-applied (skip running it)
go run ./cmd/migrate -c mark-applied -v 3
```

---

## Creating a New Migration

### Step 1 — Generate the file

From the repo root, run:

```bash
cd apps/server-go && go run ./cmd/migrate -c create <name>
```

Use `snake_case` for the name. Examples:
- `add_user_preferences`
- `create_event_log_table`
- `add_index_on_documents_project_id`

This produces `apps/server-go/migrations/<next_version>_<name>.sql`.
The version number is auto-incremented (e.g. `00035_add_user_preferences.sql`).

### Step 2 — Write the SQL

Every migration file must have an `Up` block and a `Down` block:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE kb.user_preferences (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES core.user_profiles(id),
    key         TEXT        NOT NULL,
    value       JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_preferences_user_id ON kb.user_preferences(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.user_preferences;
-- +goose StatementEnd
```

### Step 3 — Apply it

```bash
task migrate:up
```

### Step 4 — Verify

```bash
task migrate:status
```

---

## Goose SQL Directives

| Directive | Purpose |
|---|---|
| `-- +goose Up` | Start of the forward migration |
| `-- +goose Down` | Start of the rollback migration |
| `-- +goose StatementBegin` / `-- +goose StatementEnd` | Wrap multi-statement blocks |
| `-- +goose NO TRANSACTION` | Disable wrapping in a transaction (required for `CREATE INDEX CONCURRENTLY`) |

`NO TRANSACTION` example:

```sql
-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_docs_embedding
    ON kb.documents USING ivfflat (embedding vector_cosine_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_docs_embedding;
```

---

## Naming and Schema Conventions

- **File names**: `{5-digit-version}_{snake_case_description}.sql`
  — example: `00035_add_user_preferences.sql`
- **Always schema-qualify table names** — use `kb.` or `core.` prefixes:
  ```sql
  CREATE TABLE kb.new_table (...)   -- correct
  CREATE TABLE new_table (...)      -- wrong
  ```
- **Use `IF NOT EXISTS` / `IF EXISTS`** to make migrations idempotent:
  ```sql
  CREATE TABLE IF NOT EXISTS kb.my_table (...);
  DROP TABLE IF EXISTS kb.my_table;
  CREATE INDEX IF NOT EXISTS idx_name ON kb.my_table(...);
  ```
- **Always write the `Down` block** — even if rollback is unlikely.
- **Split large changes** into multiple migrations: one for new tables, one for data backfill, one for dropping old columns.

---

## Test the Full Round-Trip

After writing a migration, always verify up/down/up works cleanly:

```bash
task migrate:up      # apply
task migrate:status  # check version
task migrate:down    # roll back
task migrate:up      # re-apply
task migrate:status  # confirm
```

---

## Troubleshooting

**Migration fails mid-run**
```bash
task migrate:status   # see where it stopped
task migrate:down     # roll back the failed step
# fix the SQL, then:
task migrate:up
```

**Database already has the schema (onboarding an existing DB)**
```bash
# 1. Check what version the DB thinks it is
task migrate:status

# 2. Mark the baseline as applied without running it
cd apps/server-go && go run ./cmd/migrate -c mark-applied -v 1

# 3. Apply any subsequent migrations normally
task migrate:up
```

**Embed not picking up new file**
Ensure the file ends in `.sql` and is inside `apps/server-go/migrations/`.
The `embed.go` file there covers all `*.sql` files automatically — no edits needed.

---

## Steps to Follow

1. Understand what schema change the user wants.
2. Generate the migration file with `go run ./cmd/migrate -c create <name>` from `apps/server-go/`.
3. Write the `Up` and `Down` SQL blocks — apply conventions above.
4. Run `task migrate:up` to apply.
5. Run `task migrate:status` to confirm it applied.
6. If the user wants to test rollback: `task migrate:down`, then `task migrate:up`.
7. Report the migration file path and the version number it received.

**Important:** Never edit an already-applied migration. If a fix is needed, create a new migration that corrects it.
</instructions>
