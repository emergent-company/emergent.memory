## Context

`kb.project_journal` records every knowledge-graph mutation (object/relationship create, update, delete, restore, relate, move, merge, fork, bulk). It is an append-only audit record that must be retained indefinitely; that constraint drives every decision below.

Current implementation facts:
- Writer: `domain/journal.Service.Log` spawns a detached goroutine (`context.WithoutCancel`) per mutation, one insert each; failures are logged as warnings only.
- Table: `id` PK (single column), `project_id` with no FK, `branch_id` (99.97% NULL on dev), `metadata` JSONB, `created_at`. Indexes: `(project_id, created_at DESC)`, `(project_id, branch_id, created_at DESC)`, `pkey(id)`.
- Reader: `domain/journal/store.go` `List` uses OFFSET + `ScanAndCount` (exact `Total`).
- Bulk endpoints call per-item `Create`/`CreateOrUpdate`/`Patch` and then write an extra `EventTypeBatch` summary.

## Goals / Non-Goals

**Goals**
- Bound journal write fan-out so a large bulk mutation cannot exhaust the DB pool or starve the mutation workers.
- Preserve per-mutation audit granularity (one event per mutated entity).
- Remove avoidable write amplification (redundant index, redundant batch rows) without losing audit content.
- Make journal reads scale under indefinite retention by removing the unbounded exact-count query.

**Non-Goals**
- Changing the journal's retained-forever guarantee.
- Retention, pruning, archive, or tiering.
- Time partitioning of the table.
- A foreign key from `project_id` to projects, or any cascading delete of audit rows.
- Making the async writer lossless (transactional outbox).

## Decisions

### D1. Bounded async writer with batch flush (chosen) over synchronous in-transaction insert or the current goroutine-per-call

- The current unbounded goroutine-per-call is the concrete risk: a 1000-item bulk can enqueue ~1000 concurrent inserts and saturate `DB_MAX_OPEN_CONNS`.
- A synchronous insert inside the mutation transaction would couple graph-write availability to journal availability (wrong direction for an audit log) and inject the journal repo into every write path.
- Chosen: an in-process bounded queue (default 4096), a fixed worker pool (default 8, `JOURNAL_WRITER_CONCURRENCY`), and multi-row batch flushes (`JOURNAL_WRITE_BATCH_SIZE`, default 256). `Log` captures `CreatedAt` at submit time, then submits with blocking backpressure; it warns only if the caller context is cancelled or the service is stopped — it does not silently drop on the normal path.
- Durability note: this does not worsen the existing crash-loss window (a mutation commits, then its event is flushed shortly after). A transactional outbox writing the journal row inside the mutation transaction is the gold standard for losslessness and is explicitly deferred.

### D2. Keep per-item audit rows; remove only the redundant bulk summaries

The five list-style bulk endpoints (`BulkCreateObjects`, `BulkUpsertObjects`, `BulkUpdateObjects`, `BulkCreateRelationships`, `BulkUpsertRelationships`) already emit one row per item; their trailing `EventTypeBatch` row duplicates information derivable from those rows, so it is removed. `BulkAction`, `MergeBranch`, `ForkBranch`, and `MoveObject` emit no per-item rows, so their single summary remains the only audit record and is retained. Per-item rows are never compressed into batch metadata (that would discard per-entity attribution).

### D3. Drop `idx_project_journal_branch`; keep the feed index non-covering

`idx_project_journal_branch` is maintained on every insert but serves almost nothing (99.97% NULL; the `branch_id IS NULL` post-filter on the feed is effectively free using `idx_project_journal_project_created`). It is dropped. The feed index stays non-covering: including JSONB `metadata` would bloat it for no benefit at `LIMIT`. The notes `(journal_id)` index is retained; a partial standalone-notes index is deferred until volume warrants it.

### D4. Keyset pagination replaces OFFSET + exact `Total`

Under indefinite retention an exact `Total` count grows without bound and dominates list cost. `List` orders by `(created_at DESC, id DESC)`, uses `(created_at, id) < (cursor)` with a `limit+1` probe to compute `has_more`, and returns `next_cursor`. `JournalResponse.total` is replaced by `next_cursor` / `has_more`. The cursor is `RFC3339Nano|uuid`, decoded defensively; malformed cursors yield 400.

### D5. No pruning, no partitioning, no `project_id` FK

- **Prune/retention (dropped):** incompatible with retained-forever. Archival/tiering is a future lever only, and must copy-then-delete in age batches rather than rely on partitioning.
- **Time partitioning (no-go):** PostgreSQL requires the partition key in every unique constraint, so `PK(id)` would become `PK(id, created_at)`. That invalidates `project_journal_notes.journal_id REFERENCES project_journal(id)`, forcing a composite FK plus a denormalized `journal_created_at` column on notes, and it would not help the leading-`project_id` feed query. The `notes.journal_id ... ON DELETE CASCADE` is the concrete blocker to revisit before any future retention/partition work.
- **`project_id` FK (dropped):** an FK would risk cascading audit deletion and would couple the backup exporter's project-filtered streaming to referential integrity. The journal must survive soft-deleted projects; orphaned `project_id` values act as tombstone references. No FK is added.

## Risks / Trade-offs

- **Backpressure latency:** a saturated queue makes `Log` block the caller. Mitigated by a generous buffer (4096) sized well above steady-state (~8k/day) and by batch flushes keeping drain rate high.
- **In-flight loss on hard crash/shutdown:** bounded by the drain-on-stop hook (5s) and unchanged from today's window; outbox deferred.
- **Breaking response shape:** `GET /api/graph/journal` drops `total` for `next_cursor`/`has_more`; CLI and MCP consumers are updated in the same change.
- **Ordering:** a multi-worker pool gives only approximate global ordering; `CreatedAt` is captured at submit time and `(created_at, id)` ties are broken deterministically. Exact total ordering is not required for an activity feed.

## Migration Plan

- One reversible Goose migration drops `idx_project_journal_branch` (Down recreates it).
- No data migration: no table/column/PK changes.
- Rollback: revert code + run the migration Down.

## Open Questions

None. Retention/partition/FK are resolved as recorded above.
