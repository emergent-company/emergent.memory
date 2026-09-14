# Backfill UUID-keyed migration archive entries

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-review-bot-and-backlog](../sessions/2026-09-13-review-bot-and-backlog.md)

## What

Objects migrated before emergent.memory PR #415 have `kb.schema_migration_archive` entries keyed
with the schema-id **UUID** for `from_version`/`to_version`. After #415 the archive keys are
**human versions**, and `RollbackObject` matches by human version — so those older entries can
still never be found and rolled back.

Decide and implement: backfill old archive rows (map UUID → pack human version by joining the
pack table), or accept forward-only semantics and document it.

## Why

Forward-only means pre-#415 migrations remain non-rollbackable, which is a silent data-safety
gap for any project that migrated before the fix.

## Depends on

- emergent.memory PR #415 (human-version archive keys) — merged.

## Notes

- Table: `kb.schema_migration_archive`; pack versions live in the graph-memory schemas table.
- If backfilling, make it an idempotent SQL migration (no ORM), and guard rows already in
  human-version form.
- Also confirm `RestoreTypeRegistry` (#418/#422) behaves sensibly for pre-#415 rows once keyed.
