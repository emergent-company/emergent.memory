## Why

The Session/Message conversation primitives are shipped as a global **system** schema pack: a single
`kb.graph_schemas` row (`source='builtin'`, `is_system=true`, name `session-message-types`) resolved
at read time into every project's compiled types and extraction schemas, with no
`kb.project_schemas` row and never auto-assigned. This `is_system`/read-time-resolution mechanism
exists only for this one pack and is an awkward special case: a project's schema should reflect the
blueprints it actually applied, not a hidden always-on pack.

## What Changes

- Ship the Session/Message conversation primitives as a normal built-in blueprint pack
  (`builtin_packs/session-message-types/`), seeded as a global published blueprint
  (`project_id NULL`, `origin='builtin'`) by the existing blueprints builtin seeder. The pack
  materializes only when the blueprint is applied (the existing `resolvePack` →
  `upsertBuiltinSchemaPack` → `AssignPackWithTypes` path).
- Delete the schemas-domain builtin seeder (`schemas/builtin_seeder.go`) and its fx registration —
  there is no longer a global system pack to seed.
- Remove the `is_system` concept entirely: the `kb.graph_schemas.is_system` column, the
  `graph_schemas_is_system_idx` partial index, and the read-time system-pack unions in compiled-types
  and extraction-schema paths. `IsSystem` is dropped from the schema entities, the compiled-types
  API/JSON, and the web-ui `System` provenance badge.
- Migration `00242` preserves visibility for projects that already own Session/Message graph objects:
  it assigns the existing global `session-message-types` pack (and materializes its registry rows) to
  exactly those projects, then drops the `is_system` column and index.

## Capabilities

### Modified Capabilities
- `session-message-types`: Session/Message/`has_message` are provided by the built-in blueprint
  `session-message-types` instead of a read-time-resolved system pack; the `is_system` concept is
  removed.
- `blueprint-registry`: the built-in blueprint list grows to include `session-message-types`.
- `schema-navigation`: the schema packs list drops the `System` owner; packs come only from applied
  blueprints, manual installs, and project-authored packs.
- `schema-editing`: object-type provenance drops the `System` badge; there is no read-time system pack.

## Impact

- Server: `domain/schemas` (entity, repository compiled-types / assignable-pack, builtin seeder
  deleted), `domain/extraction` (schema provider), `domain/mcp` (schema tool), `domain/blueprints`
  (manifest `ObjectTypeDef.required`, new builtin pack).
- Migration `00242_session_message_types_blueprint.sql`.
- Web UI: remove the `System` provenance badge and `isSystem` plumbing.
- No change to the system embedding policy for Message content (out of scope).
