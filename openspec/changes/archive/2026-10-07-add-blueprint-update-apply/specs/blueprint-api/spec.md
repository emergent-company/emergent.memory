## ADDED Requirements

### Requirement: User-triggered asynchronous apply

The API SHALL let a caller enqueue the application of a detected update as a
durable background job rather than performing the fetch and apply inline. A
request to `POST /api/blueprints/:name/updates/apply` SHALL create an `update`
job for the caller's project and return without waiting for it to finish.

#### Scenario: Enqueue an update

- **WHEN** a caller POSTs to `/api/blueprints/:name/updates/apply` for an
  applied, GitHub-imported blueprint
- **THEN** an `update` job is enqueued for that (project, blueprint) and the
  response reports the job as pending/running

#### Scenario: Duplicate enqueue is rejected

- **WHEN** an `update` job for the same (project, blueprint) is already
  pending or running
- **THEN** the request is rejected with `409 Conflict` and no second job is
  created

### Requirement: Update execution is idempotent and no-ops when current

Applying an update SHALL re-fetch the upstream manifest and, when the upstream
version and content are unchanged from the applied blueprint, complete
successfully as a no-op without creating a duplicate version. Otherwise it SHALL
create a new version carrying the blueprint's recorded source provenance,
publish it, and apply it to the project. Execution SHALL be idempotent under
retry.

#### Scenario: Upstream unchanged

- **WHEN** the update job runs and upstream matches the applied version/content
- **THEN** the job succeeds with a no-op result and no new version is created

#### Scenario: Newer upstream applied

- **WHEN** the update job runs and upstream is newer
- **THEN** a new blueprint version is created with the same source provenance,
  published, and applied to the project, and the job succeeds

#### Scenario: Retry after partial progress

- **WHEN** the job retries after the new version was already created (same
  name and version)
- **THEN** the existing version is reused rather than duplicated, and publish
  and apply continue

### Requirement: Update job status exposure

`GET /api/blueprints/:name/updates` SHALL return the latest `update`/`check` job
for the (project, blueprint) — its action, status
(`pending`/`running`/`succeeded`/`failed`), attempts, last error, timestamps, and
result — in addition to the current check record, so callers can follow progress
across reloads.

#### Scenario: Latest job returned

- **WHEN** a caller GETs `/api/blueprints/:name/updates` after an update was
  enqueued
- **THEN** the response includes the latest job with its status and timestamps

#### Scenario: Terminal outcome visible

- **WHEN** the update job has finished
- **THEN** the returned latest job status reflects `succeeded` or `failed` and
  carries the recorded result or error
