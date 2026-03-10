## ADDED Requirements

### Requirement: Root run ID established at top-level entry
The system SHALL establish a `root_run_id` at the top-level execution entry points (`Execute` and `Resume`). The `root_run_id` SHALL be set to the current run's own ID when no caller-supplied value is present. If the caller supplies a `root_run_id` in `ExecuteRequest`, that value SHALL be used unchanged.

#### Scenario: Top-level Execute sets root run ID to own run ID
- **WHEN** `Execute` is called with `ExecuteRequest.RootRunID == nil`
- **THEN** the executor SHALL set `RootRunID` to the newly created `AgentRun.ID` before entering `runPipeline`

#### Scenario: Caller-supplied root run ID is preserved
- **WHEN** `Execute` is called with a non-nil `ExecuteRequest.RootRunID`
- **THEN** the executor SHALL use the caller-supplied value unchanged as the `root_run_id` for that run

#### Scenario: Resume entry point also sets root run ID
- **WHEN** `Resume` is called with `ExecuteRequest.RootRunID == nil`
- **THEN** the executor SHALL set `RootRunID` to the resumed run's own ID before entering `runPipeline`

### Requirement: Root run ID propagated through sub-agent spawns
The system SHALL propagate the `root_run_id` from a parent run to all sub-agents it spawns, so every node in an orchestration tree shares the same `root_run_id` as the top-level trigger.

#### Scenario: root_run_id forwarded to every spawned sub-agent
- **WHEN** an orchestrator agent calls `spawn_agents` to launch one or more sub-agents
- **THEN** every `ExecuteRequest` built for each sub-agent SHALL carry the same `RootRunID` as the parent run
- **AND** the sub-agent's own `root_run_id` SHALL equal the top-level run's ID, not the immediate parent's ID

#### Scenario: Nested sub-agents preserve the original root run ID
- **WHEN** a sub-agent at depth 2 spawns further sub-agents at depth 3
- **THEN** the depth-3 sub-agents SHALL receive the same `root_run_id` as the depth-1 top-level run
- **AND** the `root_run_id` SHALL NOT be overwritten at any intermediate depth

#### Scenario: CoordinationToolDeps carries root run ID
- **WHEN** `runPipeline` constructs `CoordinationToolDeps` for a run
- **THEN** `CoordinationToolDeps.RootRunID` SHALL be set to the resolved `root_run_id` for that run
- **AND** `executeSingleSpawn` SHALL copy `deps.RootRunID` into each child `ExecuteRequest.RootRunID`

### Requirement: Root run ID injected into execution context
The system SHALL inject the `root_run_id` into the Go context at the start of `runPipeline` so that all code executing within the pipeline (including the `TrackingModel` and OTel spans) can read it without requiring it to be passed as an explicit parameter.

#### Scenario: Context carries root run ID alongside run ID
- **WHEN** `runPipeline` begins execution
- **THEN** the context SHALL contain both `provider.RunID` (the current run's ID) and `provider.RootRunID` (the top-level run's ID)
- **AND** sub-agent pipelines SHALL each inject their own `run_id` into context while the `root_run_id` in context remains unchanged
