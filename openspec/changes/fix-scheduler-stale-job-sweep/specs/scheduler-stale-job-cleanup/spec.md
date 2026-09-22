## ADDED Requirements

### Requirement: Stale sweep never terminal-fails never-started jobs

The stale-job cleanup sweep SHALL only terminal-fail jobs that have actually started. A job whose status is `pending` and that has never started SHALL NOT be marked `failed` by the sweep, for every swept job table (`kb.document_parsing_jobs`, `kb.chunk_embedding_jobs`, `kb.graph_embedding_jobs`, `kb.object_extraction_jobs`, `kb.email_jobs`). A never-started job is queued behind a backlog, not stale, and terminal-failing it drops work.

#### Scenario: Pending job aged past the stale window stays pending

- **WHEN** a job is `pending`, has never started (`started_at IS NULL`), and its `created_at` is older than the stale threshold
- **THEN** the sweep leaves its status untouched and does not stamp it with `Job marked as stale during cleanup`

#### Scenario: Email job pending past the stale window stays pending

- **WHEN** a `kb.email_jobs` row is `pending` and its `created_at` is older than the stale threshold
- **THEN** the sweep leaves its status untouched, because a pending email has never been attempted (`started_at IS NULL`), so it is queued, not stale

#### Scenario: Backlog larger than the stale window drains without mass failures

- **WHEN** a bulk enqueue produces more pending jobs than a worker can drain within the stale window
- **THEN** the sweep does not mark any of those queued jobs `failed`; they remain eligible for dequeue until a worker processes them

### Requirement: Stale sweep reaps genuinely stuck in-flight jobs

The stale-job cleanup sweep SHALL continue to terminal-fail jobs that started but have not finished. All swept tables key on their in-flight timestamp: a `processing` or `running` job whose `started_at` is older than the stale threshold SHALL be marked `failed`, with `completed_at` and `updated_at` set to `NOW()` and the table's error column set to `Job marked as stale during cleanup`. `kb.email_jobs` now stamps `started_at` at dequeue (migration 00173), so it uses the same rule as the other four tables. `created_at` remains only a defensive fallback for a swept table that has no `started_at` column (none currently).

#### Scenario: Worker died mid-job

- **WHEN** a job is `processing` and its `started_at` is older than the stale threshold
- **THEN** the sweep marks it `failed` and records `Job marked as stale during cleanup` in the table's error column

#### Scenario: Per-table stale threshold override

- **WHEN** a table such as `kb.document_parsing_jobs` has a longer configured stale threshold
- **THEN** the sweep uses that table's threshold instead of the global stale threshold

### Requirement: Mass reaps are visible

The stale-job cleanup sweep SHALL emit a visible alert when a single run terminal-fails more jobs in one table than the configured mass-reap threshold (`STALE_JOB_MASS_REAP_THRESHOLD`, default 1000). The sweep SHALL emit an `ERROR` log carrying the stable `alert=mass_stale_reap` marker, reporting the table and reap count, for log-based alerting, and SHALL increment a per-table in-process counter `scheduler_stale_jobs_reaped_total`. The counter is not scraped by any Prometheus endpoint, so visibility is via the log marker and the in-process counter, not dashboards or a scrape endpoint.

#### Scenario: Sweep reaps more than the threshold

- **WHEN** a single sweep terminal-fails more jobs in a table than the configured mass-reap threshold
- **THEN** the sweep logs an `ERROR` record with `alert=mass_stale_reap`, the table name, the reap count and the threshold, and increments `scheduler_stale_jobs_reaped_total` for that table

#### Scenario: Routine sweep below the threshold

- **WHEN** a sweep reaps a count at or below the mass-reap threshold
- **THEN** the sweep emits no mass-reap alert
