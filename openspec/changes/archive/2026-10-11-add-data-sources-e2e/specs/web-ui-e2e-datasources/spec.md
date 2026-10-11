## ADDED Requirements

### Requirement: Data-source UI flows have hermetic browser coverage

The web UI e2e suite SHALL provide a hermetic, credential-free Playwright configuration
that drives the Data Sources surface against a stateful mock memory upstream in dev mode,
and its specs SHALL be excluded from the live session-mode `chromium` project.

#### Scenario: Hermetic config starts the mock harness

- **WHEN** `npx playwright test --config=datasources.config.ts` runs
- **THEN** it starts `run-e2e.sh` (mock memory + gateway `AUTH_MODE=dev`) and targets `http://localhost:8097` without a Zitadel session or Drive credentials

#### Scenario: Live suite does not schedule the hermetic specs

- **WHEN** the live `chromium` project enumerates tests
- **THEN** `specs/datasources/**` is excluded via `testIgnore`

### Requirement: Connect flow is covered end to end

The suite SHALL cover the schema-driven connect form, the inline connection test (success
and failure), required-field rejection, and source creation landing on the detail page.

#### Scenario: Connect form renders from the provider schema

- **WHEN** the user opens `/data-sources/new`
- **THEN** the form exposes the config fields (`folder_id`, `recursive`, …) and write-only credential fields (`refresh_token`, `client_secret`)

#### Scenario: Connection test reports success and failure inline

- **WHEN** the user tests a config whose folder resolves (`folder-ok`) then one that does not (`folder-fail`)
- **THEN** the result region reports "Connected to Google Drive." then "Drive folder not found." respectively

#### Scenario: Creating a source lands on its detail page

- **WHEN** the user submits a valid connect form
- **THEN** the source is created and the browser lands on `/data-sources/{id}` showing the sync and connection-test controls

### Requirement: Sync lifecycle is covered end to end

The suite SHALL cover triggering a sync, observing job progress and phase, cancelling an
active job, a failed job's error surface, a completed job's counts, and the last-synced
list state.

#### Scenario: In-progress sync shows progress and can be cancelled

- **WHEN** the user triggers a sync for a `folder-slow` source
- **THEN** the job shows a processing status, phase, and progress counts, and cancelling it moves the job to a non-active terminal state with no cancel control

#### Scenario: Failed sync surfaces the error

- **WHEN** a `folder-fail` source is synced
- **THEN** the job shows a failed status and the error message

#### Scenario: Completed sync records counts and last-synced

- **WHEN** a source syncs successfully
- **THEN** the job shows completed counts and the source list shows a last-synced time

### Requirement: Document version history is covered end to end

The suite SHALL cover the document version-history sub-page for a multi-version document
and for a version in the `deleted_from_source` terminal state.

#### Scenario: Multi-version history renders oldest first

- **WHEN** the user opens a document with two active versions
- **THEN** both version rows render with their version number, revision id, and active status

#### Scenario: Deleted-from-source version is shown

- **WHEN** the user opens a document whose latest version was removed at the source
- **THEN** that version row renders the "Deleted from source" state and a deleted-at marker
