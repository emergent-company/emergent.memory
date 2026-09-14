# Surface per-object drift detail; make resync pack-aware

**Status:** done
**Created:** 2026-09-09
**Source:** [2026-09-09-schema-migration-resync](../sessions/2026-09-09-schema-migration-resync.md)

## What

The migrations page's Schema health section shows only counts ("N of M objects are stale"). Memory's `/validate` already returns per-object `Results` (entity id, type, key, stored schema_version, issue text) but the gateway drops them (`migrationData` keeps only `TotalObjects`/`StaleObjects`). When drift spans more than one active pack, `suggestResync` picks only the most-recently-installed ACTIVE row (`gateway/migrations.go`), and the user cannot tell which pack owns the stale objects or whether the prefilled from==to target is the right one.

Add: surface the stale-object detail (grouped by type/pack) in Schema health, and either label the resync target by pack or let the user pick which active pack to re-stamp before Preview/Execute.

## Why

Directly observed in the field: "1 of 2 objects are stale" with a single agent-notes install — the number alone gives no pointer to which object or why (stamp lag vs NULL stamp vs content write). With multiple active packs the current single-target heuristic can point at the wrong pack.

## Depends on

None. Gateway + possibly a small memory contract check (whether `/validate` results identify the owning pack by type today; if not, map type → compiled pack client-side).

## Notes

- Gateway plumbing already exists: `memory.go` `ValidateSchemas` → `SchemaValidationResult` (fields `Results`), `ObjectValidationResult` carries `Type`/`Key`/`SchemaVersion`/`Issues`.
- Resync must stay behind Preview → Execute (dangerous-object gating preserved).

## Done (2026-09-10)

- `gateway/memory.go` — promoted the inline `Results` element to a named
  `ObjectValidationResult` (entity id, type, key, schema_version, issues); wire
  shape unchanged.
- `gateway/migrations.go` — `migrationData` now carries `StaleGroups`
  (`[]stalePackGroup` → type → objects), `ActivePacks`, and `SuggestedPack`.
  `groupStaleObjects` groups memory's per-object results by owning pack then
  object type (unresolved pack last, types and keys sorted). `activePacks` lists
  ACTIVE installs deduped by pack name. `suggestResyncPack` picks the active pack
  owning the most stale objects (tie → most recent install); with no mapping it
  falls back to `suggestResync`'s most-recently-installed active row.
  `staleTypePackIndex` maps Type → pack from memory's `/compiled-types`
  `schemaName`, falling back to the bundled pack definitions.
- `gateway/migrations.templ` — Schema health renders the grouped detail (pack
  heading, type, object key/entity id, and issue badges: NULL stamp vs stamp lag
  vs content write, with raw memory text). The resync form labels the suggested
  target pack; with more than one active pack it renders a `resyncPack` picker
  instead of the from/to selects. Preview → Execute gating and Force unchanged;
  `uiMigrate` maps a submitted `resyncPack` to from==to.
- `gateway/migrations_test.go` — tests for `activePacks`, `groupStaleObjects`,
  `suggestResyncPack`, `staleIssueLabel`, `migrationsData` retaining the grouped
  detail, render tests for the grouped detail and the pack picker/label, and a
  handler test that a bare resync submit previews (never executes) while the
  explicit Execute action still gates.

Verify: `templ generate && go build ./... && go test ./... && golangci-lint run ./...` clean.
