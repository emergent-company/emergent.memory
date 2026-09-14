# Memory rollback may leave objects stale (schema_version = schema-id UUID)

**Status:** done
**Created:** 2026-09-09
**Landed:** 2026-09-10 (emergent.memory PR #415; registry-restore follow-ups #418/#422)
**Source:** [2026-09-09-schema-migration-resync](../sessions/2026-09-09-schema-migration-resync.md)

## Done (2026-09-10)

Migration archive keys are now consistently **human versions**: `ExecuteSchemaMigration`
resolves from/to pack human `Version` strings and passes them to `MigrateObject`, so the
archive `from_version`/`to_version` match what `RollbackObject` queries and what
`ValidateObjects` compares. Rollback now stamps `schema_version` from the archive's
`from_version` (was wrongly `req.ToVersion`). Also fixed the silently-broken
`kb.schema_migration_runs` INSERT (it referenced nonexistent columns). Forward-only; existing
UUID-keyed archives were not backfilled (they already failed to roll back). PR #415.

## What

In **/root/emergent.memory** (separate repo), verify and fix the rollback path's `schema_version` stamping. Code read during the resync investigation indicates migration archive keys are written as the **schema-id UUID** while `ValidateObjects` compares object `schema_version` against the pack's **human version string** — so a rollback restores objects stamped with a UUID that never matches the compiled version, and the restored objects can themselves be flagged stale immediately after rollback.

Suspect sites: `apps/server/domain/schemas/service.go` (migration execute / rollback, `toVersion := req.ToSchemaID` area ~:671-700) and `apps/server/domain/graph/migration.go` (`RollbackObject` ~:304-371). Confirm the exact stamp written on rollback before changing anything.

## Why

Rollback should restore object data without creating fresh drift; a rollback that turns objects stale defeats its purpose and sends the user back to the resync flow.

## Depends on

None. Confirmation of current stamp semantics first (read the two suspect files, ideally reproduce with a rollback of a migrated object).

## Notes

- Cross-repo: this task tracks work in `emergent.memory`, not `memory.web-ui`. The web UI's Rollback form (`gateway/migrations.templ` `rollbackForm`) posts a human `toVersion` string to `/blueprints/migrate/rollback`; semantics of that version string vs the schema-id UUID should be part of the fix decision.
- Keep the canonical stamp consistent with `ValidateObjects`'s compiled human-version comparison.
