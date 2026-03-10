## ADDED Requirements

### Requirement: Schema CRUD API at /api/schemas

The system SHALL expose all MemorySchema management endpoints under the `/api/schemas` path prefix. The Go package SHALL be named `schemas`, the fx module SHALL be named `schemas`, and the primary struct SHALL be named `MemorySchema`.

#### Scenario: Create schema

- **WHEN** a POST request is sent to `/api/schemas` with a valid schema definition
- **THEN** the system SHALL create a new `MemorySchema` record in `kb.graph_schemas` and return HTTP 201 with the created schema

#### Scenario: Get schema by ID

- **WHEN** a GET request is sent to `/api/schemas/:schemaId`
- **THEN** the system SHALL return the `MemorySchema` record or HTTP 404 if not found

#### Scenario: Update schema

- **WHEN** a PUT request is sent to `/api/schemas/:schemaId` with updated fields
- **THEN** the system SHALL update the record in `kb.graph_schemas` and return HTTP 200

#### Scenario: Delete schema

- **WHEN** a DELETE request is sent to `/api/schemas/:schemaId`
- **THEN** the system SHALL remove the record and return HTTP 204

#### Scenario: List available schemas for a project

- **WHEN** a GET request is sent to `/api/schemas/projects/:projectId/available`
- **THEN** the system SHALL return all schemas available to the project (org-level schemas not yet installed)

#### Scenario: List installed schemas for a project

- **WHEN** a GET request is sent to `/api/schemas/projects/:projectId/installed`
- **THEN** the system SHALL return all `ProjectMemorySchema` assignment records for the project from `kb.project_schemas`

#### Scenario: Get compiled types for a project

- **WHEN** a GET request is sent to `/api/schemas/projects/:projectId/compiled-types`
- **THEN** the system SHALL return the merged, resolved type definitions from all installed schemas for the project

#### Scenario: Assign schema to project

- **WHEN** a POST request is sent to `/api/schemas/projects/:projectId/assign` with a `schema_id`
- **THEN** the system SHALL insert a record into `kb.project_schemas` and return HTTP 201

#### Scenario: Update schema assignment

- **WHEN** a PATCH request is sent to `/api/schemas/projects/:projectId/assignments/:assignmentId`
- **THEN** the system SHALL update the assignment (e.g., active flag) and return HTTP 200

#### Scenario: Unassign schema from project

- **WHEN** a DELETE request is sent to `/api/schemas/projects/:projectId/assignments/:assignmentId`
- **THEN** the system SHALL remove the assignment record and return HTTP 204

### Requirement: Schema Studio API at /api/schemas/studio

The system SHALL expose schema studio session endpoints under `/api/schemas/studio`. Studio sessions allow AI-assisted schema creation and editing.

#### Scenario: Create studio session

- **WHEN** a POST request is sent to `/api/schemas/studio/sessions`
- **THEN** the system SHALL create a new studio session record in `kb.schema_studio_sessions` and return the session ID

#### Scenario: Send studio chat message

- **WHEN** a POST request is sent to `/api/schemas/studio/sessions/:sessionId/chat`
- **THEN** the system SHALL stream an LLM response via SSE and store the message in `kb.schema_studio_messages`

#### Scenario: Apply studio suggestion

- **WHEN** a POST request is sent to `/api/schemas/studio/sessions/:sessionId/apply`
- **THEN** the system SHALL apply the pending schema suggestion to the draft schema

#### Scenario: Save studio schema

- **WHEN** a POST request is sent to `/api/schemas/studio/sessions/:sessionId/save`
- **THEN** the system SHALL persist the draft schema as a published `MemorySchema` record

### Requirement: Database tables use kb.graph_schemas naming

The system SHALL store all MemorySchema records in `kb.graph_schemas` (primary table) and `kb.project_schemas` (project assignment join table). Studio sessions SHALL be stored in `kb.schema_studio_sessions` and `kb.schema_studio_messages`.

#### Scenario: Schema record persisted in correct table

- **WHEN** a new MemorySchema is created via the API
- **THEN** the system SHALL insert a row into `kb.graph_schemas`
- **AND** the FK column referencing this table from `kb.project_schemas` SHALL be named `schema_id`

#### Scenario: Discovery job schema reference

- **WHEN** a DiscoveryJob completes and produces a schema
- **THEN** the system SHALL store the schema reference using the `schema_id` column on `kb.discovery_jobs`
