## Context

See `proposal.md` — Why. Current state that shapes this design (all verified against the repos):

- The Schema area (`gateway/schema.go`, `gateway/schema.templ`) is read-only: both `/schema` and `/schema/object-types/:name` only render `GetCompiledTypes`. There is no create/edit route and no `MemoryBackend` mutation method.
- `CompiledType` already carries `SchemaID`, `SchemaName`, `SchemaVersion`, `Shadowed` (`gateway/memory.go:1645`); `AppliedBlueprint` carries name/version/checksum but **no** `schemaId` (`gateway/blueprint_client.go:56`). The object-create form reads types solely from compiled types (`gateway/objects.go:318`).
- Memory persistence: schema packs are project-scoped rows (`kb.graph_schemas.project_id`); `CreatePack` always scopes to the caller's project. Blueprint apply creates a pack named from the manifest's `packs[].name`, assigns it with `Merge:true` and `SourceBlueprintID`, and records `kb.blueprint_pack_claims` (migration `00135`). Those provenance links are stored but **not exposed by any memory endpoint**; `ListApplied` deliberately omits them.
- `PUT /api/schemas/:packId` is a pack-level partial update that replaces the entire `object_type_schemas` / `relationship_type_schemas` blobs (no per-type route, no property validation). It is owner-scoped (`WHERE project_id = ?`), but `GetPackByNameVersion` is not project-scoped, so a blueprint's pack can in principle be reused by more than one project.
- Compiled-types merge: active packs ordered `installed_at ASC`; later installs win and earlier duplicate names are flagged `Shadowed`. There is no separate override table — a later-assigned project pack shadows an earlier one.
- Memory does expose `GET /api/schemas/projects/:pid/history` (`SchemaHistoryItem`: schemaId, name, version, active, installedAt, removedAt) and per-type CRUD under `/api/schema-registry`, but the gateway consumes neither for this surface.

## Goals / Non-Goals

**Goals:**

- Let users add a schema and create/edit object types from the Schema area, with edits reflected in compiled types (and therefore object-create).
- Resolve and display blueprint provenance for object types, and warn before an edit diverges from a blueprint.
- Surface the schema history the backend already exposes.
- Derive a blueprint draft (new blueprint or new version) from the project's current effective object types.

**Non-Goals:**

- Editing relationship types (object types only in this change).
- Deleting/uninstalling schemas (existing blueprints flow owns that).
- Server-side enforcement of the blueprint-derived warning; edits stay allowed.
- A new memory endpoint for per-type history (follow-up, not required here).
- iOS client changes.

## Decisions

### D1 — Edits target a project-owned pack; blueprint-derived edits use copy-on-write

Editing a project-authored type writes through to its project-owned pack via `PUT /api/schemas/:packId` (read-modify-write the full pack with `GetSchema`). Editing a **blueprint-derived** type MUST NOT mutate the blueprint's pack in place, because that pack is shared by name/version across projects and by the blueprint itself. Instead the edit is written into a dedicated, project-owned override pack (e.g. `project-schema-overrides`) and (re)assigned so, per compiled-types `installed_at` ordering, the override shadows the original type. Re-editing the same type updates the existing override.

- *Rationale*: preserves the user's intent (edit the effective type, with a warning) while keeping blast radius to the current project. Also makes the warning truthful: a blueprint update/reinstall re-assigns the blueprint pack and can supersede the override.
- *Alternatives*: in-place `PUT` of the blueprint pack (rejected — shared mutation); schema-registry `PUT /projects/:pid/types/:name` (rejected — writes `kb.project_object_schema_registry`, a store the compiled-types/object-create path does not read, so the edit would not appear); direct DB access (rejected — cross-repo).

### D2 — Provenance resolved gateway-side from blueprint manifests (v1)

For each compiled type, treated as blueprint-derived when its `SchemaName`/`SchemaVersion` matches a pack declared by an applied blueprint. Build the map per request from `ListAppliedBlueprints` → `GetBlueprint`/`ListBlueprintVersions` (manifest `packs[].name`/`version`). Types with no matching applied blueprint are project-authored.

- *Rationale*: memory stores the authoritative links (`source_blueprint_id`, `blueprint_pack_claims`) but exposes none of them, and the gateway cannot read the DB. Manifest matching is derivable from existing API responses.
- *Alternatives*: add a memory read endpoint exposing `source_blueprint_id`/claims (recommended follow-up, but cross-repo scope); name-only matching without version (rejected — ambiguous on versioned re-installs).
- *Ambiguity*: if two applied blueprints claim the same pack name/version, show the first and flag the ambiguity in a log; the follow-up endpoint removes this.

### D3 — Warning is UI-advisory, not server-enforced

The blueprint-derived warning is a confirmation step in the edit flow (names the owning blueprint, states the edit diverges and may be overwritten). The server accepts the mutation once confirmed.

- *Rationale*: the product goal is to allow edits and then derive a blueprint from them; blocking server-side would break that loop and add a policy layer the backend does not model. The copy-on-write override (D1) means the "overwrite" risk is scoped and reversible.

### D4 — History reuses existing schema history

The object-type detail view resolves the type's `SchemaID` and renders matching `SchemaHistoryItem`s (active marker, version, installedAt/removedAt), reusing `GetSchemaHistory`. When no entries exist or the call fails, the view renders a non-blocking notice and still shows the type.

- *Rationale*: no new backend surface is needed for a meaningful v1. Memory bumps `schema_version` on registry edits but exposes no per-type version list; adding one is a follow-up (Open Question).

### D5 — Blueprint derivation is assembled gateway-side

The derive action builds a blueprint manifest from the project's **effective** compiled types (post-override) using the existing manifest builders in `gateway/blueprint_manifest.go`, then:
- new blueprint → `POST /api/blueprints` (draft);
- new version of an existing blueprint → `POST /api/blueprints/:id/versions` to clone, then `PUT /api/blueprints/:id` to replace the manifest with the current types (draft-only per memory's contract).

Add `MemoryBackend`/client methods for update-blueprint and create-version (client already has create/apply/publish).

- *Alternatives*: a memory-side derive endpoint (rejected — assembled data is already available to the gateway and memory has no such primitive); deriving from raw packs (rejected — must capture effective/shadowed result, not every pack).

### D6 — "Add schema" reuses the existing install path

The Schema-area "Add schema" action lists available schemas and calls the same install logic as the blueprints gallery (registry/bundled), returning the assignment result (including conflicts). No new install semantics.

### D7 — Coordinate with `ui-data-authoring`

That change already plans `POST/PUT /api/schema/object-types` mapped onto the compiled-schema path and a "New draft" flow. This change owns edit/provenance/history/derive and MUST reuse those endpoints and the draft-authoring UI if it lands first; do not create a second endpoint family. If it has not landed, this change implements the edit half and the derive half, leaving creation to it.

## Risks / Trade-offs

- [Name-based provenance is brittle if a blueprint's manifest packs are renamed independently] → match name+version, log ambiguity, and add the memory provenance endpoint as a follow-up (Open Question).
- [Copy-on-write override ordering depends on `installed_at`] → assign the override after the blueprint pack and add a unit/integration test asserting the override's `Shadowed`/winner state in compiled types.
- [`PUT /api/schemas/:packId` replaces whole blobs] → always `GetSchema` → mutate the target type → write back, and test that unrelated types are preserved.
- [A blueprint's pack can be shared across projects] → this is exactly why D1 forbids in-place mutation; the override pack is created with `CreatePack` (project-scoped).
- [Scope creep from the memory backend] → keep v1 gateway-only; track the provenance/per-type-history endpoints as separate cross-repo tasks.
- [Overlap with `ui-data-authoring` causes duplicate endpoints] → enforce D7 in review; reuse, don't fork.
- [Warning fatigue] → only show the warning when the type is actually blueprint-derived, and keep it a single confirm step.

## Migration Plan

No data migration. New routes and UI are additive; the override pack is created on first blueprint-derived edit. Rollback is reverting the gateway changes; any override pack created remains as ordinary project schema data.

## Open Questions

- Should memory expose the stored blueprint→pack link and a per-type version history endpoint? Recommended follow-up; not required for this change.
- Should relationship types become editable in a follow-up?
- Final name/version scheme for the project-authored and override packs (coordinate with `ui-data-authoring`).
