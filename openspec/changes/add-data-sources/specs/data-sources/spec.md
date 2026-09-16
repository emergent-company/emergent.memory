## Purpose

Lets a signed-in user connect, configure, test, and sync external data sources (email, drive, ClickUp) into the Emergent Memory graph, and discover object types from synced documents.

## ADDED Requirements

### Requirement: List data sources

The gateway SHALL list the project's data-source integrations, each with its provider, source type, sync mode, status, and last-sync time.

#### Scenario: Data sources listed

- **WHEN** a signed-in user opens the data-sources view
- **THEN** the gateway shows the project's integrations with provider, source type, sync mode, status, and last-sync time

#### Scenario: No data sources

- **WHEN** a signed-in user has no data sources in the active project
- **THEN** the gateway shows an empty state prompting the user to connect a source

### Requirement: List providers and their configuration schema

The gateway SHALL list the available providers and, for each provider, retrieve its configuration schema so the connect form can be rendered dynamically.

#### Scenario: Providers listed

- **WHEN** a signed-in user opens the connect-data-source flow
- **THEN** the gateway lists the available providers with their source type and description

#### Scenario: Provider schema retrieved

- **WHEN** a signed-in user selects a provider
- **THEN** the gateway fetches that provider's configuration schema and renders fields for its required and optional properties

### Requirement: Create a data source

The gateway SHALL let a user create a data-source integration by supplying a name, provider, source type, configuration, and sync settings.

#### Scenario: Create with recurring sync

- **WHEN** a signed-in user submits a valid data-source form with a recurring sync mode and an interval
- **THEN** the gateway creates the integration in Memory and shows it in the list

#### Scenario: Create rejected

- **WHEN** a signed-in user submits an invalid or incomplete data-source form
- **THEN** the gateway surfaces the validation error and does not create the integration

### Requirement: Test a connection before saving

The gateway SHALL let a user test a provider configuration, and an existing integration's connection, without mutating the integration.

#### Scenario: Test a new configuration

- **WHEN** a signed-in user requests a connection test on an unsaved provider configuration
- **THEN** the gateway reports whether the connection succeeded and the provider's message

#### Scenario: Test an existing integration

- **WHEN** a signed-in user tests the connection of an existing integration
- **THEN** the gateway reports whether the integration's stored credentials still connect

### Requirement: Update a data source

The gateway SHALL let a user update an integration's name, configuration, sync mode, interval, and enabled state.

#### Scenario: Update sync settings

- **WHEN** a signed-in user edits an integration's sync mode or interval
- **THEN** the gateway persists the change and shows the updated values

#### Scenario: Disable a data source

- **WHEN** a signed-in user disables an integration
- **THEN** the gateway marks the integration disabled and it no longer auto-syncs

### Requirement: Delete a data source

The gateway SHALL let a user delete an integration, while the documents already imported from it remain in the project.

#### Scenario: Delete integration

- **WHEN** a signed-in user deletes a data source
- **THEN** the gateway removes the integration and leaves its previously imported documents intact

### Requirement: Trigger a sync

The gateway SHALL let a user manually trigger a sync for an integration.

#### Scenario: Manual sync started

- **WHEN** a signed-in user triggers a sync on an integration
- **THEN** the gateway starts a sync job and reports the job identifier when one is returned

### Requirement: Monitor sync jobs

The gateway SHALL expose an integration's sync jobs, including the most recent job, and each job's progress fields (status, item counts, phase, and status message).

#### Scenario: Sync jobs listed

- **WHEN** a signed-in user opens an integration's sync history
- **THEN** the gateway lists the integration's sync jobs with status and item counts

#### Scenario: Latest job with no history

- **WHEN** an integration has never been synced
- **THEN** the gateway reports that no sync job exists rather than an error

#### Scenario: In-progress job

- **WHEN** a sync job is running
- **THEN** the gateway shows the job's current phase, processed/total items, and status message

### Requirement: Cancel a sync job

The gateway SHALL let a user cancel a running or pending sync job.

#### Scenario: Cancel job

- **WHEN** a signed-in user cancels a running sync job
- **THEN** the gateway cancels the job and reflects its cancelled status

### Requirement: Schema discovery

The gateway SHALL let a user start, monitor, and finalize a discovery job that infers object and relationship types from synced documents.

#### Scenario: Start discovery

- **WHEN** a signed-in user starts discovery on selected documents
- **THEN** the gateway starts a discovery job and returns its job identifier

#### Scenario: Monitor discovery

- **WHEN** a discovery job is running
- **THEN** the gateway shows the job's status and discovered types and relationships

#### Scenario: Finalize discovery

- **WHEN** a signed-in user finalizes a completed discovery job with selected types
- **THEN** the gateway applies the discovered types to the type registry as a template pack

### Requirement: Project-scoped and authenticated access

All data-source operations SHALL be scoped to the signed-in session's active project and MUST NOT operate on another project's integrations.

#### Scenario: Unauthenticated request

- **WHEN** a caller invokes a data-source operation without a valid session or API key
- **THEN** the gateway returns an unauthorized response and no data-source data

### Requirement: Provider secrets never exposed

The gateway MUST NOT persist or echo provider credentials, and SHALL rely on Memory's server-side encryption for stored configurations.

#### Scenario: Secrets not returned

- **WHEN** a signed-in user views or edits a data source
- **THEN** the gateway does not display the stored provider credentials
