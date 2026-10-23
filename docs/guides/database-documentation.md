# Database Documentation with DBML

This guide explains how to maintain `docs/database/schema.dbml`, the DBML
definition of the PostgreSQL schema.

## Overview

`docs/database/schema.dbml` is generated from a **live** PostgreSQL catalog after
migrations have been applied. The generator is a small Go command in this repo:

```
apps/server/cmd/schema-dbml/main.go
```

It replaces the Nx-era `npm run db:docs:generate` script, which was removed along
with the workspace tooling — the checked-in DBML then drifted for dozens of
migrations, so the file no longer matched the schema. Do not hand-edit it; the
next regeneration overwrites the file.

### Related Documentation

- **`docs/database/schema.dbml`** — complete DBML definition (generated)
- **`docs/database/schema-context.md`** — concise, AI-oriented schema summary
  (hand-maintained)

## Generating

Apply migrations first, then generate against the same database:

```bash
task migrate:up
DATABASE_URL="postgres://user:pass@host:5432/db" \
  go run ./apps/server/cmd/schema-dbml -out docs/database/schema.dbml
```

The generator reads only the catalog; it never writes to the database. Flags:

| Flag | Meaning |
|---|---|
| `-database-url` | Connection string (falls back to `DATABASE_URL`) |
| `-out` | Output path, default `docs/database/schema.dbml` |
| `-check` | Exit 1 when the file differs from the catalog |

`-check` fails when a migration changed the schema and the document was not
regenerated; the `server` workflow runs it after migrating the test database, so a
stale file fails CI.

```bash
go run ./apps/server/cmd/schema-dbml -check
```

The output is stable across the PostgreSQL majors we run: CI uses `pg17`,
production uses `pg16`. Both produce byte-identical files, so it is safe to
regenerate on either.

### Scoping

Only the schemas the product owns are documented: `core`, `kb` and `public`.
`pg_catalog`, `information_schema` and extension-owned schemas are excluded.
Everything inside those three schemas is included, including migration
bookkeeping such as `public.goose_db_version`.

### Determinism

The output is stable, so a regeneration after an unchanged schema produces no
diff:

- tables and enums are ordered by schema, then name
- columns keep their catalog order
- references follow constraint order
- defaults and comments are normalized to single lines

## Validating

DBML is parsed strictly, so validate before committing. The reference parser is
`@dbml/cli`:

```bash
npx --package @dbml/cli -- dbml2sql --postgres docs/database/schema.dbml -o /tmp/schema.sql
```

Any syntax error is reported with a line number. The generated DDL also applies
cleanly to an empty PostgreSQL database once the extensions are created (`vector`,
`pgcrypto`, `uuid-ossp`, `btree_gin`), which is a stronger check than parsing: every
table and foreign key survives the round trip.

## DBML File Structure

### Enums

Custom enum types are declared before the tables that use them:

```dbml
Enum "kb"."document_conversion_status" {
  "pending"
  "completed"
}
```

### Tables

Each table lists its columns with their constraints, then any secondary indexes:

```dbml
Table "kb"."chunks" {
  "id" uuid [pk, default: `gen_random_uuid()`]
  "document_id" uuid [not null]
  "embedding" vector(768)
  "tsv" tsvector

  Indexes {
    (document_id) [name: "idx_chunks_document_id"]
    (embedding) [name: "idx_chunks_embedding_hnsw"]
  }
}
```

Column comments from the catalog become `note:` settings. Table comments are
emitted as a `// schema.table:` comment immediately *below* the `Table` block, so
they never separate a table's name from its body.

### Relationships

One `Ref` per foreign key; composite keys keep their column tuple:

```dbml
Ref: "kb"."chunks"."document_id" > "kb"."documents"."id"
Ref: "kb"."adk_states".("app_name", "user_id", "session_id") > "kb"."adk_sessions".("app_name", "user_id", "id")
```

Every key is rendered many-to-one (`>`), including the handful that are unique.
DBML's one-to-one operator (`-`) is direction-less: `dbml2sql` has to choose which
side holds the foreign key, and it can place it on the parent — inverting the
relationship the catalog records. `>` is a weaker statement about those keys, never
a wrong one, and it keeps the direction intact both for readers and for the SQL
round trip.

## Known Limitations

- **Index method is not represented.** DBML index entries carry the name, columns
  and uniqueness, but not `USING hnsw` / `ivfflat`. See the migration that created
  the index for the operator class and parameters.
- **Partial index predicates are not represented.** A predicate such as
  `WHERE deleted_at IS NULL` is dropped.
- **Expression indexes are emitted verbatim** inside backticks; they are valid
  DBML but do not round-trip into an equivalent `CREATE INDEX` expression.
- **Extensions are not declared.** `pgvector` is assumed to exist because the
  column types reference it.
- **Check constraints are not represented.** A `CHECK (...)` on a column or table
  is emitted nowhere; read the migration that added it.
- **Composite unique constraints appear as indexes.** They are rendered as
  `Indexes { (a, b) [unique] }` rather than a named constraint, so a reader
  looking for a constraint name finds none.

## Best Practices

1. **Regenerate after migrations** — same PR as the migration, so review sees both.
2. **Never hand-edit** — the generator overwrites the file.
3. **Run `-check` in review** — a stale file is a documentation bug.
4. **Put meaning in comments** — `COMMENT ON TABLE/COLUMN` survives regeneration;
   hand-written DBML notes do not.

## Troubleshooting

### Connection errors

1. Confirm the database is running and migrated: `task migrate:status`
2. Confirm `DATABASE_URL` points at the migrated database (not a scratch one)
3. Check the schemas exist: `\dn` should list `core`, `kb` and `public`

### Empty output

`schema-dbml` writes zero tables when the schema list does not match. Check that
migrations ran against the database you passed in.

### Validation errors

1. Re-run the generator rather than editing the file
2. If the error persists, the catalog contains an identifier the renderer does not
   quote correctly — fix `quote()` in `apps/server/cmd/schema-dbml/main.go`

## Schema Context for AI

`schema-context.md` is a hand-maintained quick reference for AI assistants: a
short table list, query patterns and naming conventions. It is deliberately
concise — `schema.dbml` is the complete reference.

Update it when a migration adds or renames tables, or adds a commonly-queried
column:

1. Apply the migration and regenerate `schema.dbml`
2. Edit `schema-context.md` to match, and update its "Last Updated" line
3. Validate the DBML

## Resources

- [DBML documentation](https://dbml.dbdiagram.io/docs/)
- [@dbml/cli](https://www.npmjs.com/package/@dbml/cli)
- [Migrations](../technical/DATABASE_MIGRATIONS.md)
