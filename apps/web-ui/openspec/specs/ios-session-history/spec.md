# ios-session-history Specification

## Purpose
Lets the user review the selected agent's past sessions from the iOS app — listing that agent's recorded sessions and viewing a session's conversation timeline, driven by the session-log API.
## Requirements
### Requirement: List sessions for the selected agent

The app SHALL fetch and display the selected agent's recorded sessions from the session-log API, most recent first.

#### Scenario: Sessions listed

- **WHEN** the app loads the Sessions screen for an agent and the session-log API is reachable
- **THEN** the app shows that agent's recorded sessions ordered by recency

#### Scenario: No sessions

- **WHEN** the selected agent has no recorded sessions
- **THEN** the app shows an empty-sessions state rather than an error

### Requirement: View a session timeline

The app SHALL let the user open a session and view its conversation timeline — the ordered user and assistant turns and tool calls.

#### Scenario: Timeline shown

- **WHEN** the user opens a recorded session
- **THEN** the app displays that session's turns in chronological order

#### Scenario: Tool call details

- **WHEN** a session contains a tool call
- **THEN** the app shows the tool name, arguments, result, and whether it errored

### Requirement: Unknown session

The app SHALL treat a session the API cannot find as an empty timeline rather than an error.

#### Scenario: Missing session

- **WHEN** the user opens a session the API does not recognize
- **THEN** the app shows an empty timeline without crashing

### Requirement: Read-only history

The session history SHALL be read-only; the app MUST NOT offer any way to modify or delete recorded sessions.

#### Scenario: No mutation surface

- **WHEN** the user views session history
- **THEN** the app provides no create, edit, or delete actions for sessions

