## Context

Two CLI commands install schemas into a project:
- `memory schemas install` — direct user action
- `memory blueprints` — declarative directory apply

Both call the same two API endpoints (`POST /api/schemas` then `POST /api/schemas/projects/:id/assign`) and result in identical stored records. The `source` column on `kb.graph_schemas` is hardcoded to `"manual"` for every schema regardless of origin. The CLI blueprint applier (`applier.go`) knows the source file path at runtime but discards it before making the API call. The list endpoints (`GetAvailablePacks` / `GetInstalledPacks`) are separate, so no single query returns all schemas together.

## Goals / Non-Goals

**Goals:**
- Persist the blueprint source (file path or remote URL) on `kb.graph_schemas.blueprint_source` when a schema is installed via `memory blueprints`
- Expose `blueprintSource` on `InstalledSchemaItem` and `MemorySchemaListItem` response types
- Add a unified `GET /api/schemas/projects/:id/all` endpoint that returns every schema (available + installed) in one response with an `installed` boolean and optional `blueprintSource`
- Update `memory schemas list` to show all schemas by default with STATUS and SOURCE columns

**Non-Goals:**
- Tracking blueprint source for agents or skills (out of scope)
- Retroactively back-filling `blueprint_source` for already-installed schemas
- Validating or resolving blueprint source URLs

## Decisions

### D1 — New `blueprint_source` column on `kb.graph_schemas`, not `kb.project_schemas`

**Chosen:** `kb.graph_schemas.blueprint_source text NULL`

**Why:** The source of truth for a schema's origin is the schema definition itself, not the project assignment. A schema installed from a blueprint is logically "from that blueprint" regardless of how many projects it's assigned to later. Putting the annotation on the schema record avoids duplication and keeps the API response simple (`InstalledSchemaItem` already joins `graph_schemas`).

**Alternative considered:** `kb.project_schemas.blueprint_source` — rejected because the same schema could be assigned to multiple projects and the origin annotation would have to be copied per-assignment, creating drift risk.

### D2 — Pass `blueprintSource` as an optional field on `CreatePackRequest`

**Chosen:** Add `BlueprintSource *string json:"blueprint_source,omitempty"` to `CreatePackRequest`. The repository `CreatePack` writes it to the new column (or `"manual"` when nil).

**Why:** The blueprint applier already constructs `CreatePackRequest`; this is the minimal change point. No new API endpoint needed.

**Alternative considered:** A separate `PATCH /api/schemas/:id/blueprint-source` endpoint — rejected as over-engineered for an annotation set once at creation time.

### D3 — Unified list endpoint returns a merged slice with an `installed` discriminator

**Chosen:** `GET /api/schemas/projects/:id/all` returns `[]UnifiedSchemaItem` where each item carries `installed bool`, `installedAt *time.Time`, `assignmentId *string`, and `blueprintSource *string`.

**Why:** The blueprint applier (`fetchExistingPacks`) already manually merges both lists; this deduplication logic belongs in the server. The CLI and any future UI client get a single call.

**Alternative considered:** Extending existing endpoints with a query param — rejected because the response shapes are currently incompatible (`MemorySchemaListItem` vs `InstalledSchemaItem`).

### D4 — `memory schemas list` defaults to unified view; `--installed` / `--available` flags become filters

**Chosen:** Default behaviour shows all schemas. `--installed` and `--available` flags narrow the view (existing behaviour preserved when flags are used).

**Why:** Matches the user-stated goal of "one place to see everything". The flags allow scripts that depend on the filtered output to continue working.

## Risks / Trade-offs

- **Schema already installed before this change has `blueprint_source = NULL`** — accepted; will display as `manual` in the CLI. Back-fill out of scope.
- **`fetchExistingPacks` in applier.go makes 2 API calls today** — after this change it can be replaced with 1 call to `/all`, reducing latency. Migration is optional (the old 2-call path still works).
- **Goose migration adds a nullable column** — fully backwards compatible; no application code breaks if the migration hasn't run yet on a given environment.

## Migration Plan

1. Add Goose migration: `ALTER TABLE kb.graph_schemas ADD COLUMN blueprint_source text;`
2. Deploy server (hot reload handles handler/service/store changes; migration auto-runs on startup via Goose)
3. Deploy CLI build (`task cli:install`)
4. Rollback: `ALTER TABLE kb.graph_schemas DROP COLUMN blueprint_source;` (data loss of annotation only — no graph data affected)

## Open Questions

- Should the unified list endpoint also include schemas from other projects in the same org (org-visible)? Current plan: yes, matching the existing ownership filter in `GetAvailablePacks`.
- Should `memory blueprints` `--upgrade` update `blueprint_source` when upgrading a pack? Current plan: no — source is set once at creation time.
