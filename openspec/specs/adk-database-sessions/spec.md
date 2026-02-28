# adk-database-sessions Specification

## Purpose
TBD - created by archiving change bun-adk-session-service. Update Purpose after archive.
## Requirements
### Requirement: Database Session Storage

The ADK session service SHALL store conversational sessions and events persistently in a PostgreSQL database using `uptrace/bun`.

#### Scenario: Appending an event

- **WHEN** an ADK agent appends a new conversation event or tool call
- **THEN** the `adk-database-sessions` service persists the event to the `kb.adk_events` table

### Requirement: Session State Persistence

The ADK session service SHALL store and retrieve key-value session state persistently.

#### Scenario: Retrieving saved state

- **WHEN** an ADK agent queries the session for a specific state key
- **THEN** the service fetches the value from the `kb.adk_states` table based on the correct scope (app, user, or session)

### Requirement: Full Conversation Context Retrieval

The ADK session service SHALL reconstruct an ADK `session.Session` including all its historical events.

#### Scenario: Loading an existing session

- **WHEN** the system calls `Get()` on an existing ADK session ID
- **THEN** the service returns a session populated with all previously appended events from the `kb.adk_events` table

