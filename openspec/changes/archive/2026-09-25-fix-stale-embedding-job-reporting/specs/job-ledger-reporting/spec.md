## ADDED Requirements

### Requirement: Separate stale-sweep failures from genuine failures

Job-queue reporting SHALL distinguish jobs that are genuinely failing from jobs
that were terminal-failed in bulk by the stale-job sweep. A job is a stale-sweep
failure when its status is `failed` and its error text is exactly the canonical
stale-sweep marker; every other `failed` job is a genuine failure. Every consumer
that renders or aggregates per-queue job status SHALL count only genuine failures
in its `failed` figure and SHALL report stale-sweep failures in a separate
counter (or a separate, clearly-labelled value). This split SHALL apply to every
job table the sweep touches: `kb.graph_embedding_jobs`,
`kb.chunk_embedding_jobs`, `kb.document_parsing_jobs`,
`kb.object_extraction_jobs`, and `kb.email_jobs`.

An admin/ledger view that deliberately lists rows carrying error text (for
example a `withErrors` count) MAY continue to include stale-sweep rows, provided
the reason is documented in code, because its meaning is "rows carrying error
text", not "current failures".

#### Scenario: Stale-sweep failures excluded from failed

- **WHEN** a queue contains jobs with `status = 'failed'` and error text equal to the stale-sweep marker
- **THEN** those jobs SHALL NOT be included in the queue's `failed` count
- **AND** they SHALL be included in the queue's stale-failure count

#### Scenario: Genuine failures still counted

- **WHEN** a queue contains jobs with `status = 'failed'` and any other error text
- **THEN** those jobs SHALL be included in the queue's `failed` count
- **AND** SHALL NOT be included in the stale-failure count

#### Scenario: A failed job with no error text is genuine

- **WHEN** a job has `status = 'failed'` and no error text
- **THEN** it SHALL be counted as a genuine failure

#### Scenario: Consistent across all swept tables

- **WHEN** stale-sweep failures exist in any of the five swept job tables
- **THEN** the aggregate for that table SHALL exclude them from `failed` and report them separately

#### Scenario: Every job-status consumer honours the split

- **WHEN** any consumer of per-queue job status is read — the `/api/metrics/jobs`, `/api/embeddings/progress`, and `/api/projects/:id/embeddings/progress` endpoints, the `pkg/sdk/health` and `pkg/sdk/superadmin` clients, or the CLI displays `memory embeddings progress` and `memory auth status`
- **THEN** it SHALL report genuine failures and stale-sweep failures as distinct values
- **AND** it SHALL NOT present stale-sweep rows as current failures

#### Scenario: CLI totals reconcile with the API

- **WHEN** `memory embeddings progress` prints a queue whose only non-completed rows are stale-sweep failures
- **THEN** the printed stale-failed value SHALL be non-zero AND SHALL be included in the total used for the completion percentage (so the queue does not read as 100% complete)

#### Scenario: Server service and admin aggregates honour the split

- **WHEN** the `email`, `document_parsing_jobs`, or `object_extraction_jobs` service statistics, or the `superadmin` embedding/extraction/document-parsing statistics, are read
- **THEN** stale-sweep rows SHALL be excluded from `failed` and reported in a separate stale-failed value
- **AND** any `withErrors`-style count that intentionally lists rows carrying error text SHALL remain unchanged and its intent documented in code

### Requirement: The stale-sweep marker has a single source of truth

The sweep that stamps the marker and the reporting that matches it SHALL read the
same canonical constant, so the two can never drift.

#### Scenario: Sweep and reporting share the constant

- **WHEN** the stale-job sweep terminal-fails a job
- **THEN** it writes the canonical marker constant
- **AND** the reporting queries match rows against that same constant

### Requirement: Additive, backwards-compatible response shape

The new stale-failure counter SHALL be added without removing or renaming existing
fields, so existing consumers continue to parse the response.

#### Scenario: Existing fields preserved

- **WHEN** a client that does not know about the stale-failure counter calls a job-statistics endpoint
- **THEN** it SHALL still receive `pending`, `processing`, `completed`, `failed`, and dead-letter counts as before

### Requirement: Retention reclaims terminal rows across all swept tables

The terminal-job retention purge SHALL delete terminal rows older than the
retention window from every job table the sweep touches, using each table's
correct terminal statuses and age column. `kb.email_jobs`, which has no
`updated_at` column and whose terminal success status is `sent`, SHALL use
`COALESCE(processed_at, created_at)` as its age and treat `sent`, `failed`, and
`dead_letter` as terminal.

#### Scenario: Terminal rows reclaimed in every swept table

- **WHEN** the retention purge runs and a swept table holds terminal rows older than the retention window
- **THEN** those rows SHALL be deleted from that table

#### Scenario: Email jobs use the right age and statuses

- **WHEN** the retention purge processes `kb.email_jobs`
- **THEN** it SHALL treat `sent`, `failed`, and `dead_letter` as terminal
- **AND** it SHALL measure age by `COALESCE(processed_at, created_at)`
- **AND** it SHALL NOT reference an `updated_at` column

#### Scenario: Non-terminal rows are never purged

- **WHEN** the retention purge runs
- **THEN** it SHALL NOT delete rows whose status is `pending`, `processing`, or `running`
