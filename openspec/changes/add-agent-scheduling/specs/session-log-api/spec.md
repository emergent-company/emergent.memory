## ADDED Requirements

### Requirement: Expose a session's origin

The API SHALL include, for each session, its origin — distinguishing scheduled sessions from manual, voice, and chat sessions — so clients can tell how a session was started.

#### Scenario: Origin present on a session

- **WHEN** a client lists sessions
- **THEN** each session includes an origin value (for example, scheduled, manual, or voice)

#### Scenario: Scheduled origin on a scheduled session

- **WHEN** a session was produced by a scheduled agent
- **THEN** the session's origin is reported as scheduled

### Requirement: Filter sessions by origin

The API SHALL support filtering the session list by origin, so a client can list only scheduled sessions or exclude them.

#### Scenario: List only scheduled sessions

- **WHEN** a client lists sessions filtered to the scheduled origin
- **THEN** only sessions whose origin is scheduled are returned

#### Scenario: Filter yields no matches

- **WHEN** a client filters sessions by an origin with no matching sessions
- **THEN** the API returns an empty session list, not an error
