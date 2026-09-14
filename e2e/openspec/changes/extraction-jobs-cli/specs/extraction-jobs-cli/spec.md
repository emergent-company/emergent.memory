## ADDED Requirements

### Requirement: Create extraction job via CLI
The CLI SHALL provide `memory extraction jobs create --project <id> --document <id>` to POST a new extraction job to `/api/admin/extraction-jobs`.

#### Scenario: Successful job creation (table output)
- **WHEN** user runs `memory extraction jobs create --project <proj> --document <docID>`
- **THEN** the CLI prints a table row showing job ID, status `queued`, and source document ID

#### Scenario: Successful job creation (JSON output)
- **WHEN** user runs `memory extraction jobs create --project <proj> --document <docID> --output json`
- **THEN** the CLI prints the raw job JSON from the server including `id`, `status`, `project_id`, `source_id`

#### Scenario: Missing project flag
- **WHEN** user runs `memory extraction jobs create --document <docID>` without `--project`
- **THEN** the CLI exits non-zero with an error message indicating `--project` is required

#### Scenario: Missing document flag
- **WHEN** user runs `memory extraction jobs create --project <proj>` without `--document`
- **THEN** the CLI exits non-zero with an error message indicating `--document` is required

---

### Requirement: Get extraction job by ID via CLI
The CLI SHALL provide `memory extraction jobs get <job-id>` to GET a single job from `/api/admin/extraction-jobs/:jobId`.

#### Scenario: Successful get (table output)
- **WHEN** user runs `memory extraction jobs get <jobID>`
- **THEN** the CLI prints a table showing job ID, status, source ID, entity count, relationship count, started_at, completed_at

#### Scenario: Successful get (JSON output)
- **WHEN** user runs `memory extraction jobs get <jobID> --output json`
- **THEN** the CLI prints the full job JSON including `debug_info`

#### Scenario: Non-existent job ID
- **WHEN** user runs `memory extraction jobs get <unknown-id>`
- **THEN** the CLI exits non-zero with a not-found error

---

### Requirement: List extraction jobs for a project via CLI
The CLI SHALL provide `memory extraction jobs list --project <id>` to list jobs from `/api/admin/extraction-jobs/projects/:projectId`.

#### Scenario: List all jobs (table output)
- **WHEN** user runs `memory extraction jobs list --project <proj>`
- **THEN** the CLI prints a table with columns: ID, Status, Source ID, Duration, Entities, Relationships, Created At

#### Scenario: Filter by status
- **WHEN** user runs `memory extraction jobs list --project <proj> --status completed`
- **THEN** only jobs with status `completed` are returned

#### Scenario: Filter by document (source_id)
- **WHEN** user runs `memory extraction jobs list --project <proj> --document <docID>`
- **THEN** only jobs for that document are returned

#### Scenario: JSON output
- **WHEN** user runs `memory extraction jobs list --project <proj> --output json`
- **THEN** the CLI prints a JSON array of job objects

---

### Requirement: Cancel an extraction job via CLI
The CLI SHALL provide `memory extraction jobs cancel <job-id>` to POST to `/api/admin/extraction-jobs/:jobId/cancel`.

#### Scenario: Cancel a queued job
- **WHEN** user runs `memory extraction jobs cancel <jobID>` for a queued job
- **THEN** the CLI prints the updated job with status `cancelled`

#### Scenario: Cancel a completed job (error case)
- **WHEN** user runs `memory extraction jobs cancel <jobID>` for an already-completed job
- **THEN** the CLI exits non-zero with an error from the server

---

### Requirement: Retry a failed extraction job via CLI
The CLI SHALL provide `memory extraction jobs retry <job-id>` to POST to `/api/admin/extraction-jobs/:jobId/retry`.

#### Scenario: Retry a failed job
- **WHEN** user runs `memory extraction jobs retry <jobID>` for a failed job
- **THEN** the CLI prints the new job with status `queued`

---

### Requirement: Get extraction job logs via CLI
The CLI SHALL provide `memory extraction jobs logs <job-id>` to GET `/api/admin/extraction-jobs/:jobId/logs`.

#### Scenario: Logs for completed job
- **WHEN** user runs `memory extraction jobs logs <jobID>` for a completed job
- **THEN** the CLI prints a summary (entity count, relationship count, orphan rate) and a list of log entries

#### Scenario: JSON output
- **WHEN** user runs `memory extraction jobs logs <jobID> --output json`
- **THEN** the CLI prints the raw logs response JSON

---

### Requirement: Get extraction statistics for a project via CLI
The CLI SHALL provide `memory extraction jobs stats --project <id>` to GET `/api/admin/extraction-jobs/projects/:projectId/statistics`.

#### Scenario: Stats output (table)
- **WHEN** user runs `memory extraction jobs stats --project <proj>`
- **THEN** the CLI prints total jobs, jobs by status (queued/running/completed/failed/cancelled), success rate, and jobs this week/month

#### Scenario: Stats output (JSON)
- **WHEN** user runs `memory extraction jobs stats --project <proj> --output json`
- **THEN** the CLI prints the raw statistics JSON
