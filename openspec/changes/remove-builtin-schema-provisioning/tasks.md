# remove-builtin-schema-provisioning — Tasks

## 1. Database migrations

- [x] 1.1 `00235_drop_builtin_schema_provisioning.sql`: drop the 00146 trigger/function, add
      `kb.graph_schemas.is_system boolean NOT NULL DEFAULT false`, mark `session-message-types`
      `is_system=true`.
- [x] 1.2 `00236_cleanup_builtin_schema_provisioning.sql`: remove auto-provisioned assignments,
      object/edge registry rows, and orphaned global blueprint-pack rows where no applied blueprint
      claims the pack.

## 2. Server: system schemas

- [x] 2.1 `schemas/entity.go`: add `IsSystem` to `GraphMemorySchema`, `ObjectTypeSchema`, and
      `RelationshipTypeSchema`.
- [x] 2.2 `schemas/builtin_seeder.go`: seed the system pack as `is_system=true`; drop the
      `ProvisionBuiltinSchemasToAllProjects` call.
- [x] 2.3 `schemas/repository.go`: delete `ProvisionBuiltinSchemasToAllProjects`; load `is_system=true`
      packs first in `GetCompiledTypesByProject` (project packs shadow system types); exclude
      `is_system` from `GetAssignablePack`; stamp `source_blueprint_id` on an existing assignment in
      `AssignPackWithTypes`.

## 3. Server: read-time resolution

- [x] 3.1 `extraction/schema_provider.go`: union `is_system=true` packs into `GetProjectSchemas`
      (project packs win).
- [x] 3.2 `mcp/schema_tools.go`: exclude `is_system` rows from the builtin fallback in
      `executeGetSchema`.

## 4. Server: blueprint packs materialize on apply

- [x] 4.1 `blueprints/builtin_seeder.go`: stop seeding schema packs at boot; keep
      `upsertBuiltinSchemaPack` for the apply path.

## 5. Web UI

- [x] 5.1 Wire `isSystem` through `CompiledType`, `SchemaObjectTypeView`, and `schemaPackSummary`;
      render a System badge on the schema browser subtitle, object-type origin, and packs list/owner
      label.

## 6. Tests

- [x] 6.1 Delete `schemas/builtin_provisioning_db_test.go`; update `blueprints/builtin_seeder_test.go`
      to expect zero seeded schema packs.
- [x] 6.2 Add coverage for the `is_system` column, system-pack read-time resolution, and the
      `source_blueprint_id` stamp on an existing assignment.

## 7. Verification

- [x] 7.1 `go build ./...` (server and gateway), `templ generate`, `go vet` on the changed packages.
