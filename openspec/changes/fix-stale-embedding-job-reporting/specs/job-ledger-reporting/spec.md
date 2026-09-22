## ADDED Requirements

### Requirement: Separate stale-sweep failures from genuine failures

Job-queue reporting SHALL distinguish jobs that are genuinely failing from jobs
that were terminal-failed in bulk by the stale-job sweep. A job is a stale-sweep
failure when its status is `failed` and its error text is exactly the canonical
stale-sweep marker; every other `failed` job is a genuine failure. Reporting
endpoints SHALL count only genuine failures in the `failed` figure and SHALL
report stale-sweep failures in a separate counter. This split SHALL apply to
every job table the sweep touches: `kb.graph_embedding_jobs`,
`kb.chunk_embedding_jobs`, `kb.document_parsing_jobs`,
`kb.object_extraction_jobs`, and `kb.email_jobs`.

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
