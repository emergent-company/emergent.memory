# Bound graph bulk endpoints with a pool-aware worker pool

## Why

Issue #1573: bulk property migration is I/O-bound and does not scale. A
100-item `bulk-update` batch took up to 10 minutes on dev, and 16 concurrent
single-item PATCHes gave the same throughput as a serial run. The reported
`BulkUpdateObjects` / `BulkUpsertObjects` / `BulkCreateObjects` loops are in fact
fully serial — each item pays a full round trip for identity resolution, a
version `INSERT`, a `supersedes_id` `UPDATE` and an embedding-carry `UPDATE`,
and the next item cannot start until the previous transaction commits.

## What Changes

- The bulk object create/upsert/update service loops are replaced by a shared
  bounded worker pool (`runBulkItems`). Independent items now overlap, cutting
  wall time on the I/O-bound per-item version-write path.
- Pool size is configurable via `GRAPH_BULK_CONCURRENCY` (default `8`) and is
  clamped to `[1, DB_MAX_OPEN_CONNS-1]` (never below `1`), reserving one pooled
  connection for other traffic so a bulk request can never claim the whole pool
  (#1482).
- Semantics are unchanged: each request index is visited exactly once so
  `results[i]` stays aligned with `items[i]`, per-item error isolation is
  preserved, and items targeting the same object or `(type, key)` still
  serialize on the existing advisory lock — one HEAD per object and the
  `supersedes_id` chain are maintained, and `ContentHash` / embedding
  carry/enqueue behaviour (#1478/#1488/#1509) is untouched.

## Impact

- Config surface: one new variable, `GRAPH_BULK_CONCURRENCY`.
- No API request/response shape, schema, or migration change. The existing
  10-minute bulk request budget and transaction-local `statement_timeout`
  (#1558) are unchanged.
