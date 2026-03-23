## Why

Schemas installed via `memory blueprints` and schemas installed via `memory schemas install` are stored identically — both record `source = "manual"` — making it impossible to see where a schema came from or to list all schemas in one view. Users have no way to audit what came from a blueprint versus what was installed directly.

## What Changes

- Add a `blueprint_source` field to the `kb.graph_schemas` table to record the origin URL or name when a schema is installed from a blueprint.
- Pass the blueprint source through the CLI applier → API → repository chain so it is persisted at install time.
- Expose `blueprintSource` on `InstalledSchemaItem` and `MemorySchemaListItem` response types.
- Add a unified `GET /api/schemas/projects/:id/all` endpoint (or extend the existing list endpoints) so all schemas — available and installed — can be retrieved in a single call.
- Update `memory schemas list` CLI command to show both installed and available schemas together, with a `SOURCE` column indicating `manual`, a blueprint name, or a blueprint URL.

## Capabilities

### New Capabilities

- `schema-blueprint-annotation`: Record and expose the blueprint source for schemas installed via `memory blueprints`, and surface that annotation in the CLI and API list responses.
- `unified-schema-list`: Single CLI command and API endpoint that returns all schemas for a project (installed + available) in one response, optionally filterable by status.

### Modified Capabilities

<!-- No existing spec-level capabilities are changing — these are net-new behaviors. -->

## Impact

- **Database**: `kb.graph_schemas` — new nullable `blueprint_source text` column (migration required).
- **API**: `schemas` domain — `CreatePackRequest` gains optional `BlueprintSource` field; `InstalledSchemaItem` / `MemorySchemaListItem` gain `BlueprintSource *string`; new or extended list endpoint.
- **CLI**: `tools/cli/internal/blueprints/applier.go` — pass blueprint source name/URL when calling `CreatePack`; `tools/cli/internal/cmd/schemas.go` — unified list command with status + source columns.
- **No breaking changes** — the new field is additive; existing clients receive `null` for `blueprintSource`.
