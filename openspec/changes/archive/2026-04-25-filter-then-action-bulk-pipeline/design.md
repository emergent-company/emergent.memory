## Context

The graph API has `BulkUpdateStatus` (update status on a list of IDs) and `BulkUpdateObjects` (update up to 100 objects by ID). Neither accepts a filter — both require the caller to supply explicit object IDs. For large graphs this means paginate → collect IDs → batch update, which is slow and race-prone.

`PropertyFilter` already exists in `graph/dto.go:179` with operators: eq, neq, gt, gte, lt, lte, contains, exists, in. The filter-to-SQL translation is already implemented for list queries.

## Goals / Non-Goals

**Goals:**
- Single endpoint `POST /graph/objects/bulk-action` accepting filter + action
- Supported actions: `update_status`, `soft_delete`, `hard_delete`, `merge_properties`, `replace_properties`, `add_labels`, `remove_labels`, `set_labels`
- `dry_run` mode: count matches, no mutation
- Safety limit (default 1000, max 100000) with error if filter would exceed without explicit limit override
- Time-relative filter shorthand: `"90d"`, `"12h"`, `"6M"` evaluated server-side
- Audit log entry per bulk operation
- CLI commands

**Non-Goals:**
- Cursor-based pagination for >100k operations (v2 concern)
- Relationship bulk operations (separate endpoint if needed)
- Scheduled/recurring bulk operations (lifecycle engine, separate feature)

## Decisions

**1. Single endpoint with action discriminator, not 3 separate endpoints**

Issue #180 proposed 3 endpoints. One endpoint with `action` field is cleaner API surface, same expressiveness. The filter shape is identical across all actions.

**2. PostgreSQL `UPDATE ... WHERE` / `DELETE ... WHERE` — no object fetch**

Reuse existing PropertyFilter → SQL translation. Extend it to emit a WHERE clause usable in UPDATE/DELETE statements directly. No round-trip: objects are never loaded into Go memory. This is the performance win.

**3. Dry-run uses `SELECT COUNT(*)` with same WHERE clause**

Identical filter translation, just wrapped in COUNT. No mutation code path touched.

**4. Time-relative shorthand parsed at handler layer**

`"90d"` → `time.Now().Add(-90 * 24 * time.Hour)` before filter hits the DB layer. Keep DB layer unaware of relative time — it always receives absolute timestamps.

**5. Audit log via existing event/journal system**

One journal entry per bulk operation with: action, filter, matched count, affected count, actor, timestamp. Not per-object (too noisy).

## Risks / Trade-offs

- **[accidental mass delete]** → Default limit 1000 + dry_run mode + audit log. Require explicit `limit` override for operations >1000.
- **[long-running transactions]** → Very large UPDATEs can lock rows. Mitigate: hard cap at 100000; document that operations near cap should run during off-peak.
- **[filter translation parity]** → Bulk filter must use same translation as list filter to avoid surprises. Reuse same function, not a copy.
