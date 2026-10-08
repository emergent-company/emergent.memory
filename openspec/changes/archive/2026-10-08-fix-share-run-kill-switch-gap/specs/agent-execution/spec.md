## MODIFIED Requirements

### Requirement: Callers SHALL NOT leave a pre-created run stuck `running` when execution is disabled

Callers that create an agent run row before invoking the executor (the HTTP
trigger endpoint, the A2A send/resume paths, and the keyed agent-share
`StreamMessage` path) SHALL refuse **before** creating the run when execution is
disabled. Where a run row already exists because it was pre-created (a resume run
or a share run), the caller SHALL mark it failed with the disabled reason so it
reaches a terminal state instead of remaining `running` until the stale-run
reaper reports a misleading idle-timeout.

#### Scenario: HTTP trigger is refused before run creation
- **WHEN** the kill switch is off
- **AND** the HTTP trigger endpoint is invoked
- **THEN** it SHALL return an explicit error and SHALL NOT create a run row
- **AND** it SHALL NOT report a false `success: true`

#### Scenario: A2A send is refused before run creation on both surfaces
- **WHEN** the kill switch is off
- **AND** an A2A send is invoked via either the JSON `/message:send` or the SSE `/message:stream` surface
- **THEN** it SHALL return a disabled A2A error and SHALL NOT create a run row

#### Scenario: A refused A2A stream run is failed terminal
- **WHEN** a run reaches `ExecuteWithRun`/`Resume` on the SSE `/message:stream` surface while the kill switch is off (including a switch flipped after the pre-check)
- **THEN** the surface SHALL mark the run failed with the disabled reason
- **AND** SHALL emit a `TASK_STATE_FAILED` status update
- **AND** the run SHALL NOT remain `running`

#### Scenario: A pre-created resume run is marked failed
- **WHEN** a pre-created resume run exists
- **AND** `Resume` returns `ErrExecutionDisabled`
- **THEN** the caller SHALL mark that pre-created run failed with the disabled reason
- **AND** it SHALL reach a terminal state rather than remaining `running`

#### Scenario: Share stream is refused before run creation
- **WHEN** the kill switch is off
- **AND** the keyed share stream endpoint (`StreamMessage`) is invoked
- **THEN** it SHALL return a `503 agent_execution_disabled` error
- **AND** it SHALL NOT create a share run row

#### Scenario: A pre-created share run is marked failed
- **WHEN** a share run was pre-created
- **AND** execution returns `ErrExecutionDisabled`
- **THEN** the caller SHALL mark that share run failed with the disabled reason
- **AND** it SHALL reach a terminal state rather than remaining `running`
