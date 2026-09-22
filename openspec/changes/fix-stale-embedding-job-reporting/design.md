## Context

The stale-job sweep (`StaleJobCleanupTask`) marks a job terminal-failed by writing
`status = 'failed'` and stamping the error column with
`"Job marked as stale during cleanup"`. Reporting counted `status = 'failed'`
directly, so the #705 mass-reap (97,129 rows, last updated 2026-09-20 14:41) is
reported as current breakage forever. Separately, the retention purge only reached
three tables and assumed an `updated_at` column.

## Decision: separate counter, not endpoint versioning

**Chosen:** add an additive `staleFailed` (`stale_failed` in `/api/metrics/jobs`)
counter and narrow `failed` to genuine failures.

Rationale:

- **Nothing is hidden.** Stale-sweep rows are still visible in a dedicated
  counter; they are not silently dropped.
- **`failed` becomes truthful.** Its historical meaning was "needs attention".
  Rows reaped by a sweep bug never needed attention; counting them is what makes
  a fully-embedded project (107,233/107,233 objects) look broken.
- **Least surprising / backwards compatible.** Existing field names are
  preserved; the new field is additive. The only known consumer, the web-ui
  gateway, is updated in the same PR. Endpoint versioning (`/api/v2/...`) was
  rejected as disproportionate for an additive field.
- **Single definition.** A row is a stale-sweep failure iff
  `status = 'failed' AND <errorColumn> = jobs.StaleJobMessage`. Genuine failure is
  the complement (`COALESCE(<errorColumn>, '') <> marker`), so a failed row with a
  NULL error still counts as genuine.

Per-table error columns (matching the sweep): `last_error` for
`graph_embedding_jobs`, `graph_relationship_embedding_jobs`,
`chunk_embedding_jobs`, `email_jobs`; `error_message` for
`document_parsing_jobs`, `object_extraction_jobs`.

## Decision: widen retention, do not ship a data-deleting migration

The purge already deleted `status IN ('completed','failed','dead_letter')` for the
three embedding tables, i.e. `status='failed'` was already covered — the gap was
the other three tables and the email age column. The fix is code-only (per-table
config) plus a documented one-off reclaim for the rows already present.

A data-deleting migration is rejected: migrations in this repo are schema
migrations, and an irreversible bulk delete in a migration would run unattended on
every environment including production. A widened purge (self-healing every
retention window) plus a reviewed one-off is safer and idempotent.

## One-off reclaim (DOCUMENTED ONLY — NOT EXECUTED, NOT A MIGRATION)

Delete only the stale-sweep rows older than the retention window, across the six
tables, idempotently (safe to re-run; each run deletes only what still matches):

```sql
-- 0) BEFORE: size + counts
SELECT 'before' AS phase,
       pg_size_pretty(pg_total_relation_size('kb.graph_embedding_jobs')) AS graph_embedding_jobs,
       pg_size_pretty(pg_total_relation_size('kb.chunk_embedding_jobs')) AS chunk_embedding_jobs,
       pg_size_pretty(pg_total_relation_size('kb.graph_relationship_embedding_jobs')) AS rel_embedding_jobs,
       pg_size_pretty(pg_total_relation_size('kb.document_parsing_jobs')) AS document_parsing_jobs,
       pg_size_pretty(pg_total_relation_size('kb.object_extraction_jobs')) AS object_extraction_jobs,
       pg_size_pretty(pg_total_relation_size('kb.email_jobs')) AS email_jobs;

SELECT 'kb.graph_embedding_jobs' AS tbl, count(*) FROM kb.graph_embedding_jobs
  WHERE status='failed' AND last_error='Job marked as stale during cleanup'
UNION ALL SELECT 'kb.chunk_embedding_jobs', count(*) FROM kb.chunk_embedding_jobs
  WHERE status='failed' AND last_error='Job marked as stale during cleanup'
UNION ALL SELECT 'kb.graph_relationship_embedding_jobs', count(*) FROM kb.graph_relationship_embedding_jobs
  WHERE status='failed' AND last_error='Job marked as stale during cleanup'
UNION ALL SELECT 'kb.document_parsing_jobs', count(*) FROM kb.document_parsing_jobs
  WHERE status='failed' AND error_message='Job marked as stale during cleanup'
UNION ALL SELECT 'kb.object_extraction_jobs', count(*) FROM kb.object_extraction_jobs
  WHERE status='failed' AND error_message='Job marked as stale during cleanup'
UNION ALL SELECT 'kb.email_jobs', count(*) FROM kb.email_jobs
  WHERE status='failed' AND last_error='Job marked as stale during cleanup';

-- 1) Reclaim (idempotent; run in ONE transaction per table, or table-by-table)
BEGIN;
DELETE FROM kb.graph_embedding_jobs
 WHERE status='failed' AND last_error='Job marked as stale during cleanup';
DELETE FROM kb.chunk_embedding_jobs
 WHERE status='failed' AND last_error='Job marked as stale during cleanup';
DELETE FROM kb.graph_relationship_embedding_jobs
 WHERE status='failed' AND last_error='Job marked as stale during cleanup';
DELETE FROM kb.document_parsing_jobs
 WHERE status='failed' AND error_message='Job marked as stale during cleanup';
DELETE FROM kb.object_extraction_jobs
 WHERE status='failed' AND error_message='Job marked as stale during cleanup';
DELETE FROM kb.email_jobs
 WHERE status='failed' AND last_error='Job marked as stale during cleanup';
COMMIT;

-- 2) AFTER: re-run step 0 and compare sizes.
VACUUM (ANALYZE) kb.graph_embedding_jobs, kb.chunk_embedding_jobs,
  kb.graph_relationship_embedding_jobs, kb.document_parsing_jobs,
  kb.object_extraction_jobs, kb.email_jobs;
```

Notes:

- The predicate matches on the marker (not merely age/status) so it cannot delete
  genuine failures and is safe to re-run.
- **Expected runtime/impact** at ~97k rows: the deletes are index-assisted on
  `status`-filtered scans; expect seconds, not minutes. Row locks are held only for
  the deleted rows, but a single long transaction is visible to concurrent
  writers. Prefer the table-by-table form in production; the graph table is the
  bulk (~97k).
- **Bloat:** a plain `DELETE` frees space for reuse but does not return it to the
  OS and leaves dead tuples until vacuumed. That is acceptable at this size — no
  `REINDEX` is required, and a full `VACUUM FULL`/`REINDEX` (which takes table
  `ACCESS EXCLUSIVE` and rewrites the table) is unnecessary and riskier than the
  problem warrants. Running `VACUUM (ANALYZE)` afterwards reclaims reusable space
  and refreshes planner stats. If a future bulk delete is much larger, revisit
  `REINDEX CONCURRENTLY` for the status indexes then.
