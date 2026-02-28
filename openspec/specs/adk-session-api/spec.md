# adk-session-api Specification

## Purpose
TBD - created by archiving change adk-session-api. Update Purpose after archive.
## Requirements
### Requirement: List ADK Sessions API Endpoint

The API SHALL expose a read-only endpoint (`GET /api/projects/:projectId/adk-sessions`) to list ADK sessions for a given project.
The endpoint MUST join with `agent_runs` to verify the session legitimately belongs to the requested project and enforce tenant isolation.

#### Scenario: User requests ADK sessions for their project

- **WHEN** an authenticated user calls `GET /api/projects/123/adk-sessions`
- **THEN** the system returns a paginated list of ADK sessions (excluding raw events to save bandwidth) associated with that project

### Requirement: Get ADK Session Details API Endpoint

The API SHALL expose a read-only endpoint (`GET /api/projects/:projectId/adk-sessions/:sessionId`) to retrieve a specific ADK session and its full array of `kb.adk_events`.

#### Scenario: User requests specific session details

- **WHEN** an authenticated user calls `GET /api/projects/123/adk-sessions/abc-456`
- **THEN** the system returns the session metadata along with its complete historical event chain
- **THEN** the system verifies the session belongs to project `123`

