## Context

The `memory schemas` CLI and server currently have a `--file` install path that accepts only JSON. Users writing schemas in YAML (the natural format for large, readable files) must manually convert before every install — a fragile, error-prone step. Additionally, there is no way to preview changes before installing (every install is blind), no bulk cleanup of old assignments, no provenance in compiled-types output, no audit trail of past assignments, and no mechanism to migrate live data when type names change.

All changes live within the existing `apps/server/domain/schemas/` domain and `tools/cli/internal/cmd/schemas.go` — no new domains or services required.

## Goals / Non-Goals

**Goals:**
- Accept YAML files natively via `--file` on `schemas install` and `schemas create`
- Add `memory schemas diff` to preview changes before installing
- Add `--all-except` / `--keep-latest` flags to `schemas uninstall` for bulk cleanup
- Add `--verbose` flag to `schemas compiled-types` to show schema version and shadow warnings
- Add `memory schemas history` with soft-delete migration so past assignments are visible
- Add `memory schemas migrate` to rename types/properties across live data

**Non-Goals:**
- Automatic conflict resolution during migration (user must specify renames explicitly)
- Schema registry publish/discovery (out of scope — this is local project schema management)
- GraphQL or non-REST API changes

## Decisions

### YAML parsing approach
**Decision:** Detect file extension (`.yaml`/`.yml`) in a shared `loadSchemaFile()` helper added to `tools/cli/internal/cmd/schemas.go`. Unmarshal YAML into `map[string]any`, re-marshal to JSON, then unmarshal into `CreatePackRequest`. No YAML dependency added — `gopkg.in/yaml.v3` already present.

**Alternative considered:** Add a YAML→JSON conversion at server level. Rejected because the CLI is the user-facing boundary; the server should receive clean JSON regardless of source.

### Diff as a client-side operation (v1)
**Decision:** Implement `schemas diff` fully client-side in v1: fetch old schema via `GetPack`, parse both schemas, compare types. No new server endpoint in v1.

**Alternative considered:** Server-side diff endpoint with affected-object counts. Deferred to v2 — adds server complexity; the client-side diff gives most of the value immediately.

### Soft-delete for history
**Decision:** Add `removed_at TIMESTAMPTZ` column to `kb.project_schemas`. `DeleteAssignment` sets `removed_at = NOW()` instead of hard-deleting. `GetInstalledPacks` filters `WHERE removed_at IS NULL`. New `GetAssignmentHistory` query returns all rows.

**Alternative considered:** Separate audit log table. Overkill for this use case — the assignment table itself is the natural place for this data.

### Migrate as a dry-run-first transaction
**Decision:** `schemas migrate` posts to a new `POST /api/schemas/projects/:projectId/migrate` endpoint. Request includes `from_schema_id`, `to_schema_id` (or inline schema JSON), explicit rename pairs (`from_type`→`to_type`, `from_property`→`to_property`), and `dry_run` flag. Server runs in a single transaction; dry-run returns counts without committing.

**Data touched:** `kb.graph_objects.type_name`, `kb.graph_objects.properties` JSONB, `kb.graph_edges.type_name`.

**Alternative considered:** Client-side migration via raw SQL. Rejected — migration must be atomic and auditable on the server.

## Risks / Trade-offs

- **Soft-delete migration** requires a Goose migration. If the migration fails mid-deploy, `DeleteAssignment` will error until the column exists. Mitigation: migration runs before code deploy; migration is additive (column added with `DEFAULT NULL`, no data rewritten).
- **YAML→JSON round-trip** may lose comments and ordering. Acceptable — schema files are already declarative; comments are not preserved in the stored schema.
- **Client-side diff** has no object-count data. Users may see "0 affected objects" even if live data exists if type registry diverged from objects table. Mitigation: clearly label counts as registry-based estimates and note v2 will add exact counts.
- **Migrate renames** use exact string matching on `type_name`. A typo in `--rename` silently updates zero rows. Mitigation: dry-run output shows "N objects affected" before committing; warn on zero-row renames.

## Migration Plan

1. Run Goose migration adding `removed_at` to `kb.project_schemas`.
2. Deploy server binary (hot-reload picks up; no restart needed unless new fx modules are added — none here).
3. Deploy CLI binary via `task cli:install`.
4. Rollback: revert to previous server binary + CLI; the `removed_at` column is nullable so old code ignores it safely.

## Open Questions

- Should `schemas migrate` also update `kb.graph_edges.type_name`? Yes — included in scope to keep graph consistent.
- Should `schemas history` show assignment metadata (project, org) or just schema name/version? Show schema name, version, installed_at, removed_at — sufficient for the use case.
