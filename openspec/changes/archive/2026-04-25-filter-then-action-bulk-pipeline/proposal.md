## Why

Production agent memory systems accumulate large object graphs and need lifecycle management — archiving stale facts, deleting old sessions, marking low-confidence data. The current approach (paginate → iterate client-side → update each object) is slow, race-prone, and wasteful. This ships a server-side filter-then-action pipeline that executes bulk operations atomically in the DB.

## What Changes

- New bulk operation endpoints: `POST /graph/objects/bulk-action` — accepts a filter + action in a single request
- Supported actions: `update_status`, `soft_delete`, `hard_delete`, `merge_properties`, `replace_properties`, `add_labels`, `remove_labels`, `set_labels`
- Filter reuses existing `PropertyFilter` semantics plus time-relative shorthand (`"90d"`, `"12h"`)
- `dry_run` mode returns match count without executing
- Safety limits: default 1000, configurable up to 100000
- Audit log entry written for each bulk operation
- CLI: `memory graph objects bulk-update` and `memory graph objects bulk-delete`

## Capabilities

### New Capabilities

- `bulk-object-operations`: Server-side filter-then-action pipeline for bulk update/delete on graph objects with dry-run support and audit logging

### Modified Capabilities

- (none — purely additive)

## Impact

- `apps/server/domain/graph/` — new bulk handler, service methods, store queries
- PostgreSQL `UPDATE ... WHERE` / `DELETE ... WHERE` for efficient execution
- `tools/cli/` — new `bulk-update` / `bulk-delete` subcommands on `memory graph objects`
- No breaking changes — new endpoints only
