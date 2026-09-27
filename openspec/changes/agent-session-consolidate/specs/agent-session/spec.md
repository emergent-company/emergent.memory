## Purpose

Defines the canonical runtime model for agent work — a Session (a thread that groups one or more Runs) and a Run (a single execution turn) — and the mapping every interface uses to reach them, so the REST chat conversation, A2A `contextId`, public-share session, and CLI share one identity and one status vocabulary instead of each inventing its own.

## ADDED Requirements

### Requirement: A session groups runs

The system SHALL model a conversation thread as a Session that groups one or more Runs. A Run SHALL reference at most one Session. A Run MAY have no Session (ad-hoc or scheduled runs with no thread). A Session SHALL belong to exactly one project.

#### Scenario: Runs share one session

- **WHEN** two turns are executed against the same thread
- **THEN** both Runs reference the same Session

#### Scenario: Ad-hoc run has no session

- **WHEN** a run is created by a trigger or schedule with no thread
- **THEN** the Run has no Session and still executes normally

### Requirement: One session identity across interfaces

Every interface that groups runs — the REST chat conversation, the A2A task `contextId`, and the public-share session — SHALL resolve to the same Session identity for the same thread. Interface-specific identity SHALL NOT be exposed as a distinct core Session.

#### Scenario: Chat and A2A share the identity

- **WHEN** a chat conversation has a backing session and an A2A task is linked to the same `contextId`
- **THEN** both resolve to the same Session record

#### Scenario: Share and MCP identities stay adapter-local

- **WHEN** a public-share end-user session or an MCP transport session exists
- **THEN** it is not presented as a core Session and does not create a second session identity for the same thread

### Requirement: Session creation is create-or-get

Every interface SHALL obtain a thread's Session through one create-or-get path, so that repeated resolution for the same logical thread returns the same Session rather than creating duplicates.

#### Scenario: Repeated turns reuse the session

- **WHEN** the same thread is resolved on successive turns
- **THEN** the same Session is returned and no additional Session is created

### Requirement: Run execution status is the only per-run status

A Run SHALL carry a single execution status drawn from the defined run-status vocabulary. The system SHALL NOT persist a separate workspace-provisioning status on a Run; provisioning SHALL be a phase of a Run in the working state, and a provisioning failure SHALL be reported as a failed Run carrying an error message.

#### Scenario: Provisioning run reports working

- **WHEN** a run's workspace is being provisioned
- **THEN** the run reports the working execution status

#### Scenario: Provisioning failure reports failed

- **WHEN** workspace provisioning fails
- **THEN** the run reports a failed status with an error message, and no separate provisioning status is returned

### Requirement: Sessions do not carry execution status

A Session SHALL NOT expose an execution status; clients SHALL read execution status from its Runs. A session-level status that duplicates or conflicts with run status SHALL NOT be returned.

#### Scenario: Session response has no execution status

- **WHEN** a client reads a Session
- **THEN** the response carries no session-level execution status field

### Requirement: Session identity uses the consolidated vocabulary

A session's identity SHALL be exposed to clients as `sessionId`; its record SHALL live in the `sessions` table and run events in the `run_events` table. During the migration window the legacy `acpSessionId` key SHALL continue to be accepted and emitted with the same value.

#### Scenario: Client reads sessionId

- **WHEN** a client reads a conversation or run bound to a session
- **THEN** the response carries `sessionId`

#### Scenario: Legacy key still works

- **WHEN** a client that predates the rename reads `acpSessionId`
- **THEN** the response still carries the legacy key with the same value
