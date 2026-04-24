# Spec: session-api

## Purpose

HTTP API endpoints for managing Sessions and their Messages as graph objects within a project.

## Requirements

### Requirement: Create session endpoint
The system SHALL expose `POST /api/v1/projects/:project/sessions` that creates a Session graph object. Request body: `title` (required), `started_at` (optional, defaults to now), `summary` (optional), `agent_version` (optional). Response: the created Session object with its graph ID.

#### Scenario: Session created successfully
- **WHEN** a POST request is sent to `/sessions` with a valid title
- **THEN** a Session graph object MUST be created and returned with a unique ID

### Requirement: Append message to session
The system SHALL expose `POST /api/v1/projects/:project/sessions/:id/messages` that atomically: (1) creates a Message graph object, (2) assigns `sequence_number` as the next integer in the session's message sequence, (3) creates a `has_message` relationship from Session → Message. All three operations MUST succeed or all MUST be rolled back.

#### Scenario: Message appended with correct sequence number
- **WHEN** three messages are appended sequentially to a session
- **THEN** they SHALL have `sequence_number` values 1, 2, 3 respectively

#### Scenario: Concurrent message appends
- **WHEN** two messages are appended concurrently to the same session
- **THEN** each MUST receive a unique, non-duplicate sequence_number

#### Scenario: Session not found
- **WHEN** a message is appended to a non-existent session ID
- **THEN** the endpoint MUST return 404

### Requirement: List session messages
The system SHALL expose `GET /api/v1/projects/:project/sessions/:id/messages` returning messages ordered by `sequence_number` ascending, with pagination (`cursor`, `limit`).

#### Scenario: Messages returned in order
- **WHEN** messages are fetched for a session
- **THEN** they MUST be ordered by `sequence_number` ascending

### Requirement: List sessions
The system SHALL expose `GET /api/v1/projects/:project/sessions` returning sessions ordered by `started_at` descending, with pagination.

#### Scenario: Sessions listed
- **WHEN** GET /sessions is called
- **THEN** all project sessions MUST be returned paginated by started_at descending
