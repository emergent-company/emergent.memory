## ADDED Requirements

### Requirement: Bounded asynchronous mutation logging

The journal SHALL record graph mutation events without spawning an unbounded number of concurrent database writers. Writes SHALL flow through a bounded queue and a fixed-size worker pool, and SHALL be flushed in multi-row batches.

#### Scenario: Bulk mutation does not fan out unrestrictedly

- **WHEN** a bulk mutation processes many items concurrently
- **THEN** journal events are submitted to a bounded queue and written by at most the configured number of workers, never one unbounded goroutine-and-connection per item

#### Scenario: Backpressure instead of silent loss

- **WHEN** the journal queue is full
- **THEN** the submitting caller blocks until space is available, and an event is only abandoned (with a warning) when the caller's context is cancelled or the writer is stopped

#### Scenario: Drain on shutdown

- **WHEN** the service is stopped
- **THEN** the writer drains the remaining queued events within a bounded timeout before workers exit

#### Scenario: Per-mutation timestamp

- **WHEN** an event is submitted
- **THEN** its `created_at` is captured at submission time, not at flush time

### Requirement: Append-only journal retained indefinitely

The project journal SHALL be an append-only audit log that is retained indefinitely. The system SHALL NOT prune journal entries, partition the table for cheap deletion, or cascade-delete journal entries when a project is deleted.

#### Scenario: No retention deletion

- **WHEN** journal entries age beyond any time window
- **THEN** they remain queryable; no background job deletes them

#### Scenario: Project deletion preserves audit

- **WHEN** a project is soft-deleted
- **THEN** its journal entries remain present

### Requirement: Per-mutation audit granularity

Each mutated graph entity SHALL produce its own journal event. Bulk endpoints that emit one event per item SHALL NOT additionally write a redundant batch summary event for the same operation. Batch/operation-level summaries SHALL be retained only where no per-item events exist.

#### Scenario: List-style bulk emits per-item events only

- **WHEN** a bulk create/upsert/update of objects or relationships succeeds
- **THEN** each successful item is journaled individually and no additional batch summary event is written

#### Scenario: Operation-level actions keep a summary

- **WHEN** a set-level `BulkAction`, a branch merge, a branch fork, or an object move completes
- **THEN** a single summary event is journaled because no per-item events exist for that operation

### Requirement: Cursor-paginated journal listing

`GET /api/graph/journal` SHALL paginate by keyset cursor rather than offset, and SHALL NOT compute an exact total count. The response SHALL include `next_cursor` and `has_more`.

#### Scenario: First page

- **WHEN** the journal is listed without a cursor
- **THEN** the newest entries are returned ordered by `created_at DESC, id DESC`, with `has_more` set when more entries exist and `next_cursor` set when more entries exist

#### Scenario: Subsequent page

- **WHEN** the journal is listed with the `cursor` returned from the previous page
- **THEN** entries strictly older than the cursor are returned, with no duplicates and no gaps

#### Scenario: Invalid cursor

- **WHEN** the `cursor` parameter is malformed
- **THEN** the request fails with a 400 error

#### Scenario: Branch scoping preserved

- **WHEN** the listing is filtered by branch, or `include_branches=true` is set
- **THEN** the same main-branch / named-branch / merged-branches semantics apply as before, paginated by cursor
