## Why

`kb.project_journal` is the append-only audit log of every knowledge-graph mutation. It is an operational and compliance asset that must be retained indefinitely. Two implementation properties make it a growing performance liability:

- **Unbounded write fan-out.** `domain/journal.Service.Log` spawns one goroutine and one DB insert per mutation, detached from the caller. Bulk endpoints process items with a bounded worker pool (`GRAPH_BULK_CONCURRENCY`, default 8) but every per-item mutation spawns its own journal goroutine, each grabbing a connection from the shared pool (`DB_MAX_OPEN_CONNS`, default 100). A large bulk import can therefore saturate the pool and starve the mutation workers it depends on.
- **Unbounded read cost + write amplification.** `List` uses OFFSET pagination plus `ScanAndCount`, issuing an exact `Total` count over a per-project slice that grows forever. The table also carries `idx_project_journal_branch`, which serves ~0.03% of rows (241,607 of 241,673 have `branch_id NULL`) yet is maintained on every insert. Five bulk endpoints additionally write a redundant `EventTypeBatch` summary row on top of their per-item rows (N+1).

Measured on dev: 241,673 rows / 112 MB (heap 66 MB, indexes 46 MB) in ~1 month (~8k rows/day, ~1.4 GB/yr).

## What Changes

- Replace the per-mutation goroutine in `journal.Service.Log` with a bounded in-process writer: a buffered queue, a fixed worker pool, and multi-row batch flushes. Backpressure (blocking submit) replaces silent drops; the writer drains on shutdown.
- Remove the five redundant `EventTypeBatch` summary writes from list-style bulk endpoints while keeping per-item audit rows. Keep the summaries for `BulkAction`, `MergeBranch`, `ForkBranch`, and `MoveObject`, which have no per-item rows.
- Drop `idx_project_journal_branch`.
- Replace offset + exact `Total` in the journal list with keyset (cursor) pagination exposing `next_cursor` / `has_more`.
- Record the retention decisions as resolved: **no prune, no time partitioning, no `project_id` foreign key** — the journal is retained forever and must survive project lifecycle.

## Capabilities

### New Capabilities
- `graph-journal`: the project audit journal — bounded asynchronous mutation logging, per-mutation audit granularity, and cursor-paginated reads. Retained indefinitely.

### Modified Capabilities
<!-- none -->

## Impact

- **Server**: `domain/journal/{service,store,entity,handler,module}.go`, new `domain/journal/cursor.go`, `domain/graph/service.go` (remove 5 summary writes), `internal/config/config.go` (new `Journal` config group).
- **CLI**: `apps/cli/internal/cmd/journal.go` (response shape).
- **MCP**: `domain/mcp/service.go` `executeJournalList` (response shape).
- **DB**: new migration `00227_drop_journal_branch_index.sql` (drops the branch index; reversible).
- **API**: `GET /api/graph/journal` response replaces `total` with `next_cursor` / `has_more` and accepts a `cursor` query param. This is a breaking response-shape change for that endpoint.
- **Not changed**: backup export/restore (same tables/rows), the `project_journal_notes` schema and its `ON DELETE CASCADE`, and the `GraphEventSink` interface.
- **Out of scope / deferred**: retention or archival, time partitioning, transactional outbox (lossless audit), and a `project_id` foreign key. See design for rationale.
