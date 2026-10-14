# session-message-types-blueprint — Tasks

## 1. Built-in blueprint pack

- [ ] 1.1 Add `builtin_packs/session-message-types/project.yaml` (name/version/description/author).
- [ ] 1.2 Add `builtin_packs/session-message-types/schemas/session-message-types.yaml` with
      `Session`, `Message`, and `has_message` copied verbatim from the removed schemas builtin seeder
      (including `x-embedding-target: true` on `Message.content`).
- [ ] 1.3 Add `Required []string` to `blueprints.ObjectTypeDef` so the manifest round-trip preserves
      each type's `required` list (previously carried by the schemas builtin seeder JSON).

## 2. Delete the global system pack seeding

- [ ] 2.1 Delete `schemas/builtin_seeder.go` and its fx wiring in `schemas/module.go`.

## 3. Remove the `is_system` concept

- [ ] 3.1 `schemas/repository.go`: drop the system-pack read-time union and `IsSystem` propagation.
- [ ] 3.2 `schemas/entity.go`: drop `IsSystem` from `GraphMemorySchema`, `ObjectTypeSchema`,
      `RelationshipTypeSchema`.
- [ ] 3.3 `extraction/schema_provider.go`: drop `IsSystem` and the system-pack union.
- [ ] 3.4 `mcp/schema_tools.go`: drop the `NOT is_system` builtin visibility filter.
- [ ] 3.5 Web UI: remove `isSystem`/`System` badge from `schema.templ`, `schema_packs.templ`,
      `schema_packs.go`, `schema.go`, `memory_schemas.go`; run `templ generate`.

## 4. Migration

- [ ] 4.1 `00242_session_message_types_blueprint.sql`: backfill assignment + registry rows for
      projects owning Session/Message objects, delete the global pack only when unreferenced, drop the
      `is_system` column and index. Down restores the column/index and deletes the backfilled rows.

## 5. Tests

- [ ] 5.1 Rework `schemas/system_schema_provisioning_test.go`: drop `is_system` assertions; keep the
      migration SQL splitter tests (00235–00242); add DB-backed coverage for blueprint seeding,
      apply-materialization, and the 00242 backfill.
- [ ] 5.2 Update `blueprints/builtin_seeder_test.go` for the fifth pack and remove `is_system` queries.

## 6. Verification

- [ ] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .` clean.
- [ ] 6.2 DB-backed tests with `REQUIRE_DB=1` (schemas, blueprints, backups).
- [ ] 6.3 Scratch-Postgres migration up→down→up round-trip.
- [ ] 6.4 `templ generate` + `go build ./...` in `apps/web-ui`.
- [ ] 6.5 `task lint` (or scoped linters).
- [ ] 6.6 `openspec validate session-message-types-blueprint --strict`.
