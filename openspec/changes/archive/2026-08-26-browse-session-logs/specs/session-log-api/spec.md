## Purpose

Provides a read-only HTTP API exposing agent session logs (conversation timelines with turns and tool calls) to clients.

## ADDED Requirements

### Requirement: List sessions

The API SHALL list recorded sessions, most recent first.

#### Scenario: Sessions listed

- **WHEN** a client lists sessions
- **THEN** the API returns the sessions ordered by recency, each identified by its room name

#### Scenario: No sessions

- **WHEN** no sessions have been recorded
- **THEN** the API returns an empty session list

### Requirement: Retrieve a session timeline

The API SHALL return the ordered records for a session, including user and assistant turns, tool calls, and usage.

#### Scenario: Timeline returned

- **WHEN** a client requests a session's timeline
- **THEN** the API returns the session's records in chronological order

#### Scenario: Unknown session

- **WHEN** a client requests a timeline for an unknown room
- **THEN** the API returns an empty timeline, not an error

### Requirement: Expose tool call details

The API SHALL include, for each tool call in a session, the tool name, its arguments, its result, and whether it errored.

#### Scenario: Tool call detail present

- **WHEN** a session contains a tool call
- **THEN** the timeline record includes the tool name, arguments, output, and error flag

### Requirement: Read-only access

The API SHALL provide read-only access to session logs and MUST NOT modify them.

#### Scenario: Mutation rejected

- **WHEN** a client attempts to modify a session log through the API
- **THEN** the API rejects the request

### Requirement: Authenticated access

The API SHALL require the same shared API-key trust boundary as the token endpoint and MUST NOT expose session logs to unauthenticated callers.

#### Scenario: Unauthenticated request

- **WHEN** a client calls the API without a valid API key
- **THEN** the API returns an unauthorized response and no session data
