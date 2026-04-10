## ADDED Requirements

### Requirement: Create ACP session
The system SHALL expose `POST /acp/v1/sessions` that creates a new thin session for grouping related runs. The request body MAY include an optional `agent_name` field to scope the session to a specific agent. The server SHALL respond with HTTP 201 and the session object containing `id`, `agent_name` (if scoped), `created_at`, and an empty `history` array.

#### Scenario: Create session with no agent scope
- **WHEN** an authenticated client sends `POST /acp/v1/sessions` with an empty body `{}`
- **THEN** the server responds with HTTP 201 and a session object with a valid UUID `id`, `agent_name: null`, `created_at`, and `history: []`

#### Scenario: Create session scoped to an agent
- **WHEN** an authenticated client sends `POST /acp/v1/sessions` with `{"agent_name": "my-agent"}` and `my-agent` is a valid external agent
- **THEN** the server responds with HTTP 201 and a session object with `agent_name: "my-agent"`

#### Scenario: Create session with non-existent agent returns 404
- **WHEN** an authenticated client sends `POST /acp/v1/sessions` with `{"agent_name": "nonexistent"}`
- **THEN** the server responds with HTTP 404

#### Scenario: Create session without auth returns 401
- **WHEN** an unauthenticated client sends `POST /acp/v1/sessions`
- **THEN** the server responds with HTTP 401

#### Scenario: Create session without agents:write scope returns 403
- **WHEN** a client sends `POST /acp/v1/sessions` with a token that lacks `agents:write` scope
- **THEN** the server responds with HTTP 403

### Requirement: Get ACP session
The system SHALL expose `GET /acp/v1/sessions/:sessionId` that returns the session object including `id`, `agent_name`, `created_at`, `updated_at`, and `history` — an ordered array of run summary objects. Each run summary SHALL include `id`, `agent_name`, `status` (mapped to ACP status), and `created_at`.

#### Scenario: Get session with run history
- **WHEN** an authenticated client sends `GET /acp/v1/sessions/<sessionId>` and the session has 3 associated runs
- **THEN** the server responds with HTTP 200 and the session object with `history` containing 3 run summary objects ordered by `created_at` ascending

#### Scenario: Get session with no runs
- **WHEN** an authenticated client sends `GET /acp/v1/sessions/<sessionId>` and the session has no associated runs
- **THEN** the server responds with HTTP 200 and the session object with `history: []`

#### Scenario: Get non-existent session returns 404
- **WHEN** an authenticated client sends `GET /acp/v1/sessions/nonexistent-uuid`
- **THEN** the server responds with HTTP 404

#### Scenario: Get session from another project returns 404
- **WHEN** an authenticated client sends `GET /acp/v1/sessions/<sessionId>` where the session belongs to a different project
- **THEN** the server responds with HTTP 404 (sessions are project-scoped via the API token)

### Requirement: Thin session model
ACP sessions SHALL be "thin" — they track run history only. There SHALL be no cross-run context injection, no shared memory, and no session state URI scratchpad. Each run within a session executes independently with its own context. The session serves purely as a grouping mechanism for client-side conversation tracking.

#### Scenario: Runs in same session are independent
- **WHEN** two runs are created with the same `session_id`, where run 1 generates output "X"
- **THEN** run 2 does NOT automatically receive run 1's output as context — it starts with only its own `message` input

#### Scenario: Session does not inject prior run context
- **WHEN** a client creates a run with `session_id` set to an existing session that has prior runs
- **THEN** the agent executor receives ONLY the current run's message, not messages from prior session runs

### Requirement: ACP sessions database schema
The system SHALL create a `kb.acp_sessions` table with columns: `id` (UUID, primary key), `project_id` (UUID, NOT NULL, FK to projects), `agent_name` (TEXT, nullable — the ACP slug if session is agent-scoped), `created_at` (TIMESTAMPTZ), `updated_at` (TIMESTAMPTZ). The `kb.agent_runs` table SHALL gain an `acp_session_id` (UUID, nullable, FK to `kb.acp_sessions`) column.

#### Scenario: Migration creates acp_sessions table
- **WHEN** migration `00082_acp_sessions.sql` runs
- **THEN** the `kb.acp_sessions` table exists with columns `id`, `project_id`, `agent_name`, `created_at`, `updated_at`

#### Scenario: Migration adds acp_session_id to agent_runs
- **WHEN** migration `00082_acp_sessions.sql` runs
- **THEN** the `kb.agent_runs` table has a nullable `acp_session_id` column with a foreign key to `kb.acp_sessions(id)`

#### Scenario: Existing agent_runs are unaffected
- **WHEN** migration `00082_acp_sessions.sql` runs on a database with existing agent_runs rows
- **THEN** all existing rows have `acp_session_id = NULL` and no data is lost

#### Scenario: Index on acp_session_id for history queries
- **WHEN** migration `00082_acp_sessions.sql` runs
- **THEN** an index exists on `kb.agent_runs(acp_session_id)` for efficient session history lookups

### Requirement: Auth scopes for session endpoints
Session creation (`POST /acp/v1/sessions`) SHALL require `agents:write` scope. Session retrieval (`GET /acp/v1/sessions/:sessionId`) SHALL require `agents:read` scope. The project context SHALL be determined from the API token's associated project.

#### Scenario: Create session requires agents:write
- **WHEN** a client with `agents:write` scope sends `POST /acp/v1/sessions`
- **THEN** the server creates the session successfully

#### Scenario: Get session requires agents:read
- **WHEN** a client with `agents:read` scope sends `GET /acp/v1/sessions/<sessionId>`
- **THEN** the server returns the session successfully

#### Scenario: Get session without agents:read returns 403
- **WHEN** a client with only `graph:read` scope sends `GET /acp/v1/sessions/<sessionId>`
- **THEN** the server responds with HTTP 403
