## ADDED Requirements

### Requirement: Google Drive provider is listed and configurable

The server SHALL advertise `google_drive` as an available data-source provider with a config
schema, and SHALL accept a source `config` containing a required `folder_id`, an optional
`shared_drive_id`, a `mime_types` allowlist, and a `recursive` flag, alongside an encrypted
credential reference.

#### Scenario: Provider catalog includes Google Drive

- **WHEN** a client lists data-source providers
- **THEN** `google_drive` is present and marked available with its config schema

#### Scenario: Config schema declares the required folder

- **WHEN** a client fetches the `google_drive` provider schema
- **THEN** the schema requires `folder_id` and describes the optional `shared_drive_id`, `mime_types`, and `recursive` fields

### Requirement: Source credentials are stored encrypted, never in config

A Drive source SHALL store its OAuth2 credentials (client id, client secret, refresh token)
encrypted at rest, and credentials SHALL NOT be returned in any source read response or stored in
the plaintext `config`.

#### Scenario: Credentials are write-only

- **WHEN** a source is created and then read back
- **THEN** the response contains no client secret or refresh token

#### Scenario: Connection test uses stored credentials

- **WHEN** a client tests an existing Drive source's connection
- **THEN** the server decrypts the stored credentials and performs a bounded Drive API call, returning success or a failure message

### Requirement: Drive sources are created, read, updated, and deleted

A Drive source SHALL be a durable, project-scoped record, and a user SHALL be able to create,
read, update, and delete it. Cross-project identifiers SHALL resolve to not-found.

#### Scenario: Create and list a Drive source

- **WHEN** a user creates a Drive source with a folder and credentials
- **THEN** it is persisted project-scoped and appears in that project's source listing

#### Scenario: Cross-project access is not-found

- **WHEN** a user requests a source id belonging to another project
- **THEN** the server returns not-found and reveals no data

#### Scenario: Delete a source

- **WHEN** a source is deleted
- **THEN** its sync jobs are removed, its documents are retained with `source_id` set to NULL, and it no longer appears in listings

### Requirement: A sync enumerates the configured folder and MIME scope

A Drive sync SHALL enumerate the configured folder subtree (`recursive` when set), filtered to the
configured MIME-type allowlist and excluding trashed items, paginating through all results. It
SHALL use each item's provider revision id as a cheap pre-filter and SHALL fetch only items whose
revision id differs from the stored one.

#### Scenario: Only in-scope items are considered

- **WHEN** a sync runs for a source scoped to a folder and MIME allowlist
- **THEN** only items within that folder subtree and matching the allowlist are ingested, and items outside the scope are ignored

#### Scenario: Unchanged revisions are skipped without fetching

- **WHEN** an in-scope item's provider revision id equals the stored revision id
- **THEN** the sync does not download its content

### Requirement: Drive content is fetched and converted for the pipeline

Google-native documents (Docs, Sheets, Slides) SHALL be fetched via export and other supported
files via media download, with bounded size and request-time limits, then parsed into text through
the existing pipeline. A fetch failure for one item SHALL NOT abort the whole sync.

#### Scenario: Google-native document is exported and ingested

- **WHEN** an in-scope Google Docs file changes
- **THEN** the sync exports its content, parses it to text, and ingests it as a document version

#### Scenario: Binary file is downloaded and parsed

- **WHEN** an in-scope PDF or office file changes
- **THEN** the sync downloads its bytes, parses them through the pipeline, and ingests the resulting text

#### Scenario: One item failing does not abort the sync

- **WHEN** fetching or parsing one item fails
- **THEN** the sync records a per-item error, continues with the remaining items, and completes having processed them

### Requirement: Sync is idempotent and version-aware

Re-running a sync over already-ingested content SHALL NOT create duplicate documents, chunks, or
graph objects. A changed item SHALL version per the `document-versioning` and
`document-ingestion` capabilities (new version only when parsed text changes).

#### Scenario: Re-sync with no changes is a no-op

- **WHEN** a source is synced twice and nothing changed at the provider
- **THEN** no new documents, versions, chunks, or graph objects are created

#### Scenario: Repeated sync uses the persisted source state

- **WHEN** a subsequent sync runs
- **THEN** it uses the source's persisted per-item revision state rather than re-downloading unchanged content

### Requirement: In-scope removals are marked deleted-from-source

When an item that is currently ingested and within scope is reported removed or trashed, the sync
SHALL mark its HEAD document `deleted_from_source` per the `document-versioning` capability rather
than deleting it.

#### Scenario: Removed item is marked, not deleted

- **WHEN** an ingested in-scope item is removed or trashed at the provider
- **THEN** the sync marks its document `deleted_from_source` and retains its versions and chunks

### Requirement: Sync jobs have a monitored lifecycle with progress

A sync SHALL be a job with lifecycle `pending`, `processing`, `completed`, `failed`, and
`cancelled`. At most one sync job SHALL be `pending` or `processing` per source, enforced by a
partial unique constraint; a trigger while one is active SHALL be rejected or return the active
job. A running job SHALL be cancellable, each transition recorded, and progress (phase, status
message, trigger type, and total/processed/successful/failed/skipped item counts) SHALL be
persisted so it is accurate after a restart.

#### Scenario: Job transitions through the lifecycle

- **WHEN** a sync is triggered
- **THEN** it enters `pending`, then `processing`, then a terminal state, with each transition recorded

#### Scenario: Overlapping trigger is rejected

- **WHEN** a sync job is `pending` or `processing` for a source and a second trigger arrives
- **THEN** the second trigger is rejected or returns the existing job, and no second active job exists

#### Scenario: Cancel stops a running sync

- **WHEN** a running sync is cancelled
- **THEN** it transitions to `cancelled` and stops fetching further items

#### Scenario: Progress survives a restart

- **WHEN** a sync job is interrupted and later read
- **THEN** its reported phase, message, and item counts reflect persisted progress, not zero

#### Scenario: Sync job responses carry the SDK fields

- **WHEN** a sync job is fetched
- **THEN** the response includes `status`, `totalItems`, `processedItems`, `successfulItems`, `failedItems`, `skippedItems`, `currentPhase`, `statusMessage`, `errorMessage`, `triggerType`, `retryCount`, `maxRetries`, `startedAt`, and `completedAt`

### Requirement: Syncs can be scheduled or triggered manually

A source SHALL support a manual sync trigger and, when configured with a sync interval, SHALL be
triggered on that cadence by the scheduler. Both paths SHALL admit through the same single-active-job
rule.

#### Scenario: Manual trigger starts a sync

- **WHEN** a client triggers a sync for a source
- **THEN** a sync job is admitted and its progress is observable

#### Scenario: Scheduled trigger starts a sync

- **WHEN** a source's configured sync interval elapses
- **THEN** the scheduler admits a sync through the same lifecycle and admission rules

### Requirement: The server exposes the SDK data-source REST surface

The server SHALL expose the `google_drive` subset of `/api/data-source-integrations/*` matching
`apps/server/pkg/sdk/datasources/client.go`: provider catalog and schema, source types,
connection testing (saved and unsaved config), source CRUD, sync trigger, and sync-job
list/latest/detail/cancel. All routes SHALL require authentication and project scope.

#### Scenario: SDK client can drive the lifecycle

- **WHEN** the SDK client lists providers, creates a source, tests it, triggers a sync, and polls its job
- **THEN** each call maps to a documented server route and succeeds under authentication and project scope

#### Scenario: Unauthenticated access is rejected

- **WHEN** a caller invokes a data-source route without valid credentials
- **THEN** the server returns an unauthorized response
