## MODIFIED Requirements

### Requirement: Studio Session Management

The system SHALL manage studio sessions for creating and editing schema definitions with draft mode and version tracking. Sessions SHALL be stored in `kb.schema_studio_sessions` and messages in `kb.schema_studio_messages`. All API endpoints for studio sessions SHALL be under `/api/schemas/studio/`.

#### Scenario: Create new studio session for new schema

- **WHEN** a user initiates a new schema creation via the studio
- **THEN** the system SHALL create a new studio session in `kb.schema_studio_sessions`
- **AND** the system SHALL create a draft `MemorySchema` with `draft: true` in `kb.graph_schemas`
- **AND** the system SHALL initialize an empty schema definition
- **AND** the system SHALL return a session ID for subsequent interactions

#### Scenario: Create studio session for existing schema

- **WHEN** a user opens an existing schema in the studio
- **THEN** the system SHALL create a new studio session in `kb.schema_studio_sessions`
- **AND** the system SHALL clone the existing schema as a draft with `draft: true`
- **AND** the system SHALL set `parent_version_id` to reference the original schema in `kb.graph_schemas`
- **AND** the system SHALL load the existing schema definition into the preview

#### Scenario: Discard studio session

- **WHEN** a user discards a studio session
- **THEN** the system SHALL delete the draft `MemorySchema` from `kb.graph_schemas`
- **AND** the system SHALL delete all conversation history from `kb.schema_studio_messages` for the session
- **AND** the system SHALL NOT affect the original schema (if editing existing)

#### Scenario: Session timeout

- **WHEN** a studio session has been inactive for 24 hours
- **THEN** the system SHALL mark the session as expired in `kb.schema_studio_sessions`
- **AND** the system SHALL retain the draft schema for potential recovery
- **AND** the system SHALL warn the user on next access

### Requirement: Chat Interface

The system SHALL provide a streaming chat interface for natural language schema definition with the LLM, accessible at `/api/schemas/studio/sessions/:sessionId/chat`.

#### Scenario: User sends schema description message

- **WHEN** a user sends a message describing a schema requirement to `/api/schemas/studio/sessions/:sessionId/chat`
- **THEN** the system SHALL stream the LLM response in real-time via SSE
- **AND** the system SHALL store the message in `kb.schema_studio_messages`
- **AND** the system SHALL parse any schema suggestions from the response
- **AND** the system SHALL render suggestions as actionable change cards

#### Scenario: User asks about schema best practices

- **WHEN** a user asks a question about JSON Schema or schema conventions in the studio chat
- **THEN** the system SHALL provide educational information from the system prompt
- **AND** the system SHALL NOT modify the current draft schema
- **AND** the system SHALL suggest relevant schema changes if applicable

#### Scenario: Chat error handling

- **WHEN** the LLM API fails or times out during a studio chat request
- **THEN** the system SHALL display a clear error message
- **AND** the system SHALL preserve the conversation history in `kb.schema_studio_messages`
- **AND** the system SHALL provide a retry option
- **AND** the system SHALL NOT corrupt the draft schema in `kb.graph_schemas`

#### Scenario: Context assembly for LLM

- **WHEN** the system prepares context for the LLM request in the studio
- **THEN** the system SHALL include the current draft schema definition from `kb.graph_schemas`
- **AND** the system SHALL include the full conversation history from `kb.schema_studio_messages`
- **AND** the system SHALL include JSON Schema best practices in the system prompt
- **AND** the system SHALL include schema structure guidelines
