## Why

Every `source='builtin'` schema pack is swept into every project. A database trigger (migration
00146) and a startup reconcile (`ProvisionBuiltinSchemasToAllProjects`) both insert a
`kb.project_schemas` row linking each built-in pack to every project. That drags the four blueprint
packs (`agent-notes`, `dreaming-agent`, `operator`, `task-board`) into projects that never installed
the blueprint, and it is the only reason the Session/Message conversation primitives show up. A
project's schema should reflect the blueprints it actually installed, not the platform's pack list.

## What Changes

- Stop auto-assigning built-in schema packs to projects: drop the 00146 trigger/function, delete the
  startup `ProvisionBuiltinSchemasToAllProjects` reconcile, and remove the provisioning call from the
  schemas builtin seeder.
- Make the platform's conversation primitives (Session/Message) read-time-resolved **system** schemas:
  one `is_system=true` registry row (`source='builtin'`, name `session-message-types`) available in
  every project through the compiled-types and extraction-schema paths, with no `kb.project_schemas`
  row and never auto-assigned.
- Add `kb.graph_schemas.is_system boolean NOT NULL DEFAULT false`; `session-message-types` is the only
  `is_system=true` row.
- Materialize built-in blueprint packs only on blueprint apply (the defensive
  `resolvePack` → `upsertBuiltinSchemaPack` path); the blueprints seeder no longer seeds schema packs at
  boot.
- Clean up prior auto-provisioned assignments, registry rows, and orphaned global pack rows for
  blueprint packs that no applied blueprint claims.

## Capabilities

### Modified Capabilities
- `session-message-types`: Session and Message are read-time-resolved system schemas (is_system=true)
  rather than pre-registered into every project at startup.
- `schema-navigation`: the schema packs list gains a `System` owner for the read-time system pack and
  drops the "global pack without an owning blueprint" case.
- `schema-editing`: object-type provenance renders a `System` badge for system-pack types instead of
  the project-authored "Not from an installed blueprint" marker.
- `blueprint-registry`: pack-assignment ownership is stamped only when unowned (or the same blueprint
  name family), and unapply removes only assignments the blueprint owns while leaving ones another
  applied blueprint still claims.

## Impact

- Server: `domain/schemas` (entity, builtin seeder, repository compiled-types / assignable-pack /
  assign-stamping), `domain/extraction` (schema provider), `domain/mcp` (schema tool),
  `domain/blueprints` (builtin seeder).
- Migrations `00235_drop_builtin_schema_provisioning.sql` and `00236_cleanup_builtin_schema_provisioning.sql`.
- Web UI: system-type provenance (System badge) on the schema browser, object-type origin, and schema
  packs list.
- No change to the system embedding policy for Message content.
