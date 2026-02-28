# adk-session-sdk Specification

## Purpose
TBD - created by archiving change adk-session-api. Update Purpose after archive.
## Requirements
### Requirement: ADK Session SDK Methods

The Go SDK SHALL provide methods to interact with the new ADK session endpoints:

- `ListADKSessions(ctx context.Context, projectID string) ([]ADKSessionDTO, error)`
- `GetADKSession(ctx context.Context, projectID string, sessionID string) (*ADKSessionDetailDTO, error)`

#### Scenario: Client fetches session details programmatically

- **WHEN** a developer calls `GetADKSession` via the SDK
- **THEN** the SDK executes the HTTP request and unmarshals the response into strongly-typed DTOs containing the session and its event sequence

