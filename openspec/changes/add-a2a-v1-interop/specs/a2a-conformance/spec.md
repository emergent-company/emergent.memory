## ADDED Requirements

### Requirement: Protocol version negotiation
The system SHALL negotiate the A2A protocol version from the `A2A-Version` header or query parameter and SHALL support version `1.0`. Requests specifying an unsupported version SHALL be rejected with a `VersionNotSupported` error.

#### Scenario: Version 1.0 is accepted
- **WHEN** a client sends a request with `A2A-Version: 1.0`
- **THEN** the server processes the request normally

#### Scenario: Absent version defaults to 1.0
- **WHEN** a client sends an A2A request with no `A2A-Version` header or query parameter
- **THEN** the server treats the request as version `1.0`

#### Scenario: Unsupported version is rejected
- **WHEN** a client sends a request with `A2A-Version: 9.9`
- **THEN** the server responds with HTTP 400 and a reason of `VERSION_NOT_SUPPORTED`

### Requirement: A2A error envelope
All A2A error responses SHALL use the `google.rpc.Status` JSON shape `{"error": {"code", "status", "message", "details"}}` where each detail has an `@type`, a `reason` in UPPER_SNAKE_CASE, and `domain` equal to `a2a-protocol.org`.

#### Scenario: Not-found produces TASK_NOT_FOUND
- **WHEN** a client requests an unknown task
- **THEN** the error body contains `details[0].reason` = `TASK_NOT_FOUND` and `details[0].domain` = `a2a-protocol.org`

#### Scenario: Not-cancelable produces TASK_NOT_CANCELABLE
- **WHEN** a client cancels a terminal task
- **THEN** the error body contains `details[0].reason` = `TASK_NOT_CANCELABLE`

#### Scenario: HTTP code matches the A2A mapping
- **WHEN** a not-found, not-cancelable, or version-not-supported error occurs
- **THEN** the HTTP status is 404, 400, and 400 respectively

### Requirement: Unimplemented operations are declared, not silently ignored
Operations that are not implemented in this milestone — push-notification configs and subscription variants — SHALL return an `UnsupportedOperation` error rather than a generic 404 or a success response.

#### Scenario: Push notification config is unsupported
- **WHEN** a client calls a `pushNotificationConfigs` endpoint while `capabilities.pushNotifications` is false
- **THEN** the server responds with HTTP 400 and a reason of `PUSH_NOTIFICATION_NOT_SUPPORTED`

#### Scenario: Advertised capabilities match implementation
- **WHEN** the extended AgentCard advertises `capabilities.streaming` and `capabilities.extendedAgentCard`
- **THEN** both the streaming endpoint and the extended-card endpoint are implemented and functional

### Requirement: Conformance shape tests
The system SHALL ship tests asserting the exact A2A v1.0 JSON shapes so internal vocabulary cannot leak onto the wire. This change satisfies the requirement via inline assertions (DTO camelCase keys, `TaskState`/`Role` enum spelling, oneof serialization, AgentCard required fields, tenant-leak invariant). Checked-in golden fixture files are a deferred enhancement.

#### Scenario: Conformance test rejects internal status strings
- **WHEN** the conformance test suite renders task JSON for every internal status
- **THEN** no output contains `cancelling`, `skipped`, `resume_run_id`, or any internal status string

#### Scenario: Conformance test enforces camelCase keys
- **WHEN** DTOs are serialized
- **THEN** keys are camelCase (`taskId`, `contextId`, `artifactId`, `supportedInterfaces`, `defaultInputModes`) and never snake_case

#### Scenario: Conformance test validates the AgentCard
- **WHEN** the global and extended AgentCards are rendered
- **THEN** both contain all required A2A fields (`name`, `description`, `version`, `capabilities`, `supportedInterfaces`, `defaultInputModes`, `defaultOutputModes`, `skills`)

### Requirement: Official TCK conformance (deferred)
Running the official A2A Technology Compatibility Kit in CI is an explicitly deferred future conformance goal for this change, because the TCK currently targets the 0.3 wire format rather than v1.0. This change SHALL NOT claim TCK CI coverage, and SHALL record the 0.3-vs-1.0 coverage gap in the change documentation.

#### Scenario: TCK CI coverage is not claimed
- **WHEN** a maintainer reads the conformance documentation for this change
- **THEN** no claim is made that the official TCK runs in CI as part of this change

#### Scenario: TCK gap is documented
- **WHEN** a maintainer reads the change documentation
- **THEN** the 0.3-wire TCK limitation is described as a deferred future goal alongside the current v1.0 shape-assertion coverage
