## Why

The Schema area is a read-only browser: users can inspect compiled object types but cannot add a schema or edit a type from it. They also cannot tell that a type came from an installed blueprint, so edits silently diverge from the blueprint that owns it, and schema history is scattered across the Migrations and Blueprints pages. Users want to evolve their schema in place from the UI and, when their edits are worth keeping, preserve them as a new blueprint.

## What Changes

- **Add a schema from the Schema area**: an "Add schema" action that installs an available schema pack (bundled/registry) without leaving `/schema`, reusing the existing install path.
- **Edit object types in the Schema area**: a type editor for description and typed properties (add/remove/edit properties), persisted via a gateway write path; type name stays immutable on edit.
- **Blueprint-derived provenance + warning**: each object type shows its owning schema/blueprint, and a type that originates from an installed blueprint is flagged as blueprint-derived. Editing such a type shows a warning that the change diverges from the blueprint and may be overwritten by a blueprint update/reinstall.
- **Schema history in the Schema area**: surface the history that the memory backend already exposes for a type's schema (assignment history / blueprint versions with active marker and timestamps) on the type detail view. Per-type version history is not currently exposed by the backend — captured as a scoped backend follow-up rather than silently assumed.
- **Derive a blueprint from edits**: from the project's current object types, create a blueprint draft (new blueprint or a new version of an existing one) that captures the changes, so an edited schema can be preserved and reinstalled.
- **Gateway write + derivation paths**: new `MemoryBackend` methods and HTTP routes for object-type mutation and blueprint derivation; the memory backend is a separate repo, so its gaps are tracked explicitly.

## Capabilities

### New Capabilities
- `schema-editing`: adding/installing a schema from the Schema area, creating and editing object types with typed properties, blueprint-derived provenance warnings, and viewing schema history — all from the Schema area.

### Modified Capabilities
- `blueprint-api`: derive a blueprint draft (or a new version of an existing blueprint) from the project's current object types, driven by schema edits.
- `blueprint-gallery`: expose the "create blueprint from current schema" action and link blueprint provenance back to the owning blueprint.

## Impact

- **Gateway UI**: `gateway/schema.go`, `gateway/schema.templ` (add/edit/provenance/history UI), `gateway/blueprints.templ` (derive action), `gateway/main.go` (new routes).
- **Gateway data layer**: `gateway/backend.go` (`MemoryBackend` gains object-type create/update/delete and blueprint-derive methods), `gateway/memory.go` (`/api/schemas`, `/api/schema-registry` mutations), `gateway/blueprint_client.go` (create/update/new-version already exist client-side; expose via new routes).
- **Memory backend (separate repo, `emergent.memory`)**: `PUT /api/schemas/:packId` and `/api/schema-registry/.../types` CRUD already exist; a per-type version-history endpoint does not and is a scoped follow-up. No existing gateway endpoint derives a blueprint manifest from project types — derivation can be assembled gateway-side from compiled types (reuse `blueprint_manifest.go`).
- **Specs**: `blueprint-api` currently documents `PATCH/DELETE /api/blueprints/assignments/:id` that are not implemented; reconcile while touching this surface.
- **Testing**: TDD — unit tests for the type mutation payloads, provenance resolution, history mapping, and derive-manifest assembly. Deterministic; e2e optional.
- **Related in-flight change**: `ui-data-authoring` plans initial object-type *creation* and blueprint *draft authoring* (`POST/PUT /api/schema/object-types`, "New draft"). This change owns the *edit/provenance/history/derive* surface and MUST reuse, not fork, those endpoints if `ui-data-authoring` lands first.
