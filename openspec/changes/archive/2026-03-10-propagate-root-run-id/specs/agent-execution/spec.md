## MODIFIED Requirements

### Requirement: Agent Run Lifecycle Tracking
The system SHALL create an AgentRun record before execution begins and update it upon completion or failure.

#### Scenario: Successful execution
- **WHEN** an agent executes successfully
- **THEN** an AgentRun record SHALL be created with `status: running` before execution
- **AND** updated to `status: completed` after execution
- **AND** `summary` SHALL contain the agent's final text output
- **AND** `duration` SHALL reflect the wall-clock execution time
- **AND** `completed_at` SHALL be set to the completion timestamp

#### Scenario: Failed execution
- **WHEN** an agent execution encounters an error
- **THEN** the AgentRun SHALL be updated to `status: failed`
- **AND** `error_message` SHALL contain the error description
- **AND** `duration` and `completed_at` SHALL still be set

#### Scenario: Cancelled execution
- **WHEN** an agent execution is cancelled via context cancellation
- **THEN** the AgentRun SHALL be updated to `status: cancelled`
- **AND** `summary` SHALL contain any partial output produced before cancellation

### Requirement: Trigger Endpoint Activation
The existing agent trigger endpoint SHALL execute agents using the AgentExecutor instead of marking runs as skipped.

#### Scenario: Manual trigger via API
- **WHEN** a POST request is made to `/api/projects/:id/agents/:agentId/trigger`
- **THEN** the system SHALL build an ADK-Go pipeline from the agent's definition
- **AND** execute it with the request body as input
- **AND** return the AgentRun record with execution results
- **AND** the run SHALL NOT have `status: skipped`

## ADDED Requirements

### Requirement: ExecuteRequest carries RootRunID
`ExecuteRequest` SHALL include a `RootRunID *string` field. The top-level entry points (`Execute`, `Resume`) SHALL set this field to the current run's own ID when the caller does not supply one. Sub-agent spawns SHALL always receive a non-nil `RootRunID` copied from `CoordinationToolDeps`.

#### Scenario: Execute sets RootRunID when absent
- **WHEN** `Execute` is called with `RootRunID == nil`
- **THEN** after creating the `AgentRun` record, the executor SHALL set `req.RootRunID = &run.ID`
- **AND** pass this populated request into `runPipeline`

#### Scenario: Sub-agent receives parent's root run ID
- **WHEN** `executeSingleSpawn` builds an `ExecuteRequest` for a child agent
- **THEN** `ExecuteRequest.RootRunID` SHALL equal `CoordinationToolDeps.RootRunID`
- **AND** SHALL NOT be overwritten with the child's own run ID
